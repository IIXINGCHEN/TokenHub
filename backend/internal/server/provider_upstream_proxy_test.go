package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type providerProxySettingsStore struct {
	Store
	fail bool
}

func (store *providerProxySettingsStore) ListResourcesContext(_ context.Context, kind string) ([]AdminResource, error) {
	if store.fail && kind == "settings" {
		return nil, errors.New("temporary settings read failure")
	}
	return store.Store.ListResourcesChecked(kind)
}

func TestProviderTransportUsesConfiguredForwardProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyRequests.Add(1)
		if request.Method != http.MethodConnect {
			t.Errorf("expected HTTPS proxy CONNECT, got %s", request.Method)
		}
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	var directRequests atomic.Int32
	direct := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		directRequests.Add(1)
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})
	transport := providerTransportWithProxy(direct, nil, func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	})
	request, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("expected the test proxy to reject the CONNECT request")
	}
	if proxyRequests.Load() != 1 {
		t.Fatalf("expected one request through the forward proxy, got %d", proxyRequests.Load())
	}
	if directRequests.Load() != 0 {
		t.Fatalf("expected the guarded direct transport to be bypassed, got %d requests", directRequests.Load())
	}
}

func TestCodexSubscriptionResponsesInheritsEnvironmentProxy(t *testing.T) {
	for _, proxyVariables := range []struct {
		name  string
		http  string
		https string
	}{
		{name: "uppercase", http: "HTTP_PROXY", https: "HTTPS_PROXY"},
		{name: "lowercase", http: "http_proxy", https: "https_proxy"},
	} {
		t.Run(proxyVariables.name, func(t *testing.T) {
			connectTargets := make(chan string, 1)
			proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodConnect {
					t.Errorf("environment proxy method = %s, want CONNECT", request.Method)
				}
				connectTargets <- request.Host
				writer.WriteHeader(http.StatusBadGateway)
			}))
			defer proxy.Close()

			for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
				t.Setenv(name, "")
			}
			t.Setenv(proxyVariables.http, proxy.URL)
			t.Setenv(proxyVariables.https, proxy.URL)

			server, _, secret := newCodexCompatibilityRouteTestServer(t, nil)
			t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
			response := doCodexCompatibilityRouteJSON(t, server.Handler(), "/v1/responses", map[string]any{
				"model": codexCompatibilityRouteModel,
				"input": "verify the inherited environment proxy",
			}, secret, "environment-proxy-regression")
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "provider_proxy_connect_failed") {
				t.Fatalf("environment proxy failure = %d: %s", response.Code, response.Body.String())
			}

			select {
			case target := <-connectTargets:
				if target != "chatgpt.com:443" {
					t.Fatalf("environment proxy CONNECT target = %q, want %q", target, "chatgpt.com:443")
				}
			case <-time.After(time.Second):
				t.Fatalf("Codex Subscription request did not use %s", proxyVariables.https)
			}
		})
	}
}

func TestProviderTransportUsesGuardedDirectPathWithoutProxy(t *testing.T) {
	var directRequests atomic.Int32
	direct := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		directRequests.Add(1)
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})
	transport := providerTransportWithProxy(direct, nil, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	request, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	if directRequests.Load() != 1 {
		t.Fatalf("expected one guarded direct request, got %d", directRequests.Load())
	}
}

func TestProviderProxyFailureIsResourceNeutralAndStopsFailover(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	transport := providerTransportWithProxy(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("direct path must not be used")
	}), nil, func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	})
	request, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if err == nil {
		t.Fatal("expected unreachable proxy to fail")
	}
	if got := providerErrorDisposition(err); got != ProviderErrorEgress {
		t.Fatalf("proxy error disposition = %q, want %q", got, ProviderErrorEgress)
	}
	if got := AsHTTPError(err).Code; got != "provider_proxy_connect_failed" {
		t.Fatalf("proxy error code = %q", got)
	}
	if outcome := providerAttemptOutcome(err); outcome != AttemptNeutral {
		t.Fatalf("proxy error outcome = %v, want neutral", outcome)
	}
	if shouldFailoverRoutedError(err, false) {
		t.Fatal("shared proxy failure must not try another Provider resource")
	}
}

func TestProviderProxyAuthenticationFailureHasSafeStageError(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Proxy-Authenticate", `Basic realm="provider-egress"`)
		writer.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	transport := providerTransportWithProxy(nil, nil, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
	request, _ := http.NewRequest(http.MethodGet, "https://provider.example/v1/models", nil)
	_, err := transport.RoundTrip(request)
	if AsHTTPError(err).Code != "provider_proxy_auth_failed" || providerErrorDisposition(err) != ProviderErrorEgress {
		t.Fatalf("proxy auth error = %#v", err)
	}
	if strings.Contains(err.Error(), proxyURL.String()) {
		t.Fatalf("proxy auth error leaked proxy URL: %v", err)
	}
}

func TestProviderProxyPolicyRefreshesSharedSettingsWithinFiveSeconds(t *testing.T) {
	base := NewMemoryStore()
	setting := AdminResource{ID: gatewaySettingsID, Name: "Gateway", Status: StatusActive, Fields: map[string]any{}}
	setConfiguredProxyFields(t, setting.Fields, "http://proxy-one.example:8080")
	base.CreateResource("settings", setting)
	policy := newProviderProxyPolicy(base)
	request, _ := http.NewRequest(http.MethodGet, "https://provider.example/v1/models", nil)

	first, err := policy.proxyForRequest(request)
	if err != nil || first.Host != "proxy-one.example:8080" {
		t.Fatalf("initial proxy = %v, %v", first, err)
	}
	setConfiguredProxyFields(t, setting.Fields, "http://proxy-two.example:8081")
	if _, err := base.UpdateResource("settings", gatewaySettingsID, setting); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(providerProxyRefreshInterval + time.Second)
	for time.Now().Before(deadline) {
		current, refreshErr := policy.proxyForRequest(request)
		if refreshErr == nil && current != nil && current.Host == "proxy-two.example:8081" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("shared Provider proxy settings did not refresh within five seconds")
}

func TestProviderProxyPolicyKeepsLastKnownConfigurationOnTemporaryReadFailure(t *testing.T) {
	base := NewMemoryStore()
	setting := AdminResource{ID: gatewaySettingsID, Name: "Gateway", Status: StatusActive, Fields: map[string]any{}}
	setConfiguredProxyFields(t, setting.Fields, "http://proxy-known.example:8080")
	base.CreateResource("settings", setting)
	store := &providerProxySettingsStore{Store: base}
	policy := newProviderProxyPolicy(store)
	request, _ := http.NewRequest(http.MethodGet, "https://provider.example/v1/models", nil)

	store.fail = true
	policy.mu.Lock()
	policy.loadedAt = time.Now().Add(-providerProxyRefreshInterval)
	policy.mu.Unlock()
	proxyURL, err := policy.proxyForRequest(request)
	if err != nil || proxyURL == nil || proxyURL.Host != "proxy-known.example:8080" {
		t.Fatalf("last-known proxy = %v, %v", proxyURL, err)
	}
}

func TestProviderProxyPolicyFailsClosedBeforeFirstSuccessfulLoad(t *testing.T) {
	store := &providerProxySettingsStore{Store: NewMemoryStore(), fail: true}
	policy := newProviderProxyPolicy(store)
	request, _ := http.NewRequest(http.MethodGet, "https://provider.example/v1/models", nil)
	proxyURL, err := policy.proxyForRequest(request)
	if proxyURL != nil || AsHTTPError(err).Code != "provider_proxy_config_error" {
		t.Fatalf("unloaded policy = %v, %v", proxyURL, err)
	}
}
