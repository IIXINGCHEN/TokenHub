package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderCredentialRefreshServiceRenewsExpiringOpenAIAccounts(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Codex OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Codex OAuth Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{
			AuthType: "oauth", AccessToken: "access-before-renewal", RefreshToken: "refresh-before-renewal", ClientID: openAIAccountOAuthClientID,
			ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh-before-renewal" {
			t.Fatalf("unexpected refresh request: %v", r.Form)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-after-renewal", "refresh_token": "refresh-after-renewal", "expires_in": 3600})
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	service := newProviderCredentialRefreshService(store)
	service.RunDue(context.Background())
	if requests.Load() != 1 {
		t.Fatalf("expected one renewal request, got %d", requests.Load())
	}
	credentials, err := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "access-after-renewal" || credentials.RefreshToken != "refresh-after-renewal" {
		t.Fatalf("expected rotated credentials to be stored, got %+v", credentials)
	}
	stored, _ := store.GetProviderResource(resource.ID)
	if stored.Options[codexImageCapabilityOption] != codexImageCapabilitySupported {
		t.Fatalf("scheduled refresh cleared image capability: %+v", stored.Options)
	}
}

func TestOAuthInvalidGrantIsRefreshSpecific(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	if _, err := requestOpenAIAccountOAuthToken(context.Background(), url.Values{"grant_type": {"authorization_code"}}); AsHTTPError(err).Code != "oauth_token_failed" {
		t.Fatalf("authorization-code invalid_grant was classified as a refresh failure: %v", err)
	}
	if _, err := requestOpenAIAccountOAuthToken(context.Background(), url.Values{"grant_type": {"refresh_token"}}); AsHTTPError(err).Code != "oauth_refresh_reauthorization_required" {
		t.Fatalf("refresh invalid_grant did not require reauthorization: %v", err)
	}
}

func TestProviderCredentialRefreshServiceSkipsHealthyOrUnsupportedResources(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "OAuth Resources", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	unsupportedProvider := store.AddProvider(Provider{Name: "API Key Resources", Type: ProviderOpenAICompatible, Status: StatusActive, Healthy: true})
	disabledProvider := store.AddProvider(Provider{Name: "Disabled OAuth Provider", Type: ProviderOpenAICodex, Status: StatusDisabled, Healthy: true})
	for _, resource := range []ProviderResource{
		{ProviderID: provider.ID, Name: "No Refresh Token", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "access-no-refresh", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}},
		{ProviderID: provider.ID, Name: "Disabled", ResourceType: ProviderResourceOpenAISubscription, Status: StatusDisabled, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "access-disabled", RefreshToken: "refresh-disabled", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}},
		{ProviderID: unsupportedProvider.ID, Name: "API Key", ResourceType: ProviderResourceAPIKey, Status: StatusActive, Healthy: true, APIKey: "api-key"},
		{ProviderID: disabledProvider.ID, Name: "Disabled Provider Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "access-disabled-provider", RefreshToken: "refresh-disabled-provider", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}},
	} {
		if _, err := store.AddProviderResource(resource); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected token renewal", http.StatusInternalServerError)
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	newProviderCredentialRefreshService(store).RunDue(context.Background())
	if requests.Load() != 0 {
		t.Fatalf("expected no renewal requests, got %d", requests.Load())
	}
}

func TestProviderResourceEditPreservesStoredOAuthAndServerManagedOptions(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Edited OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Edited Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{
		openAIAccountReauthorizationRequiredOption: "true",
		codexImageCapabilityOption:                 codexImageCapabilitySupported,
		codexImageCapabilityCheckedAtOption:        time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Options: map[string]string{"auth_type": "oauth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CredentialSummary["has_refresh_token"] != "true" || updated.CredentialSummary[openAIAccountReauthorizationRequiredOption] != "true" {
		t.Fatalf("account edit lost OAuth metadata: %+v", updated.CredentialSummary)
	}
	if updated.Options[codexImageCapabilityOption] != codexImageCapabilitySupported || updated.Options[codexImageCapabilityCheckedAtOption] == "" {
		t.Fatalf("account edit lost image capability state: %+v", updated.Options)
	}
	var persisted ProviderResource
	if err := store.db.First(&persisted, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.APIKey == "access" || !strings.HasPrefix(persisted.APIKey, "enc:v1:") {
		t.Fatalf("account edit stored the access token without encryption: %q", persisted.APIKey)
	}
}

func TestProviderResourceCredentialMetadataDoesNotClearReauthorization(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Locked OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Locked Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "old-access", RefreshToken: "invalid-refresh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{openAIAccountReauthorizationRequiredOption: "true"}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Scopes: "openid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CredentialSummary[openAIAccountReauthorizationRequiredOption] != "true" {
		t.Fatalf("credential metadata cleared the reauthorization lock: %+v", updated.CredentialSummary)
	}
	persisted, _ := store.GetProviderResource(resource.ID)
	credentials := store.providerResourceCredentialsForRuntime(persisted)
	if credentials.AccessToken != "old-access" || credentials.RefreshToken != "invalid-refresh" {
		t.Fatalf("credential metadata overwrote stored authentication material: %+v", credentials)
	}
}

func TestCredentialRefreshDoesNotOverwriteConcurrentReauthorization(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Concurrent Reauthorization", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Concurrent Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	done := make(chan error, 1)
	go func() {
		_, refreshErr := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, true)
		done <- refreshErr
	}()
	<-started
	_, err = store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("stale refresh result should be discarded after reauthorization: %v", err)
	}
	credentials, err := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "new-access" || credentials.RefreshToken != "new-refresh" {
		t.Fatalf("stale refresh overwrote new credentials: %+v", credentials)
	}
	listed := store.ListProviderResources()
	if listed[0].CredentialSummary[openAIAccountReauthorizationRequiredOption] == "true" {
		t.Fatal("stale invalid_grant marked newly authorized credentials invalid")
	}
}

func TestCredentialRefreshSurvivesConcurrentMetadataEdit(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Concurrent Metadata", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Before Edit", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "old-access", RefreshToken: "single-use-refresh", AccountID: "old-account", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "rotated-access", "refresh_token": "rotated-refresh", "expires_in": 3600})
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	done := make(chan error, 1)
	go func() {
		_, refreshErr := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, true)
		done <- refreshErr
	}()
	<-started
	if _, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: "After Edit", ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccountID: "updated-account", ExpiresAt: time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339), Scopes: "openid profile"},
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetProviderResource(resource.ID)
	credentials := store.providerResourceCredentialsForRuntime(stored)
	if credentials.AccessToken != "rotated-access" || credentials.RefreshToken != "rotated-refresh" || credentials.AccountID != "updated-account" {
		t.Fatalf("metadata edit discarded rotated credentials: %+v", credentials)
	}
}

func TestCredentialReplacementInvalidatesImageCapability(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Image Identity", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Image Identity Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "old-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{
		codexImageCapabilityOption: codexImageCapabilitySupported, codexImageCapabilityCheckedAtOption: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "new-access", RefreshToken: "new-refresh", AccountID: "new-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Options[codexImageCapabilityOption] != "" || updated.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("replacement credentials inherited image capability: %+v", updated.Options)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported}); err != nil {
		t.Fatal(err)
	}
	upserted, err := store.AddProviderResource(ProviderResource{
		ID: resource.ID, ProviderID: provider.ID, Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "third-access", RefreshToken: "third-refresh", AccountID: "third-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upserted.Options[codexImageCapabilityOption] != "" || upserted.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("upserted replacement credentials inherited image capability: %+v", upserted.Options)
	}
}

func TestRequestConfigurationChangeInvalidatesImageCapability(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{ID: "prv_image_config", Name: "Image Configuration", Type: ProviderOpenAICodex, BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ID: "rsrc_image_config", ProviderID: provider.ID, Name: "Image Configuration Account", ResourceType: ProviderResourceOpenAISubscription,
		BaseURL: openAICodexBaseURL, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "config-access", RefreshToken: "config-refresh", AccountID: "config-account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported}); err != nil {
		t.Fatal(err)
	}
	_, _, binding, err := store.ProviderResourceImageCapabilitySnapshot(resource.ID)
	if err != nil {
		t.Fatalf("load image request binding: %v", err)
	}
	updated, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, BaseURL: "https://chatgpt.com/backend-api/codex-v2", Status: StatusActive, Healthy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("resource endpoint change preserved image capability: %+v", updated.Options)
	}
	if persisted, err := store.UpdateProviderResourceImageCapability(resource.ID, binding, codexImageCapabilitySupported, time.Now()); err != nil || persisted {
		t.Fatalf("stale request binding persisted capability: persisted=%v err=%v", persisted, err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{codexImageCapabilityOption: codexImageCapabilitySupported}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProvider(provider.ID, Provider{Type: ProviderOpenAICodex, BaseURL: "https://chatgpt.com/backend-api/codex-v3", Status: StatusActive, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	updated, _ = store.GetProviderResource(resource.ID)
	if updated.Options[codexImageCapabilityOption] != "" {
		t.Fatalf("Provider endpoint change preserved image capability: %+v", updated.Options)
	}
}

func TestProviderResourceWritesCannotMutateServerManagedOptions(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Managed Options", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Managed Options Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Options: map[string]string{
			openAIAccountReauthorizationRequiredOption: "true",
			codexImageCapabilityOption:                 codexImageCapabilitySupported,
			codexImageCapabilityCheckedAtOption:        "forged",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Options[openAIAccountReauthorizationRequiredOption] != "" || resource.Options[codexImageCapabilityOption] != "" || resource.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("create accepted server-managed options: %+v", resource.Options)
	}
	checkedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{
		openAIAccountReauthorizationRequiredOption: "true",
		codexImageCapabilityOption:                 codexImageCapabilityUnsupported,
		codexImageCapabilityCheckedAtOption:        checkedAt,
	}); err != nil {
		t.Fatal(err)
	}
	upserted, err := store.AddProviderResource(ProviderResource{
		ID: resource.ID, ProviderID: provider.ID, Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Options: map[string]string{
			openAIAccountReauthorizationRequiredOption: "",
			codexImageCapabilityOption:                 codexImageCapabilitySupported,
			codexImageCapabilityCheckedAtOption:        "forged-upsert",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upserted.Options[openAIAccountReauthorizationRequiredOption] != "true" || upserted.Options[codexImageCapabilityOption] != codexImageCapabilityUnsupported || upserted.Options[codexImageCapabilityCheckedAtOption] != checkedAt {
		t.Fatalf("create upsert mutated server-managed options: %+v", upserted.Options)
	}
	updated, err := store.UpdateProviderResource(resource.ID, ProviderResource{
		Name: resource.Name, ResourceType: resource.ResourceType, Status: StatusActive, Healthy: true,
		Options: map[string]string{
			openAIAccountReauthorizationRequiredOption: "",
			codexImageCapabilityOption:                 codexImageCapabilitySupported,
			codexImageCapabilityCheckedAtOption:        "forged-again",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Options[openAIAccountReauthorizationRequiredOption] != "true" || updated.Options[codexImageCapabilityOption] != codexImageCapabilityUnsupported || updated.Options[codexImageCapabilityCheckedAtOption] != checkedAt {
		t.Fatalf("update mutated server-managed options: %+v", updated.Options)
	}
}

func TestProviderCredentialRefreshServiceStopsRetryingInvalidatedRefreshTokens(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Invalidated OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	resource, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Invalidated Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "expired-access", RefreshToken: "invalidated-refresh", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProviderResourceOptions(resource.ID, map[string]string{
		codexImageCapabilityOption:          codexImageCapabilitySupported,
		codexImageCapabilityCheckedAtOption: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"` + strings.Repeat("x", 260) + `","code":"refresh_token_invalidated"}}`))
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	if _, err := store.RefreshProviderResourceCredentials(context.Background(), resource.ID, true); AsHTTPError(err).Code != "provider_resource_reauthorization_required" {
		t.Fatalf("expected manual refresh to require reauthorization, got %v", err)
	}
	service := newProviderCredentialRefreshService(store)
	service.RunDue(context.Background())
	if requests.Load() != 1 {
		t.Fatalf("expected one renewal attempt after token invalidation, got %d", requests.Load())
	}
	var stored *ProviderResource
	for _, candidate := range store.ListProviderResources() {
		if candidate.ID == resource.ID {
			stored = &candidate
			break
		}
	}
	if stored == nil {
		t.Fatal("expected provider resource to exist")
	}
	if stored.CredentialSummary[openAIAccountReauthorizationRequiredOption] != "true" {
		t.Fatalf("expected resource to require reauthorization, got %+v", stored.CredentialSummary)
	}
	if stored.Options[codexImageCapabilityOption] != "" || stored.Options[codexImageCapabilityCheckedAtOption] != "" {
		t.Fatalf("reauthorization-required resource retained image capability: %+v", stored.Options)
	}
	if store.codexImageResourceAvailable(*stored) {
		t.Fatal("reauthorization-required resource remained eligible for image routing")
	}
}

func TestProviderCredentialRefreshServiceRenewsAccountsConcurrently(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Concurrent OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	for index, refreshToken := range []string{"blocked-1", "blocked-2", "blocked-3", "ready"} {
		if _, err := store.AddProviderResource(ProviderResource{
			ProviderID: provider.ID, Name: refreshToken, ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true, Priority: index,
			Credentials: &ProviderResourceCredentials{AccessToken: "expired-" + refreshToken, RefreshToken: refreshToken, ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	blocked := make(chan struct{}, 3)
	ready := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("refresh_token") == "ready" {
			ready <- struct{}{}
		} else {
			blocked <- struct{}{}
			<-release
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "renewed", "expires_in": 3600})
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	done := make(chan struct{})
	go func() {
		newProviderCredentialRefreshService(store).RunDue(context.Background())
		close(done)
	}()
	defer func() {
		releaseAll()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("credential refresh workers did not finish")
		}
	}()
	for range 3 {
		select {
		case <-blocked:
		case <-time.After(time.Second):
			t.Fatal("expected the blocked account renewals to start")
		}
	}
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("ready account was blocked behind slow account renewals")
	}
}

func TestProviderCredentialRefreshServiceDoesNotLogOAuthResponse(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "Logging OAuth", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	if _, err := store.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Logging Account", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{AccessToken: "expired-access", RefreshToken: "refresh-secret", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)},
	}); err != nil {
		t.Fatal(err)
	}
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream-secret-session","token":"upstream-secret-token"}}`))
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	newProviderCredentialRefreshService(store).RunDue(context.Background())
	if output := logs.String(); strings.Contains(output, "upstream-secret") || strings.Contains(output, "refresh-secret") {
		t.Fatalf("OAuth response detail leaked to logs: %s", output)
	}
	if !strings.Contains(logs.String(), "code=oauth_token_failed") {
		t.Fatalf("expected stable OAuth error code in logs, got %s", logs.String())
	}
}
