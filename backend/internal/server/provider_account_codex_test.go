package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConsumeCodexResponsesStreamTerminatesCompletedEvent(t *testing.T) {
	stream := strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_real","status":"completed","usage":{"input_tokens":2,"output_tokens":1}}}`,
		"",
	}, "\n")
	var destination bytes.Buffer

	response, output, usage, err := consumeCodexResponsesStream(strings.NewReader(stream), &destination)
	if err != nil {
		t.Fatalf("consume stream: %v", err)
	}
	if response["status"] != "completed" || output != "ok" {
		t.Fatalf("unexpected completed response: response=%v output=%q", response, output)
	}
	if usage.PromptTokens != 2 || usage.CompletionTokens != 1 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if !strings.HasSuffix(destination.String(), "\n\n") {
		t.Fatalf("completed SSE event is not terminated: %q", destination.String())
	}
}

func TestCodexSubscriptionModelsUsesLiveVisibleCatalog(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("client_version") != openAICodexVersion {
			t.Fatalf("missing client version: %s", req.URL.String())
		}
		if req.Header.Get("Authorization") != "Bearer access_real" || req.Header.Get("ChatGPT-Account-ID") != "acct_real" {
			t.Fatalf("missing Codex auth headers: %#v", req.Header)
		}
		body := `{"models":[
			{"slug":"gpt-live-codex","display_name":"GPT Live Codex","description":"Live model","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}],"visibility":"list","supported_in_api":true,"priority":2,"additional_speed_tiers":["fast"],"context_window":272000,"input_modalities":["text","image"]},
			{"slug":"codex-hidden","display_name":"Hidden","supported_reasoning_levels":[{"effort":"medium"}],"visibility":"hide","priority":1}
		]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	adapter := CodexSubscriptionAdapter{
		Client:    client,
		ModelsURL: "https://chatgpt.example/backend-api/codex/models",
		RefreshCredentials: func(context.Context, string, bool) (ProviderResourceCredentials, error) {
			return ProviderResourceCredentials{AccessToken: "access_real", AccountID: "acct_real"}, nil
		},
	}

	catalog, err := adapter.Models(context.Background(), "resource_real")
	if err != nil {
		t.Fatalf("list Codex models: %v", err)
	}
	if catalog.DisplayName != "OpenAI Codex" || catalog.Type != ProviderOpenAICodex || catalog.BaseURL != openAICodexBaseURL || catalog.ModelsCount != 1 || len(catalog.Models) != 1 {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	model := catalog.Models[0]
	if model.ID != "gpt-live-codex" || model.Category != "codex" || model.ContextWindow != 272000 {
		t.Fatalf("unexpected live model: %+v", model)
	}
	if model.Metadata["supported_reasoning_levels"] != "low,medium,high" || model.Metadata["additional_speed_tiers"] != "fast" {
		t.Fatalf("missing live model metadata: %+v", model.Metadata)
	}
}

func TestCodexRouteFilteringUsesPerAccountModels(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID:      "prv_codex_pool",
		Name:    "Codex Pool",
		Type:    ProviderOpenAICodex,
		Status:  StatusActive,
		Healthy: true,
	})
	solResource, err := store.AddProviderResource(ProviderResource{
		ID:           "rsrc_codex_sol",
		ProviderID:   provider.ID,
		Name:         "Sol Account",
		ResourceType: ProviderResourceOpenAISubscription,
		Status:       StatusActive,
		Healthy:      true,
		Options:      codexCapabilityOptionsForTest("gpt-5.6-sol", "gpt-5.6-luna"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProviderResource(ProviderResource{
		ID:           "rsrc_codex_luna",
		ProviderID:   provider.ID,
		Name:         "Luna Account",
		ResourceType: ProviderResourceOpenAISubscription,
		Status:       StatusActive,
		Healthy:      true,
		Options:      codexCapabilityOptionsForTest("gpt-5.6-luna"),
	}); err != nil {
		t.Fatal(err)
	}
	store.AddModel(Model{Name: "gpt-5.6-sol", Modality: "chat", Status: StatusActive})
	store.AddRoute(ModelRoute{
		ID:            "route_codex_sol",
		ModelName:     "gpt-5.6-sol",
		ProviderID:    provider.ID,
		ProviderModel: "gpt-5.6-sol",
		Status:        StatusActive,
	})

	routes, err := store.SelectRouteCandidates("gpt-5.6-sol")
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := New(store).filterCodexRoutesByModel(context.Background(), "gpt-5.6-sol", routes)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || routeResourceID(filtered[0]) != solResource.ID {
		t.Fatalf("expected only Sol-capable account, got %+v", filtered)
	}
}

func TestCodexRouteFilteringRefreshesEachAccountCatalog(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID:      "prv_codex_live_pool",
		Name:    "Codex Live Pool",
		Type:    ProviderOpenAICodex,
		Status:  StatusActive,
		Healthy: true,
	})
	for _, resource := range []ProviderResource{
		{
			ID:           "rsrc_codex_live_sol",
			ProviderID:   provider.ID,
			Name:         "Live Sol Account",
			ResourceType: ProviderResourceOpenAISubscription,
			Status:       StatusActive,
			Healthy:      true,
			Credentials: &ProviderResourceCredentials{
				AccessToken: "access_live_sol",
				AccountID:   "account_live_sol",
			},
		},
		{
			ID:           "rsrc_codex_live_luna",
			ProviderID:   provider.ID,
			Name:         "Live Luna Account",
			ResourceType: ProviderResourceOpenAISubscription,
			Status:       StatusActive,
			Healthy:      true,
			Credentials: &ProviderResourceCredentials{
				AccessToken: "access_live_luna",
				AccountID:   "account_live_luna",
			},
		},
	} {
		if _, err := store.AddProviderResource(resource); err != nil {
			t.Fatal(err)
		}
	}
	store.AddModel(Model{Name: "gpt-5.6-sol", Modality: "chat", Status: StatusActive})
	store.AddRoute(ModelRoute{
		ID:            "route_codex_live_sol",
		ModelName:     "gpt-5.6-sol",
		ProviderID:    provider.ID,
		ProviderModel: "gpt-5.6-sol",
		Status:        StatusActive,
	})

	server := New(store)
	server.codexSubscription.ModelsURL = "https://chatgpt.example/backend-api/codex/models"
	server.codexSubscription.Client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		model := "gpt-5.6-luna"
		if req.Header.Get("ChatGPT-Account-ID") == "account_live_sol" {
			model = "gpt-5.6-sol"
		}
		body := `{"models":[{"slug":"` + model + `","display_name":"` + model + `","visibility":"list","supported_in_api":true,"priority":1}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	routes, err := store.SelectRouteCandidates("gpt-5.6-sol")
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := server.filterCodexRoutesByModel(context.Background(), "gpt-5.6-sol", routes)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || routeResourceID(filtered[0]) != "rsrc_codex_live_sol" {
		t.Fatalf("expected only live Sol account, got %+v", filtered)
	}
	for _, resource := range store.ListProviderResources() {
		models, fetchedAt, cached := codexResourceCachedModels(&resource)
		if !cached || fetchedAt.IsZero() || len(models) != 1 {
			t.Fatalf("account catalog was not persisted for %s: models=%v fetched_at=%s", resource.ID, models, fetchedAt)
		}
	}
}

func TestCodexUnsupportedModelFailsOverAndUpdatesAccountModels(t *testing.T) {
	store := NewMemoryStore()
	provider := store.AddProvider(Provider{
		ID:      "prv_codex_failover",
		Name:    "Codex Failover Pool",
		Type:    ProviderOpenAICodex,
		Status:  StatusActive,
		Healthy: true,
	})
	unsupported, err := store.AddProviderResource(ProviderResource{
		ID:           "rsrc_codex_unsupported",
		ProviderID:   provider.ID,
		Name:         "Unsupported Account",
		ResourceType: ProviderResourceOpenAISubscription,
		Status:       StatusActive,
		Healthy:      true,
		Options:      codexCapabilityOptionsForTest("gpt-5.6-sol"),
		Credentials: &ProviderResourceCredentials{
			AccessToken: "access_unsupported",
			AccountID:   "account_unsupported",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	supported, err := store.AddProviderResource(ProviderResource{
		ID:           "rsrc_codex_supported",
		ProviderID:   provider.ID,
		Name:         "Supported Account",
		ResourceType: ProviderResourceOpenAISubscription,
		Status:       StatusActive,
		Healthy:      true,
		Options:      codexCapabilityOptionsForTest("gpt-5.6-sol"),
		Credentials: &ProviderResourceCredentials{
			AccessToken: "access_supported",
			AccountID:   "account_supported",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.AddModel(Model{Name: "gpt-5.6-sol", Modality: "chat", Status: StatusActive})
	store.AddRoute(ModelRoute{
		ID:            "route_codex_failover",
		ModelName:     "gpt-5.6-sol",
		ProviderID:    provider.ID,
		ProviderModel: "gpt-5.6-sol",
		Status:        StatusActive,
	})

	server := New(store)
	server.codexSubscription.Client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("ChatGPT-Account-ID") == "account_unsupported" {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`,
				)),
				Request: req,
			}, nil
		}
		completed := strings.Join([]string{
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_supported","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(completed)),
			Request:    req,
		}, nil
	})}
	server.adapters[ProviderOpenAICodex] = server.codexSubscription

	routes, err := store.SelectRouteCandidates("gpt-5.6-sol")
	if err != nil {
		t.Fatal(err)
	}
	ordered := make([]RouteSelection, 0, len(routes))
	for _, resourceID := range []string{unsupported.ID, supported.ID} {
		for _, route := range routes {
			if routeResourceID(route) == resourceID {
				ordered = append(ordered, route)
			}
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp, selected, _, attempts, err := server.executeRoutedResponses(request, RoutedCall{
		Call: CallContext{
			RequestID: "req_codex_failover",
			Model:     Model{Name: "gpt-5.6-sol", Status: StatusActive},
		},
		Routes: ordered,
	}, ResponsesRequest{Model: "gpt-5.6-sol", Input: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || routeResourceID(selected) != supported.ID || len(attempts) != 2 {
		t.Fatalf("expected failover to supported account, selected=%s attempts=%d response=%v", routeResourceID(selected), len(attempts), resp)
	}
	updated, ok := server.providerResourceByID(unsupported.ID)
	if !ok {
		t.Fatal("unsupported account disappeared")
	}
	models, _, cached := codexResourceCachedModels(&updated)
	if !cached || codexModelInList("gpt-5.6-sol", models) {
		t.Fatalf("unsupported model was not removed from account capabilities: %v", models)
	}
	if updated.FailureCount != 0 {
		t.Fatalf("model entitlement mismatch should not degrade account health: %+v", updated)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func codexCapabilityOptionsForTest(models ...string) map[string]string {
	encoded, _ := json.Marshal(models)
	return map[string]string{
		codexResourceSupportedModelsOption: string(encoded),
		codexResourceModelsFetchedAtOption: time.Now().UTC().Format(time.RFC3339Nano),
	}
}
