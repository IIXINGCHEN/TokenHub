package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCodexFingerprintModeForProvider(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]string
		want    codexFingerprintMode
	}{
		{name: "non-resource request stays off", options: nil, want: codexFingerprintOff},
		{name: "resource defaults to session", options: map[string]string{"resource_id": "rsrc_default"}, want: codexFingerprintSession},
		{name: "explicit off", options: map[string]string{"resource_id": "rsrc_off", codexFingerprintModeOption: "off"}, want: codexFingerprintOff},
		{name: "device", options: map[string]string{"resource_id": "rsrc_device", codexFingerprintModeOption: "device"}, want: codexFingerprintDevice},
		{name: "full", options: map[string]string{"resource_id": "rsrc_full", codexFingerprintModeOption: "full"}, want: codexFingerprintFull},
		{name: "unknown defaults to session", options: map[string]string{"resource_id": "rsrc_unknown", codexFingerprintModeOption: "unexpected"}, want: codexFingerprintSession},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := codexFingerprintModeForProvider(Provider{Options: test.options}); got != test.want {
				t.Fatalf("mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDeriveCodexFingerprintUUIDIsStableVersion4(t *testing.T) {
	first := deriveCodexFingerprintUUID("same-seed")
	if first != deriveCodexFingerprintUUID("same-seed") {
		t.Fatal("same seed produced different fingerprints")
	}
	if first == deriveCodexFingerprintUUID("different-seed") {
		t.Fatal("different seeds produced the same fingerprint")
	}
	parsed, err := uuid.Parse(first)
	if err != nil {
		t.Fatalf("parse fingerprint UUID: %v", err)
	}
	if parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
		t.Fatalf("unexpected UUID shape: version=%v variant=%v", parsed.Version(), parsed.Variant())
	}
}

func TestResolveCodexFingerprintIDsUsesAccountAndClientScopes(t *testing.T) {
	provider := Provider{Options: map[string]string{"resource_id": "rsrc_one"}}
	first := resolveCodexFingerprintIDs(provider, "client-session-one")
	second := resolveCodexFingerprintIDs(provider, "client-session-two")
	if first == nil || second == nil {
		t.Fatal("default session mode did not resolve fingerprints")
	}
	if first.installationID != second.installationID || first.sessionID != second.sessionID {
		t.Fatalf("account-scoped IDs drifted: first=%+v second=%+v", first, second)
	}
	if first.threadID == second.threadID {
		t.Fatalf("client sessions should derive distinct thread IDs: %+v", first)
	}
	if first.turnID == second.turnID {
		t.Fatalf("turn IDs should be unique: %+v", first)
	}

	fullProvider := Provider{Options: map[string]string{
		"resource_id":              "rsrc_one",
		codexFingerprintModeOption: "full",
	}}
	full := resolveCodexFingerprintIDs(fullProvider, "client-session-one")
	if full.threadID != full.sessionID {
		t.Fatalf("full mode should converge thread and session: %+v", full)
	}
}

func TestCodexFingerprintRewritesHeadersAndBodyWithSharedIDs(t *testing.T) {
	var observedHeader http.Header
	var observedBody map[string]any
	adapter := CodexSubscriptionAdapter{
		Client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			observedHeader = request.Header.Clone()
			if err := json.NewDecoder(request.Body).Decode(&observedBody); err != nil {
				t.Fatalf("decode upstream request: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		})},
		RefreshCredentials: func(context.Context, string, bool) (ProviderResourceCredentials, error) {
			return ProviderResourceCredentials{AccessToken: "access", AccountID: "account"}, nil
		},
	}
	provider := Provider{
		Type:    ProviderOpenAICodex,
		BaseURL: "http://localhost/backend-api/codex",
		Options: map[string]string{"resource_id": "rsrc_shared"},
	}
	var request ResponsesRequest
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-test",
		"input":[],
		"client_metadata":{
			"preserved":"body-value",
			"x-codex-turn-metadata":"{\"preserved\":\"embedded-value\",\"session_id\":\"original\"}"
		}
	}`), &request); err != nil {
		t.Fatal(err)
	}
	incoming := make(http.Header)
	incoming.Set("session-id", "client-session")
	incoming.Set("thread-id", "client-thread")
	incoming.Set("x-codex-turn-metadata", `{"preserved":"header-value","session_id":"original"}`)
	originalPayload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.OpenResponses(context.Background(), provider, "gpt-test", request, incoming)
	if err != nil {
		t.Fatalf("open Codex response: %v", err)
	}
	response.Body.Close()
	afterPayload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterPayload) != string(originalPayload) {
		t.Fatalf("fingerprint rewrite mutated the caller request: before=%s after=%s", originalPayload, afterPayload)
	}

	clientMetadata := observedBody["client_metadata"].(map[string]any)
	if clientMetadata["preserved"] != "body-value" {
		t.Fatalf("client metadata field was not preserved: %#v", clientMetadata)
	}
	sessionID := observedHeader.Get("session-id")
	threadID := observedHeader.Get("thread-id")
	turnID, _ := clientMetadata["turn_id"].(string)
	if sessionID == "" || threadID == "" || turnID == "" || sessionID == "client-session" || threadID == "client-thread" {
		t.Fatalf("fingerprints were not converged: headers=%#v metadata=%#v", observedHeader, clientMetadata)
	}
	if clientMetadata["session_id"] != sessionID || clientMetadata["thread_id"] != threadID {
		t.Fatalf("header/body IDs differ: headers=%#v metadata=%#v", observedHeader, clientMetadata)
	}
	if clientMetadata["x-codex-installation-id"] != observedHeader.Get("x-codex-installation-id") ||
		clientMetadata["x-codex-window-id"] != observedHeader.Get("x-codex-window-id") {
		t.Fatalf("header/body installation or window IDs differ: headers=%#v metadata=%#v", observedHeader, clientMetadata)
	}
	var headerTurnMetadata map[string]any
	if err := json.Unmarshal([]byte(observedHeader.Get("x-codex-turn-metadata")), &headerTurnMetadata); err != nil {
		t.Fatalf("decode header turn metadata: %v", err)
	}
	var bodyTurnMetadata map[string]any
	if err := json.Unmarshal([]byte(clientMetadata["x-codex-turn-metadata"].(string)), &bodyTurnMetadata); err != nil {
		t.Fatalf("decode body turn metadata: %v", err)
	}
	if headerTurnMetadata["preserved"] != "header-value" || bodyTurnMetadata["preserved"] != "embedded-value" {
		t.Fatalf("turn metadata fields were not preserved: header=%#v body=%#v", headerTurnMetadata, bodyTurnMetadata)
	}
	for _, key := range []string{"installation_id", "session_id", "thread_id", "turn_id", "window_id", "turn_started_at_unix_ms"} {
		if headerTurnMetadata[key] != bodyTurnMetadata[key] {
			t.Fatalf("turn metadata %s differs: header=%#v body=%#v", key, headerTurnMetadata, bodyTurnMetadata)
		}
	}
}

func TestCodexFingerprintHandlesNullMetadata(t *testing.T) {
	provider := Provider{Options: map[string]string{"resource_id": "rsrc_null_metadata"}}
	var request ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"gpt-test","input":[],"client_metadata":null}`), &request); err != nil {
		t.Fatal(err)
	}
	ids := prepareCodexFingerprintRequest(provider, nil, &request)
	if ids == nil {
		t.Fatal("default session mode did not resolve fingerprints")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	metadata, ok := decoded["client_metadata"].(map[string]any)
	if !ok || metadata["session_id"] != ids.sessionID {
		t.Fatalf("null client metadata was not replaced safely: %s", payload)
	}

	headers := make(http.Header)
	headers.Set("x-codex-turn-metadata", "null")
	applyCodexFingerprintHeaders(headers, ids)
	if headers.Get("x-codex-turn-metadata") != "null" {
		t.Fatalf("null turn metadata should remain unchanged: %#v", headers)
	}
}

func TestCodexFingerprintOffPreservesClientValues(t *testing.T) {
	provider := Provider{Options: map[string]string{
		"resource_id":              "rsrc_off",
		codexFingerprintModeOption: "off",
	}}
	var request ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"gpt-test","input":[],"client_metadata":{"session_id":"body-session"}}`), &request); err != nil {
		t.Fatal(err)
	}
	incoming := make(http.Header)
	incoming.Set("session-id", "header-session")
	if ids := prepareCodexFingerprintRequest(provider, incoming, &request); ids != nil {
		t.Fatalf("off mode returned IDs: %+v", ids)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"session_id":"body-session"`) {
		t.Fatalf("off mode rewrote the request: %s", payload)
	}
}

func TestCodexFingerprintDeviceModeOnlyRewritesInstallation(t *testing.T) {
	provider := Provider{Options: map[string]string{
		"resource_id":              "rsrc_device",
		codexFingerprintModeOption: "device",
	}}
	ids := resolveCodexFingerprintIDs(provider, "client-session")
	headers := make(http.Header)
	headers.Set("session-id", "client-session")
	headers.Set("thread-id", "client-thread")
	headers.Set("x-codex-turn-metadata", `{"installation_id":"client-installation","session_id":"client-session"}`)
	applyCodexFingerprintHeaders(headers, ids)

	if headers.Get("x-codex-installation-id") != ids.installationID {
		t.Fatalf("installation ID was not rewritten: %#v", headers)
	}
	if headers.Get("session-id") != "client-session" || headers.Get("thread-id") != "client-thread" {
		t.Fatalf("device mode changed session identifiers: %#v", headers)
	}
	var turnMetadata map[string]any
	if err := json.Unmarshal([]byte(headers.Get("x-codex-turn-metadata")), &turnMetadata); err != nil {
		t.Fatal(err)
	}
	if turnMetadata["installation_id"] != ids.installationID || turnMetadata["session_id"] != "client-session" {
		t.Fatalf("device mode rewrote the wrong turn metadata: %#v", turnMetadata)
	}
}
