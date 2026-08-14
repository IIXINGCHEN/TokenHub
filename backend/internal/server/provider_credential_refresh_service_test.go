package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
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
