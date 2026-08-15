package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminCodexImageCapabilityTestsOnceAndManagesRoute(t *testing.T) {
	imageBytes := realPNGFixture(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		var request codexSubscriptionImageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode capability request: %v", err)
			return
		}
		if r.URL.Path != "/backend-api/codex/images/generations" || request.Model != codexImageUpstreamModel ||
			request.Quality != "low" || request.Size != "1024x1024" || request.Prompt == "" {
			t.Errorf("unexpected capability request path=%s body=%+v", r.URL.Path, request)
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{{"b64_json": encodeBase64(imageBytes)}},
		})
	}))
	defer upstream.Close()

	store, server, resource := newCodexImageCapabilityTestServer(t, upstream.URL)
	server.codexSubscription.Client = upstream.Client()
	handler := server.Handler()
	responses := make(chan responseBody, 2)
	requestCapability := func(enabled bool) {
		payload, _ := json.Marshal(map[string]bool{"enabled": enabled})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", bytes.NewReader(payload))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("authorization", "Bearer dev_admin_token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		responses <- responseBody{Code: recorder.Code, Body: recorder.Body.String()}
	}

	go requestCapability(true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("capability test did not reach the real upstream")
	}
	go requestCapability(true)
	close(release)
	for range 2 {
		response := <-responses
		if response.Code != http.StatusOK {
			t.Fatalf("enable image capability: status=%d body=%s", response.Code, response.Body)
		}
	}
	if calls := upstreamCalls.Load(); calls != 1 {
		t.Fatalf("concurrent capability configuration sent %d upstream tests, want 1", calls)
	}
	routes := matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID)
	if len(routes) != 1 || routes[0].Status != StatusActive || routes[0].ProviderResourceID != "" {
		t.Fatalf("expected one active provider-level Codex image route, got %+v", routes)
	}
	updated, ok := store.GetProviderResource(resource.ID)
	if !ok || updated.Options[codexImageCapabilityOption] != codexImageCapabilitySupported ||
		updated.Options[codexImageCapabilityCheckedAtOption] == "" {
		t.Fatalf("successful capability was not recorded: %+v", updated.Options)
	}

	disabled := doJSON(t, handler, http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", map[string]bool{"enabled": false}, "")
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable image capability: status=%d body=%s", disabled.Code, disabled.Body)
	}
	routes = matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID)
	if len(routes) != 1 || routes[0].Status != StatusDisabled {
		t.Fatalf("disable must retain exactly one disabled route: %+v", routes)
	}
	updated, _ = store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != codexImageCapabilitySupported {
		t.Fatalf("disable must preserve the tested capability: %+v", updated.Options)
	}
}

func TestAdminCodexImageCapabilityDisablesRouteAfterLastAccountDeleted(t *testing.T) {
	imageBytes := realPNGFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{{"b64_json": encodeBase64(imageBytes)}},
		})
	}))
	defer upstream.Close()

	store, server, resource := newCodexImageCapabilityTestServer(t, upstream.URL)
	server.codexSubscription.Client = upstream.Client()
	handler := server.Handler()
	enabled := doJSON(t, handler, http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", map[string]bool{"enabled": true}, "")
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable image capability: status=%d body=%s", enabled.Code, enabled.Body)
	}

	deleted := doJSON(t, handler, http.MethodDelete, "/api/admin/provider-resources/"+resource.ID, nil, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete final Codex account: status=%d body=%s", deleted.Code, deleted.Body)
	}
	routes := matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID)
	if len(routes) != 1 || routes[0].Status != StatusDisabled {
		t.Fatalf("deleting the final Codex account must disable its image route: %+v", routes)
	}
}

func TestAdminCodexImageCapabilityClassifiesUnsupportedWithoutRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "not available"}})
	}))
	defer upstream.Close()
	store, server, resource := newCodexImageCapabilityTestServer(t, upstream.URL)
	server.codexSubscription.Client = upstream.Client()

	response := doJSON(t, server.Handler(), http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", map[string]bool{"enabled": true}, "")
	if response.Code != http.StatusForbidden || !bytes.Contains([]byte(response.Body), []byte(`"code":"codex_image_forbidden"`)) {
		t.Fatalf("unsupported capability: status=%d body=%s", response.Code, response.Body)
	}
	updated, _ := store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != codexImageCapabilityUnsupported ||
		updated.Options[codexImageCapabilityCheckedAtOption] == "" {
		t.Fatalf("unsupported capability was not recorded: %+v", updated.Options)
	}
	if routes := matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID); len(routes) != 0 {
		t.Fatalf("unsupported capability created routes: %+v", routes)
	}
}

func TestAdminCodexImageCapabilityLeavesTransientFailureRetryable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limited"}})
	}))
	defer upstream.Close()
	store, server, resource := newCodexImageCapabilityTestServer(t, upstream.URL)
	server.codexSubscription.Client = upstream.Client()

	response := doJSON(t, server.Handler(), http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", map[string]bool{"enabled": true}, "")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("transient capability failure: status=%d body=%s", response.Code, response.Body)
	}
	updated, _ := store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != "" || updated.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("transient failure must not become unsupported: %+v", updated.Options)
	}
	if routes := matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID); len(routes) != 0 {
		t.Fatalf("transient failure created routes: %+v", routes)
	}
}

func TestAdminCodexImageCapabilityRequiresReauthorizationWithoutRoute(t *testing.T) {
	store, server, resource := newCodexImageCapabilityTestServer(t, "http://127.0.0.1:1")
	server.codexSubscription.RefreshCredentials = func(context.Context, string, bool) (ProviderResourceCredentials, error) {
		return ProviderResourceCredentials{}, NewHTTPError(http.StatusUnauthorized, "provider_resource_reauthorization_required", "OpenAI account session ended; reauthorization is required")
	}

	response := doJSON(t, server.Handler(), http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-capability", map[string]bool{"enabled": true}, "")
	if response.Code != http.StatusUnauthorized || !bytes.Contains([]byte(response.Body), []byte(`"code":"provider_resource_reauthorization_required"`)) {
		t.Fatalf("reauthorization failure: status=%d body=%s", response.Code, response.Body)
	}
	updated, _ := store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("reauthorization failure changed capability: %+v", updated.Options)
	}
	if routes := matchingCodexImageRoutes(store.ListRoutes(), resource.ProviderID); len(routes) != 0 {
		t.Fatalf("reauthorization failure created routes: %+v", routes)
	}
}

func TestCodexImageRouteBackfillPreservesDisabledRoute(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID: "prv_codex_backfill", Name: "Codex Backfill", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true,
	})
	if _, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_codex_backfill", ProviderID: provider.ID, Name: "Codex Backfill Account",
		ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Options: map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported},
	}); err != nil {
		t.Fatal(err)
	}
	server := NewWithConfig(store, Config{AdminToken: "dev_admin_token", SecretKey: "codex-image-backfill-secret"})
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes := matchingCodexImageRoutes(store.ListRoutes(), provider.ID)
	if len(routes) != 1 || routes[0].Status != StatusActive {
		t.Fatalf("supported account was not backfilled: %+v", routes)
	}
	routes[0].Status = StatusDisabled
	if _, err := store.UpdateRoute(routes[0].ID, routes[0]); err != nil {
		t.Fatal(err)
	}
	restarted := NewWithConfig(store, Config{AdminToken: "dev_admin_token", SecretKey: "codex-image-backfill-secret"})
	if err := restarted.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes = matchingCodexImageRoutes(store.ListRoutes(), provider.ID)
	if len(routes) != 1 || routes[0].Status != StatusDisabled {
		t.Fatalf("backfill re-enabled or duplicated an explicitly disabled route: %+v", routes)
	}
	resource, _ := store.GetProviderResource("rsrc_codex_backfill")
	if resource.Options[codexImageRouteBackfillOption] != codexImageRouteBackfillCompleted {
		t.Fatalf("backfill completion was not recorded: %+v", resource.Options)
	}
	if err := store.DeleteRoute(routes[0].ID); err != nil {
		t.Fatal(err)
	}
	afterDeletion := NewWithConfig(store, Config{AdminToken: "dev_admin_token", SecretKey: "codex-image-backfill-secret"})
	if err := afterDeletion.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if routes := matchingCodexImageRoutes(store.ListRoutes(), provider.ID); len(routes) != 0 {
		t.Fatalf("one-time backfill recreated an explicitly deleted route: %+v", routes)
	}
}

func newCodexImageCapabilityTestServer(t *testing.T, baseURL string) (*GormStore, *Server, ProviderResource) {
	t.Helper()
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID: "prv_codex_image_capability", Name: "Codex Image Capability", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true,
	})
	resource, err := store.AddProviderResource(ProviderResource{
		ID:           "rsrc_codex_image_capability",
		ProviderID:   provider.ID,
		Name:         "Codex Image Capability Account",
		ResourceType: ProviderResourceOpenAISubscription,
		BaseURL:      baseURL + "/backend-api/codex",
		Status:       StatusActive,
		Healthy:      true,
		Credentials: &ProviderResourceCredentials{
			AccessToken: "access_capability_test",
			AccountID:   "account_capability_test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithConfig(store, Config{AdminToken: "dev_admin_token", SecretKey: "codex-image-capability-secret"})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return store, server, resource
}

func matchingCodexImageRoutes(routes []ModelRoute, providerID string) []ModelRoute {
	var matches []ModelRoute
	for _, route := range routes {
		if codexImageRouteMatches(route, providerID) {
			matches = append(matches, route)
		}
	}
	return matches
}
