package tokenhub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tokenhub/backend/internal/server"
)

// TestProviderRequestsCarryAPIKey guards against server.Provider being
// marshalled directly: its APIKey field is tagged `json:"-"`, which would
// silently drop the resolved credential from create and update requests.
func TestProviderRequestsCarryAPIKey(t *testing.T) {
	var bodies []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider":{"id":"prov-1","name":"openai","type":"openai"}}`))
	}))
	defer ts.Close()

	client := NewAdminAPIClient(ts.URL, "test-admin-token", http.DefaultClient)
	provider := server.Provider{
		Name:    "openai",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-super-secret",
		Status:  server.StatusActive,
	}

	if _, err := client.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := client.UpdateProvider(context.Background(), "prov-1", provider); err != nil {
		t.Fatalf("update provider: %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(bodies))
	}
	for i, payload := range bodies {
		if got, _ := payload["api_key"].(string); got != "sk-super-secret" {
			t.Fatalf("request %d missing api_key, payload=%v", i, payload)
		}
		if createRoutes, ok := payload["create_routes"].(bool); !ok || createRoutes {
			t.Fatalf("request %d expected create_routes=false, payload=%v", i, payload)
		}
	}
}
