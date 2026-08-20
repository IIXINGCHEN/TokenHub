package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const providerProxyTestTimeout = 15 * time.Second

type providerEgressTestRequest struct {
	ProviderID string         `json:"provider_id"`
	Fields     map[string]any `json:"fields"`
}

func (s *Server) handleAdminProviderEgressTestPost(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r, "provider", r.Method); !ok {
		return
	}
	var req providerEgressTestRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	provider, found := s.store.GetProvider(strings.TrimSpace(req.ProviderID))
	if !found {
		writeError(w, r, NewHTTPError(http.StatusNotFound, "provider_not_found", "Provider not found"))
		return
	}
	target, err := providerEgressTestTarget(provider)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setting := AdminResource{ID: gatewaySettingsID, Status: StatusActive, Fields: req.Fields}
	if current, findErr := s.findResource("settings", gatewaySettingsID); findErr == nil {
		setting.Fields = preserveAdminResourceSecrets("settings", current.Fields, req.Fields)
	}
	setting.Fields = normalizeProviderProxySettingsFields(setting.Fields)
	if err := validateProviderProxySettings(setting, s.store); err != nil {
		writeError(w, r, err)
		return
	}
	snapshot, err := providerProxySnapshotFromSetting(setting, s.store)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if snapshot.mode != providerEgressModeConfigured || snapshot.proxyURL == nil {
		writeError(w, r, NewHTTPError(http.StatusBadRequest, "provider_proxy_test_requires_configured_proxy", "Proxy test requires configured proxy mode"))
		return
	}
	started := time.Now()
	if err := testProviderProxyConnection(r.Context(), snapshot.proxyURL, target); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "provider_id": provider.ID, "target_host": target.Hostname(),
		"mode": snapshot.mode, "latency_ms": time.Since(started).Milliseconds(),
	})
}

func providerEgressTestTarget(provider Provider) (*url.URL, error) {
	raw := strings.TrimSpace(provider.BaseURL)
	if raw == "" {
		return nil, NewHTTPError(http.StatusBadRequest, "provider_base_url_required", "Provider base URL is required for proxy testing")
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, NewHTTPError(http.StatusBadRequest, "provider_base_url_invalid", "Base URL is invalid")
	}
	if err := validateProviderUpstreamURLSyntax(target); err != nil {
		return nil, err
	}
	return target, nil
}

func testProviderProxyConnection(parent context.Context, proxyURL, target *url.URL) error {
	ctx, cancel := context.WithTimeout(parent, providerProxyTestTimeout)
	defer cancel()
	proxyAddress := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddress = net.JoinHostPort(proxyURL.Hostname(), map[string]string{"http": "80", "https": "443"}[proxyURL.Scheme])
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return newProviderProxyTransportError("connect", err)
	}
	defer connection.Close()
	if strings.EqualFold(proxyURL.Scheme, "https") {
		tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: proxyURL.Hostname()})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return newProviderProxyTransportError("connect", err)
		}
		connection = tlsConnection
	}
	targetPort := target.Port()
	if targetPort == "" {
		targetPort = map[string]string{"http": "80", "https": "443"}[strings.ToLower(target.Scheme)]
	}
	targetAddress := net.JoinHostPort(target.Hostname(), targetPort)
	request := "CONNECT " + targetAddress + " HTTP/1.1\r\nHost: " + targetAddress + "\r\n"
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credential := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		request += "Proxy-Authorization: Basic " + credential + "\r\n"
	}
	request += "Proxy-Connection: close\r\n\r\n"
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if _, err := connection.Write([]byte(request)); err != nil {
		return newProviderProxyTransportError("connect", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return newProviderProxyTransportError("connect", err)
	}
	if response.StatusCode == http.StatusProxyAuthRequired {
		return newProviderProxyTransportError("auth", nil)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return newProviderProxyTransportError("connect", fmt.Errorf("proxy CONNECT returned status %s", strconv.Itoa(response.StatusCode)))
	}
	if strings.EqualFold(target.Scheme, "https") {
		tlsTarget := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.Hostname()})
		if err := tlsTarget.HandshakeContext(ctx); err != nil {
			return newProviderProxyTransportError("connect", err)
		}
	}
	return nil
}
