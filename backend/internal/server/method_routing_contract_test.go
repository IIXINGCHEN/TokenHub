package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMethodRoutingContracts(t *testing.T) {
	app := newTestServer()

	valid := methodRoutingRequest(app, http.MethodGet, "/livez", "")
	if valid.Code != http.StatusOK {
		t.Fatalf("GET /livez: expected 200, got %d: %s", valid.Code, valid.Body.String())
	}

	tests := []struct {
		name      string
		method    string
		path      string
		wantCode  string
		wantAllow string
	}{
		{name: "health wrong method", method: http.MethodPost, path: "/livez", wantCode: "method_not_allowed", wantAllow: ""},
		{name: "admin login wrong method", method: http.MethodGet, path: "/api/admin/auth/login", wantCode: "method_not_allowed", wantAllow: ""},
		{name: "gateway wrong method", method: http.MethodGet, path: "/v1/chat/completions", wantCode: "method_not_allowed", wantAllow: ""},
		{name: "models rejects head", method: http.MethodHead, path: "/v1/models", wantCode: "method_not_allowed", wantAllow: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := methodRoutingRequest(app, test.method, test.path, "")
			assertJSONError(t, response, http.StatusMethodNotAllowed, test.wantCode)
			assertAllowHeader(t, response, test.wantAllow)
		})
	}
}

func TestMethodRoutingPreservesAdminAuthenticationOrder(t *testing.T) {
	response := methodRoutingRequest(New(NewMemoryStore()).Handler(), http.MethodPost, "/api/admin/auth/me", "")
	assertJSONError(t, response, http.StatusUnauthorized, "invalid_admin_token")
	assertAllowHeader(t, response, "")
}

func TestMethodRoutingPreservesAnthropicErrorShape(t *testing.T) {
	response := methodRoutingRequest(newTestServer(), http.MethodGet, "/v1/messages", "")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/messages: expected 405, got %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("content-type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("GET /v1/messages: content type = %q, want application/json", contentType)
	}
	var payload struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode Anthropic method error: %v", err)
	}
	if payload.Type != "error" || payload.Error.Type != "invalid_request_error" || payload.Error.Code != "method_not_allowed" || payload.Error.Message == "" {
		t.Fatalf("unexpected Anthropic method error: %#v", payload)
	}
	assertRequestID(t, response, payload.RequestID)
	assertAllowHeader(t, response, "")
}

func TestMethodRoutingPreservesCORSPreflight(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/admin/auth/login", nil)
	request.Header.Set("origin", "https://console.example.com")
	request.Header.Set("access-control-request-method", http.MethodPost)
	request.Header.Set("access-control-request-headers", "authorization,content-type")
	response := httptest.NewRecorder()

	newTestServer().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/admin/auth/login: expected 204, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("access-control-allow-methods"); got != "GET,POST,PUT,PATCH,DELETE,OPTIONS" {
		t.Fatalf("access-control-allow-methods = %q", got)
	}
	if got := response.Header().Get("access-control-allow-headers"); got != "authorization,content-type" {
		t.Fatalf("access-control-allow-headers = %q", got)
	}
}

func methodRoutingRequest(handler http.Handler, method string, path string, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("expected %d, got %d: %s", wantStatus, response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("content-type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode method error: %v", err)
	}
	if payload.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, wantCode)
	}
	if payload.Error.Type != wantCode {
		t.Fatalf("error type = %q, want %q", payload.Error.Type, wantCode)
	}
	if payload.Error.Message == "" {
		t.Fatal("error message is empty")
	}
	assertRequestID(t, response, payload.RequestID)
}

func assertRequestID(t *testing.T, response *httptest.ResponseRecorder, bodyRequestID string) {
	t.Helper()
	if bodyRequestID == "" {
		t.Fatal("error body has no request_id")
	}
	if headerRequestID := response.Header().Get("x-request-id"); headerRequestID != bodyRequestID {
		t.Fatalf("body request_id = %q, header x-request-id = %q", bodyRequestID, headerRequestID)
	}
}

func assertAllowHeader(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	_, present := response.Header()[http.CanonicalHeaderKey("Allow")]
	if want == "" {
		if present {
			t.Fatalf("Allow header is present with value %q, want it absent", response.Header().Get("Allow"))
		}
		return
	}
	if !present {
		t.Fatalf("Allow header is absent, want %q", want)
	}
	if got := response.Header().Get("Allow"); got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}
