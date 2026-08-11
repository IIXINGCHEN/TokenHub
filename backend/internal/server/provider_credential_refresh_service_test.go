package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}

func TestProviderCredentialRefreshServiceSkipsHealthyOrUnsupportedResources(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{Name: "OAuth Resources", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true})
	unsupportedProvider := store.AddProvider(Provider{Name: "API Key Resources", Type: ProviderOpenAICompatible, Status: StatusActive, Healthy: true})
	for _, resource := range []ProviderResource{
		{ProviderID: provider.ID, Name: "No Refresh Token", ResourceType: ProviderResourceOpenAISubscription, Status: StatusActive, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "access-no-refresh", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}},
		{ProviderID: provider.ID, Name: "Disabled", ResourceType: ProviderResourceOpenAISubscription, Status: StatusDisabled, Healthy: true, Credentials: &ProviderResourceCredentials{AccessToken: "access-disabled", RefreshToken: "refresh-disabled", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}},
		{ProviderID: unsupportedProvider.ID, Name: "API Key", ResourceType: ProviderResourceAPIKey, Status: StatusActive, Healthy: true, APIKey: "api-key"},
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
	var requests atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Your session has ended. Please log in again.","code":"refresh_token_invalidated"}}`))
	}))
	defer tokenServer.Close()
	previousEndpoint := openAIAccountOAuthTokenEndpoint
	openAIAccountOAuthTokenEndpoint = tokenServer.URL
	defer func() { openAIAccountOAuthTokenEndpoint = previousEndpoint }()

	service := newProviderCredentialRefreshService(store)
	service.RunDue(context.Background())
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
}
