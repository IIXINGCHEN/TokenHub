package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestAdminCodexImageCapabilityTestDoesNotPersistAcrossReauthorization(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"b64_json": encodeBase64(realPNGFixture(t))}}})
	}))
	defer upstream.Close()
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_reauth", Name: "Image Reauthorization", Type: ProviderOpenAICodex, BaseURL: upstream.URL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_reauth", ProviderID: provider.ID, Name: "Image Reauthorization Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: upstream.URL, Status: StatusActive, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "old-image-access", RefreshToken: "old-image-refresh", AccountID: "old-image-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithConfig(store, Config{AdminToken: "admin-image-token", SecretKey: "image-reauthorization-secret"})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	server.codexSubscription.Client = upstream.Client()
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-resources/"+resource.ID+"/image-test", nil)
		req.Header.Set("authorization", "Bearer admin-image-token")
		resp := httptest.NewRecorder()
		server.Handler().ServeHTTP(resp, req)
		response <- resp
	}()
	<-started
	if _, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "old-image-access", RefreshToken: "old-image-refresh", AccountID: "new-image-account"},
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	resp := <-response
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "image_capability_credentials_changed") {
		t.Fatalf("stale image test result was accepted: status=%d body=%s", resp.Code, resp.Body.String())
	}
	updated, _ := store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != "" || updated.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("stale image test changed new credentials capability: %+v", updated.Options)
	}
}

func TestCodexImageBoundRequestUsesOneCredentialAndEndpointSnapshot(t *testing.T) {
	var authorization string
	var accountID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		accountID = r.Header.Get("ChatGPT-Account-ID")
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"b64_json": encodeBase64(realPNGFixture(t))}}})
	}))
	defer upstream.Close()
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_snapshot", Name: "Image Snapshot", Type: ProviderOpenAICodex, BaseURL: upstream.URL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_snapshot", ProviderID: provider.ID, Name: "Image Snapshot Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: upstream.URL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "snapshot-old-access", RefreshToken: "snapshot-old-refresh", AccountID: "snapshot-old-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	boundProvider, boundResource, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: "https://chatgpt.com/backend-api/codex-new", Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "snapshot-new-access", RefreshToken: "snapshot-new-refresh", AccountID: "snapshot-new-account"},
	}); err != nil {
		t.Fatal(err)
	}
	server := NewWithConfig(store, Config{SecretKey: "image-snapshot-secret"})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	server.codexSubscription.Client = upstream.Client()
	_, _, _, err = server.executeCodexSubscriptionImageBound(context.Background(), RouteSelection{
		Provider: boundProvider, Resource: &boundResource, ProviderModel: codexImageUpstreamModel,
	}, ImageJob{Model: codexImageModelName, Action: "generation", Prompt: "one white square"}, binding, false)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer snapshot-old-access" || accountID != "snapshot-old-account" {
		t.Fatalf("mixed request binding: authorization=%q account_id=%q", authorization, accountID)
	}
}

func TestCodexImageExecutionRequiresCapabilityOnFreshSnapshot(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"b64_json": encodeBase64(realPNGFixture(t))}}})
	}))
	defer upstream.Close()
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_fresh", Name: "Image Fresh", Type: ProviderOpenAICodex, BaseURL: upstream.URL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_fresh", ProviderID: provider.ID, Name: "Image Fresh Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: upstream.URL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "fresh-old-access", RefreshToken: "fresh-old-refresh", AccountID: "fresh-old-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	routeResource, _ := store.GetProviderResource(resource.ID)
	if _, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: upstream.URL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "fresh-new-access", RefreshToken: "fresh-new-refresh", AccountID: "fresh-new-account"},
	}); err != nil {
		t.Fatal(err)
	}
	server := NewWithConfig(store, Config{SecretKey: "image-fresh-secret"})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	server.codexSubscription.Client = upstream.Client()
	_, _, _, err = server.executeCodexSubscriptionImage(context.Background(), RouteSelection{Provider: provider, Resource: &routeResource}, ImageJob{
		Model: codexImageModelName, Action: "generation", Prompt: "one white square",
	})
	if err == nil || AsHTTPError(err).Code != "image_capability_not_verified" {
		t.Fatalf("error = %v, want image_capability_not_verified", err)
	}
	if called {
		t.Fatal("untested replacement credentials reached the image upstream")
	}
}

func TestCodexImageExecutionRefreshesCompleteBindingAfterUnauthorized(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := upstreamCalls.Add(1)
		if call == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer image-expired-access" {
				t.Errorf("first authorization = %q", got)
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "unauthorized"}})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer image-refreshed-access" {
			t.Errorf("retried authorization = %q", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"b64_json": encodeBase64(realPNGFixture(t))}}})
	}))
	defer upstream.Close()
	var tokenCalls atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "image-refreshed-access", "refresh_token": "image-refreshed-refresh", "expires_in": 3600,
		})
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_refresh", Name: "Image Refresh", Type: ProviderOpenAICodex, BaseURL: upstream.URL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_refresh", ProviderID: provider.ID, Name: "Image Refresh Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: upstream.URL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{
			AccessToken: "image-expired-access", RefreshToken: "image-refresh-token", AccountID: "image-refresh-account",
			ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	routeResource, _ := store.GetProviderResource(resource.ID)
	server := NewWithConfig(store, Config{SecretKey: "image-refresh-secret"})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	server.codexSubscription.Client = upstream.Client()
	if _, _, _, err := server.executeCodexSubscriptionImage(context.Background(), RouteSelection{Provider: provider, Resource: &routeResource}, ImageJob{
		Model: codexImageModelName, Action: "generation", Prompt: "one white square",
	}); err != nil {
		t.Fatal(err)
	}
	if upstreamCalls.Load() != 2 || tokenCalls.Load() != 1 {
		t.Fatalf("upstream calls=%d token calls=%d", upstreamCalls.Load(), tokenCalls.Load())
	}
}

func TestImageCapabilityCompareAndSetDoesNotOverwriteAnotherStore(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "image-capability-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_image_cas", Name: "Image CAS", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_image_cas", ProviderID: provider.ID, Name: "Image CAS Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "cas-access", RefreshToken: "cas-refresh", AccountID: "cas-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := storeA.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx := storeB.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	if err := tx.Model(&ProviderResource{}).Where("id = ?", resource.ID).Update("base_url", "https://chatgpt.com/backend-api/codex-new").Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	type result struct {
		updated bool
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		updated, updateErr := storeA.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now())
		resultCh <- result{updated: updated, err: updateErr}
	}()
	time.Sleep(25 * time.Millisecond)
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	resultValue := <-resultCh
	if resultValue.updated {
		t.Fatalf("stale capability compare-and-set succeeded across stores: %v", resultValue.err)
	}
	stored, _ := storeB.GetProviderResource(resource.ID)
	if stored.BaseURL != "https://chatgpt.com/backend-api/codex-new" || stored.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("capability write overwrote concurrent configuration: %+v", stored)
	}
}

func TestImageCapabilityBindingIgnoresModelCatalogRefresh(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_catalog", Name: "Image Catalog", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_catalog", ProviderID: provider.ID, Name: "Image Catalog Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "catalog-access", RefreshToken: "catalog-refresh", AccountID: "catalog-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{
		codexResourceModelsFetchedAtOption: time.Now().UTC().Format(time.RFC3339Nano),
		codexResourceModelsETagOption:      "new-catalog-etag",
		codexResourceModelCatalogOption:    `[{"id":"gpt-5.6-sol"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("catalog-only refresh invalidated image test: updated=%v err=%v", updated, err)
	}
	credentials := ProviderResourceCredentials{AuthType: "oauth", AccessToken: "catalog-access", AccountID: "catalog-account", Email: "old@example.com"}
	metadataChanged := credentials
	metadataChanged.Email = "presentation-only@example.com"
	if providerResourceImageCapabilityIdentityVersion(metadataChanged) != providerResourceImageCapabilityIdentityVersion(credentials) {
		t.Fatal("presentation-only credential metadata invalidated image capability identity")
	}
}

func TestProviderResourceEditPreservesManagedStateWrittenByAnotherStore(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "provider-resource-managed-state-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_managed_race", Name: "Managed Race", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_managed_race", ProviderID: provider.ID, Name: "Managed Race Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "managed-access", RefreshToken: "managed-refresh", AccountID: "managed-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_provider_resource_initial_read"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "provider_resources" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	updateErr := make(chan error, 1)
	go func() {
		_, updateError := storeA.UpdateProviderResource(resource.ID, ProviderResource{
			Name: "Managed Race Renamed", ResourceType: resource.ResourceType, BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true,
			Credentials: &ProviderResourceCredentials{Email: "metadata-update@example.com"},
		})
		updateErr <- updateError
	}()
	<-loaded
	if _, err := storeB.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "managed-new-access", RefreshToken: "managed-new-refresh", AccountID: "managed-account"},
	}); err != nil {
		close(release)
		t.Fatal(err)
	}
	if _, err := storeB.UpdateProviderResourceOptions(resource.ID, map[string]string{
		codexResourceSupportedModelsOption: `["gpt-5.6-sol"]`,
		codexResourceModelsFetchedAtOption: "2026-08-14T15:00:00Z",
		codexResourceModelsETagOption:      "catalog-etag",
		codexResourceModelCatalogOption:    `[{"id":"gpt-5.6-sol"}]`,
	}); err != nil {
		close(release)
		t.Fatal(err)
	}
	cooldownUntil := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Millisecond)
	lastUsedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if err := storeB.db.Model(&ProviderResource{}).Where("id = ?", resource.ID).Updates(map[string]any{
		"failure_count": 7, "cooldown_until": &cooldownUntil, "last_used_at": &lastUsedAt,
	}).Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	_, _, binding, err := storeB.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	if updated, err := storeB.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		close(release)
		t.Fatalf("concurrent managed-state write: updated=%v err=%v", updated, err)
	}
	close(release)
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
	stored, _ := storeB.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != codexImageCapabilitySupported {
		t.Fatalf("ordinary edit erased concurrent managed state: %+v", stored.Options)
	}
	if stored.Options[codexResourceModelsETagOption] != "catalog-etag" || stored.Options[codexResourceSupportedModelsOption] != `["gpt-5.6-sol"]` {
		t.Fatalf("ordinary edit erased concurrent model catalog: %+v", stored.Options)
	}
	if stored.FailureCount != 7 || stored.CooldownUntil == nil || !stored.CooldownUntil.Equal(cooldownUntil) || stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("ordinary edit erased concurrent runtime state: %+v", stored)
	}
	credentials, err := storeB.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "managed-new-access" || credentials.RefreshToken != "managed-new-refresh" {
		t.Fatalf("ordinary edit restored stale credentials: %+v", credentials)
	}
	if credentials.Email != "metadata-update@example.com" {
		t.Fatalf("ordinary edit lost metadata credential patch: %+v", credentials)
	}
}

func TestProviderResourceUpsertPreservesServerManagedAndRuntimeState(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "provider-resource-upsert-state.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_upsert_state", Name: "Upsert State", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_upsert_state", ProviderID: provider.ID, Name: "Upsert State Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "upsert-access", RefreshToken: "upsert-refresh", AccountID: "upsert-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storeB.UpdateProviderResourceOptions(resource.ID, map[string]string{
		codexImageCapabilityOption:      codexImageCapabilitySupported,
		codexResourceModelsETagOption:   "upsert-etag",
		codexResourceModelCatalogOption: `[{"id":"gpt-5.6-sol"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	cooldownUntil := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Millisecond)
	if err := storeB.db.Model(&ProviderResource{}).Where("id = ?", resource.ID).Updates(map[string]any{
		"failure_count": 4, "cooldown_until": &cooldownUntil,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := storeA.AddProviderResource(ProviderResource{
		ID: resource.ID, ProviderID: provider.ID, Name: "Upsert State Renamed", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
	}); err != nil {
		t.Fatal(err)
	}
	stored, _ := storeB.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != codexImageCapabilitySupported || stored.Options[codexResourceModelsETagOption] != "upsert-etag" {
		t.Fatalf("upsert erased server-managed options: %+v", stored.Options)
	}
	if stored.FailureCount != 4 || stored.CooldownUntil == nil || !stored.CooldownUntil.Equal(cooldownUntil) {
		t.Fatalf("upsert erased runtime state: %+v", stored)
	}
	credentials, err := storeB.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "upsert-access" || credentials.RefreshToken != "upsert-refresh" {
		t.Fatalf("upsert erased credentials: %+v", credentials)
	}
}

func TestStaleProviderResourceEditClearsCapabilityForConcurrentEndpointTest(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "resource-endpoint-capability-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_resource_endpoint_race", Name: "Resource Endpoint Race", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_resource_endpoint_race", ProviderID: provider.ID, Name: "Resource Endpoint Race Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "endpoint-access", RefreshToken: "endpoint-refresh", AccountID: "endpoint-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_stale_resource_endpoint_edit"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "provider_resources" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	updateErr := make(chan error, 1)
	go func() {
		_, staleErr := storeA.UpdateProviderResource(resource.ID, ProviderResource{
			Name: "Stale Resource Edit", ResourceType: resource.ResourceType, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		})
		updateErr <- staleErr
	}()
	<-loaded
	newBaseURL := "https://chatgpt.com/backend-api/codex-new"
	if _, err := storeB.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: newBaseURL, Status: StatusActive, Healthy: true,
	}); err != nil {
		close(release)
		t.Fatal(err)
	}
	_, _, binding, err := storeB.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	if updated, err := storeB.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		close(release)
		t.Fatalf("mark concurrent endpoint supported: updated=%v err=%v", updated, err)
	}
	close(release)
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
	stored, _ := storeB.GetProviderResource(resource.ID)
	if stored.BaseURL != openAICodexBaseURL || stored.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("stale endpoint edit retained another binding's capability: %+v", stored)
	}
}

func TestStaleProviderEditClearsCapabilityForConcurrentProviderBinding(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_stale_provider", Name: "Stale Provider", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_stale_provider", ProviderID: provider.ID, Name: "Stale Provider Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "provider-access", RefreshToken: "provider-refresh", AccountID: "provider-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProvider(provider.ID, Provider{Name: provider.Name, Type: provider.Type, BaseURL: "https://chatgpt.com/backend-api/codex-new", Status: StatusActive, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark provider binding supported: updated=%v err=%v", updated, err)
	}
	if _, err := store.UpdateProvider(provider.ID, Provider{Name: "Stale Name Edit", Type: provider.Type, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("stale provider binding retained capability: %+v", stored.Options)
	}
}

func TestImageCapabilityBindingIncludesNormalizedCodexHostAllowlist(t *testing.T) {
	provider := Provider{ID: "prv_allowed_hosts", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Options: map[string]string{"allowed_codex_hosts": " B.example, a.example "}}
	resource := ProviderResource{ID: "rsrc_allowed_hosts", ProviderID: provider.ID, ResourceType: ProviderResourceOpenAISubscription, BaseURL: openAICodexBaseURL}
	credentials := ProviderResourceCredentials{AccessToken: "allowed-access", RefreshToken: "allowed-refresh", AccountID: "allowed-account"}
	first := providerResourceImageCapabilityBindingVersion(provider, resource, credentials)
	provider.Options["allowed_codex_hosts"] = "a.example,b.example"
	if second := providerResourceImageCapabilityBindingVersion(provider, resource, credentials); second != first {
		t.Fatalf("equivalent allowlists produced different bindings: %s != %s", first, second)
	}
	provider.Options["allowed_codex_hosts"] = "c.example"
	if third := providerResourceImageCapabilityBindingVersion(provider, resource, credentials); third == first {
		t.Fatal("allowlist change did not invalidate image capability binding")
	}
	resource.Options = map[string]string{"allowed_codex_hosts": "resource.example"}
	resourceBinding := providerResourceImageCapabilityBindingVersion(provider, resource, credentials)
	resource.Options["allowed_codex_hosts"] = "resource-two.example"
	if providerResourceImageCapabilityBindingVersion(provider, resource, credentials) == resourceBinding {
		t.Fatal("resource allowlist override did not invalidate image capability binding")
	}
}

func TestConcurrentProviderResourceInsertWithSameIDIsIdempotent(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "provider-resource-concurrent-insert.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_concurrent_insert", Name: "Concurrent Insert", Type: ProviderOpenAICompatible, BaseURL: "https://api.openai.com/v1", Status: StatusActive, Healthy: true})
	start := make(chan struct{})
	results := make(chan error, 2)
	insert := func(store *GormStore, name string) {
		<-start
		_, err := store.AddProviderResource(ProviderResource{
			ID: "rsrc_concurrent_insert", ProviderID: provider.ID, Name: name, ResourceType: ProviderResourceAPIKey,
			BaseURL: provider.BaseURL, APIKey: "concurrent-key", Status: StatusActive, Healthy: true,
		})
		results <- err
	}
	go insert(storeA, "Concurrent Insert A")
	go insert(storeB, "Concurrent Insert B")
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	stored, ok := storeA.GetProviderResource("rsrc_concurrent_insert")
	if !ok || stored.ID == "" {
		t.Fatal("concurrent insert did not persist resource")
	}
}

func TestProviderUpsertInvalidatesDependentImageCapability(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_provider_upsert", Name: "Provider Upsert", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_provider_upsert", ProviderID: provider.ID, Name: "Provider Upsert Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "provider-upsert-access", RefreshToken: "provider-upsert-refresh", AccountID: "provider-upsert-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	store.AddProvider(Provider{
		ID: provider.ID, Name: provider.Name, Type: provider.Type, BaseURL: "https://chatgpt.com/backend-api/codex-new",
		Status: StatusActive, Healthy: true,
	})
	stored, _ := store.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("provider upsert retained stale image capability: %+v", stored.Options)
	}
}

func TestProviderResourceImportCannotForgeOrEraseManagedState(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_import_managed", Name: "Import Managed", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	result, err := store.ImportProviderResources([]ProviderResource{{
		ID: "rsrc_import_injection", ProviderID: provider.ID, Name: "Import Injection", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Options:     map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported},
		Credentials: &ProviderResourceCredentials{AccessToken: "import-access", RefreshToken: "import-refresh", AccountID: "import-account"},
	}})
	if err != nil || result.Success != 1 {
		t.Fatalf("new import: result=%+v err=%v", result, err)
	}
	created, _ := store.GetProviderResource("rsrc_import_injection")
	if created.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("new import forged image capability: %+v", created.Options)
	}
	if _, err := store.UpdateProviderResourceOptions(created.ID, map[string]string{
		openAIAccountReauthorizationRequiredOption: "true",
		codexImageCapabilityOption:                 codexImageCapabilitySupported,
		codexResourceModelsETagOption:              "import-etag",
	}); err != nil {
		t.Fatal(err)
	}
	cooldown := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Millisecond)
	if err := store.db.Model(&ProviderResource{}).Where("id = ?", created.ID).Updates(map[string]any{"failure_count": 3, "cooldown_until": &cooldown}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = store.ImportProviderResources([]ProviderResource{{
		ID: created.ID, ProviderID: provider.ID, Name: "Import Injection Updated", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: "https://chatgpt.com/backend-api/codex-new", Status: StatusActive, Healthy: true,
		Options: map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported},
	}})
	if err != nil || result.Success != 1 {
		t.Fatalf("existing import: result=%+v err=%v", result, err)
	}
	stored, _ := store.GetProviderResource(created.ID)
	if stored.Options[codexImageCapabilityOption] != "" || stored.Options[openAIAccountReauthorizationRequiredOption] != "true" || stored.Options[codexResourceModelsETagOption] != "import-etag" {
		t.Fatalf("existing import corrupted managed state: %+v", stored.Options)
	}
	if stored.FailureCount != 3 || stored.CooldownUntil == nil || !stored.CooldownUntil.Equal(cooldown) {
		t.Fatalf("existing import corrupted runtime state: %+v", stored)
	}
	credentials, err := store.RefreshProviderResourceCredentials(context.Background(), created.ID, false)
	if err == nil || AsHTTPError(err).Code != "provider_resource_reauthorization_required" || credentials.AccessToken != "import-access" {
		t.Fatalf("existing import corrupted credentials or reauthorization state: credentials=%+v err=%v", credentials, err)
	}
}

func TestProviderResourceUpsertMoveClearsOldProviderCapability(t *testing.T) {
	store := NewMemoryStore()
	providerA := store.AddProvider(Provider{ID: "prv_move_a", Name: "Move A", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	providerB := store.AddProvider(Provider{ID: "prv_move_b", Name: "Move B", Type: ProviderOpenAICodex, BaseURL: "https://chatgpt.com/backend-api/codex-new", Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_provider_move", ProviderID: providerA.ID, Name: "Provider Move Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "move-access", RefreshToken: "move-refresh", AccountID: "move-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	if _, err := store.AddProviderResource(ProviderResource{
		ID: resource.ID, ProviderID: providerB.ID, Name: resource.Name, ResourceType: resource.ResourceType,
		BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true,
	}); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetProviderResource(resource.ID)
	if stored.ProviderID != providerB.ID || stored.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("provider move retained old capability: %+v", stored)
	}
}

func TestProviderResourceImportMergesEmptyAndMetadataCredentialPatches(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_import_credentials", Name: "Import Credentials", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_import_credentials", ProviderID: provider.ID, Name: "Import Credentials Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "preserved-access", RefreshToken: "preserved-refresh", AccountID: "preserved-account", Email: "old@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, patch := range []*ProviderResourceCredentials{{}, {Email: "new@example.com"}} {
		result, err := store.ImportProviderResources([]ProviderResource{{
			ID: resource.ID, ProviderID: provider.ID, Name: resource.Name, ResourceType: resource.ResourceType,
			BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true, Credentials: patch,
		}})
		if err != nil || result.Success != 1 {
			t.Fatalf("credential patch %d: result=%+v err=%v", index, result, err)
		}
	}
	credentials, err := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "preserved-access" || credentials.RefreshToken != "preserved-refresh" || credentials.Email != "new@example.com" {
		t.Fatalf("partial import erased or failed to merge credentials: %+v", credentials)
	}
	if _, err := store.AddProviderResource(ProviderResource{
		ID: resource.ID, ProviderID: provider.ID, Name: resource.Name, ResourceType: resource.ResourceType,
		BaseURL: resource.BaseURL, APIKey: "legacy-new-access", Status: StatusActive, Healthy: true,
	}); err != nil {
		t.Fatal(err)
	}
	credentials, err = store.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "legacy-new-access" || credentials.RefreshToken != "preserved-refresh" {
		t.Fatalf("legacy access-token patch erased renewable credentials: %+v", credentials)
	}
}

func TestProviderResourceImportRejectsCodexCustomHeaders(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_import_headers", Name: "Import Headers", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	result, err := store.ImportProviderResources([]ProviderResource{{
		ID: "rsrc_import_headers", ProviderID: provider.ID, Name: "Import Headers Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true, Headers: map[string]string{"X-Custom": "rejected"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success != 0 || result.Failed != 1 {
		t.Fatalf("Codex custom headers were accepted: %+v", result)
	}
}

func TestProviderResourceAddRevalidatesLockedDestinationProvider(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "resource-provider-validation-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_validation_race", Name: "Validation Race", Type: ProviderOpenAICompatible, BaseURL: "https://api.openai.com/v1", Status: StatusActive, Healthy: true})
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_resource_add_after_provider_read"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "providers" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	addErr := make(chan error, 1)
	go func() {
		_, err := storeA.AddProviderResource(ProviderResource{
			ID: "rsrc_validation_race", ProviderID: provider.ID, Name: "Validation Race Account", ResourceType: ProviderResourceOpenAISubscription,
			BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true, Headers: map[string]string{"X-Custom": "forbidden"},
		})
		addErr <- err
	}()
	<-loaded
	if _, err := storeB.UpdateProvider(provider.ID, Provider{Name: provider.Name, Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-addErr; err == nil || AsHTTPError(err).Code != "provider_headers_unsupported" {
		t.Fatalf("resource add accepted configuration invalid for locked Provider: %v", err)
	}
}

func TestProviderAddStripsAdapterForbiddenHeaders(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID: "prv_add_headers", Name: "Add Headers", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL,
		Status: StatusActive, Healthy: true, Headers: map[string]string{"X-Custom": "forbidden"},
	})
	if len(provider.Headers) != 0 {
		t.Fatalf("Provider create retained forbidden headers: %+v", provider.Headers)
	}
	provider = store.AddProvider(Provider{
		ID: provider.ID, Name: provider.Name, Type: provider.Type, BaseURL: provider.BaseURL,
		Status: StatusActive, Healthy: true, Headers: map[string]string{"X-Custom": "still-forbidden"},
	})
	if len(provider.Headers) != 0 {
		t.Fatalf("Provider upsert retained forbidden headers: %+v", provider.Headers)
	}
}

func TestProviderResourceUpdateRevalidatesLockedProvider(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "resource-update-provider-validation-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_update_validation_race", Name: "Update Validation Race", Type: ProviderOpenAICompatible, BaseURL: "https://api.openai.com/v1", Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_update_validation_race", ProviderID: provider.ID, Name: "Update Validation Race Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "validation-access", RefreshToken: "validation-refresh", AccountID: "validation-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_resource_update_after_initial_read"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "provider_resources" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	updateErr := make(chan error, 1)
	go func() {
		_, err := storeA.UpdateProviderResource(resource.ID, ProviderResource{
			Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true,
			Headers: map[string]string{"X-Custom": "forbidden-after-provider-change"},
		})
		updateErr <- err
	}()
	<-loaded
	if _, err := storeB.UpdateProvider(provider.ID, Provider{Name: provider.Name, Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-updateErr; err == nil || AsHTTPError(err).Code != "provider_headers_unsupported" {
		t.Fatalf("resource update accepted configuration invalid for locked Provider: %v", err)
	}
}

func TestProviderResourceRejectsConflictingLegacyAndStructuredAccessTokens(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_credential_conflict", Name: "Credential Conflict", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	payload := ProviderResource{
		ID: "rsrc_credential_conflict", ProviderID: provider.ID, Name: "Credential Conflict Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, APIKey: "legacy-access", Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "structured-access", RefreshToken: "structured-refresh", AccountID: "structured-account"},
	}
	if _, err := store.AddProviderResource(payload); err == nil || AsHTTPError(err).Code != "provider_resource_credential_conflict" {
		t.Fatalf("fresh create accepted conflicting access tokens: %v", err)
	}
	payload.APIKey = "structured-access"
	resource, err := store.AddProviderResource(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	matchingWhitespace := payload
	matchingWhitespace.APIKey = "  structured-access  "
	matchingWhitespace.Credentials = &ProviderResourceCredentials{
		AccessToken: "structured-access", RefreshToken: "structured-refresh", AccountID: "structured-account",
	}
	if _, err := store.AddProviderResource(matchingWhitespace); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResource(resource.ID, matchingWhitespace); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != codexImageCapabilitySupported {
		t.Fatalf("canonical equivalent token cleared capability: %+v", stored.Options)
	}
	credentials, err := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "structured-access" {
		t.Fatalf("access token was not canonicalized: %q", credentials.AccessToken)
	}
	payload.ID = resource.ID
	payload.APIKey = "different-legacy-access"
	if _, err := store.AddProviderResource(payload); err == nil || AsHTTPError(err).Code != "provider_resource_credential_conflict" {
		t.Fatalf("upsert accepted conflicting access tokens: %v", err)
	}
	if _, err := store.UpdateProviderResource(resource.ID, payload); err == nil || AsHTTPError(err).Code != "provider_resource_credential_conflict" {
		t.Fatalf("update accepted conflicting access tokens: %v", err)
	}
}

func TestImageCapabilitySnapshotDoesNotMixProviderAndResourceAcrossStores(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "image-snapshot-consistency.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{
		ID: "prv_snapshot_consistency", Name: "Snapshot Consistency", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL,
		Status: StatusActive, Healthy: true,
	})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_snapshot_consistency", ProviderID: provider.ID, Name: "Snapshot Consistency Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "snapshot-access", RefreshToken: "snapshot-refresh", AccountID: "snapshot-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := storeA.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := storeA.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || !updated {
		t.Fatalf("mark supported: updated=%v err=%v", updated, err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_snapshot_after_resource_read"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "provider_resources" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	type snapshotResult struct {
		provider Provider
		resource ProviderResource
		err      error
	}
	snapshotCh := make(chan snapshotResult, 1)
	go func() {
		gotProvider, gotResource, _, snapshotErr := storeA.ProviderResourceImageCapabilitySnapshot(resource.ID)
		snapshotCh <- snapshotResult{provider: gotProvider, resource: gotResource, err: snapshotErr}
	}()
	<-loaded
	updateStarted := make(chan struct{})
	updateErr := make(chan error, 1)
	go func() {
		close(updateStarted)
		_, providerErr := storeB.UpdateProvider(provider.ID, Provider{
			Name: provider.Name, Type: provider.Type, BaseURL: provider.BaseURL, Status: StatusDisabled, Healthy: true,
		})
		updateErr <- providerErr
	}()
	<-updateStarted
	close(release)
	snapshot := <-snapshotCh
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
	if snapshot.provider.Status != StatusActive || snapshot.resource.Options[codexImageCapabilityOption] != codexImageCapabilitySupported {
		t.Fatalf("mixed provider/resource snapshot: provider_status=%q resource_options=%+v", snapshot.provider.Status, snapshot.resource.Options)
	}
}

func TestProviderEditPreservesConcurrentReauthorizationMarker(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "provider-edit-reauthorization-race.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	provider := storeA.AddProvider(Provider{ID: "prv_provider_edit_race", Name: "Provider Edit Race", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ID: "rsrc_provider_edit_race", ProviderID: provider.ID, Name: "Provider Edit Race Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "provider-edit-access", RefreshToken: "provider-edit-refresh", AccountID: "provider-edit-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_provider_edit_after_resource_read"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "provider_resources" && blocked.CompareAndSwap(false, true) {
			close(loaded)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.db.Callback().Query().Remove(callbackName) })
	updateErr := make(chan error, 1)
	go func() {
		_, providerErr := storeA.UpdateProvider(provider.ID, Provider{
			Name: provider.Name, Type: provider.Type, BaseURL: "https://chatgpt.com/backend-api/codex-new", Status: StatusActive, Healthy: true,
		})
		updateErr <- providerErr
	}()
	<-loaded
	if _, err := storeB.UpdateProviderResourceOptions(resource.ID, map[string]string{openAIAccountReauthorizationRequiredOption: "true"}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
	stored, _ := storeB.GetProviderResource(resource.ID)
	if stored.Options[openAIAccountReauthorizationRequiredOption] != "true" {
		t.Fatalf("provider edit erased reauthorization marker: %+v", stored.Options)
	}
}
