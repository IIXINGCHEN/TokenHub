package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
)

type providerProxySelector func(*http.Request) (*url.URL, error)

type selectedProviderProxyContextKey struct{}

// providerEnvironmentProxyTransport keeps proxy and direct egress as separate
// connection pools. Direct requests retain TokenHub's guarded DialContext;
// requests selected by HTTP_PROXY, HTTPS_PROXY, and NO_PROXY use the operator's
// forward proxy without applying target-address rules to the proxy connection.
type providerEnvironmentProxyTransport struct {
	direct      http.RoundTripper
	proxied     *http.Transport
	selectProxy providerProxySelector
}

func providerTransportWithEnvironmentProxy(direct http.RoundTripper, configure func(*http.Transport), policies ...*providerProxyPolicy) http.RoundTripper {
	selectProxy := providerProxySelector(http.ProxyFromEnvironment)
	var policy *providerProxyPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	if policy != nil {
		selectProxy = policy.proxyForRequest
	}
	transport := providerTransportWithProxy(direct, configure, selectProxy)
	if policy != nil {
		policy.registerTransport(transport.(providerProxyIdleCloser))
	}
	return transport
}

func providerTransportWithProxy(direct http.RoundTripper, configure func(*http.Transport), selectProxy providerProxySelector) http.RoundTripper {
	if direct == nil {
		direct = http.DefaultTransport
	}
	if selectProxy == nil {
		selectProxy = http.ProxyFromEnvironment
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	var proxied *http.Transport
	if ok {
		proxied = base.Clone()
	} else {
		proxied = &http.Transport{}
	}
	proxied.MaxIdleConnsPerHost = 64
	proxied.MaxIdleConns = 256
	proxied.Proxy = func(request *http.Request) (*url.URL, error) {
		proxyURL, _ := request.Context().Value(selectedProviderProxyContextKey{}).(*url.URL)
		return proxyURL, nil
	}
	if configure != nil {
		configure(proxied)
	}
	proxied.OnProxyConnectResponse = func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
		if response == nil {
			return newProviderProxyTransportError("connect", nil)
		}
		if response.StatusCode == http.StatusProxyAuthRequired {
			return newProviderProxyTransportError("auth", nil)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return newProviderProxyTransportError("connect", nil)
		}
		return nil
	}
	return &providerEnvironmentProxyTransport{
		direct:      direct,
		proxied:     proxied,
		selectProxy: selectProxy,
	}
}

func (transport *providerEnvironmentProxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, NewHTTPError(http.StatusBadRequest, "provider_base_url_invalid", "Base URL is invalid")
	}
	if err := validateProviderUpstreamURLSyntax(request.URL); err != nil {
		return nil, err
	}
	proxyURL, err := transport.selectProxy(request)
	if err != nil {
		if providerErrorDisposition(err) == ProviderErrorEgress {
			return nil, err
		}
		return nil, newProviderProxyTransportError("config", err)
	}
	if proxyURL == nil {
		return transport.direct.RoundTrip(request)
	}
	ctx := context.WithValue(request.Context(), selectedProviderProxyContextKey{}, proxyURL)
	response, err := transport.proxied.RoundTrip(request.Clone(ctx))
	if err != nil {
		if egressErr := providerEgressFailure(err); egressErr != nil {
			return nil, egressErr
		}
		return nil, newProviderProxyTransportError("connect", err)
	}
	if response.StatusCode == http.StatusProxyAuthRequired {
		_ = response.Body.Close()
		return nil, newProviderProxyTransportError("auth", nil)
	}
	return response, nil
}

func (*providerEnvironmentProxyTransport) providerEgressTransport() {}

func (transport *providerEnvironmentProxyTransport) CloseIdleConnections() {
	if closer, ok := transport.direct.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	transport.proxied.CloseIdleConnections()
}

func newProviderProxyTransportError(stage string, err error) error {
	status := http.StatusBadGateway
	code := "provider_proxy_connect_failed"
	message := "Provider proxy connection failed"
	switch stage {
	case "config":
		status = http.StatusServiceUnavailable
		code = "provider_proxy_config_error"
		message = "Provider proxy configuration is invalid"
	case "auth":
		code = "provider_proxy_auth_failed"
		message = "Provider proxy authentication failed"
	case "timeout":
		status = http.StatusGatewayTimeout
		code = "provider_proxy_timeout"
		message = "Provider proxy connection timed out"
	default:
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
			status = http.StatusGatewayTimeout
			code = "provider_proxy_timeout"
			message = "Provider proxy connection timed out"
		}
	}
	return &ProviderInvocationError{
		Err:         NewHTTPError(status, code, message),
		Disposition: ProviderErrorEgress,
	}
}
