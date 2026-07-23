package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	codexProviderCatalogID             = "openai-codex"
	codexResourceSupportedModelsOption = "codex_supported_models"
	codexResourceModelsFetchedAtOption = "codex_models_fetched_at"
	codexResourceModelsCacheTTL        = 5 * time.Minute
)

type codexRemoteModelsResponse struct {
	Models []codexRemoteModel `json:"models"`
}

type codexRemoteModel struct {
	Slug                     string                      `json:"slug"`
	DisplayName              string                      `json:"display_name"`
	Description              string                      `json:"description"`
	DefaultReasoningLevel    string                      `json:"default_reasoning_level"`
	SupportedReasoningLevels []codexRemoteReasoningLevel `json:"supported_reasoning_levels"`
	Visibility               string                      `json:"visibility"`
	SupportedInAPI           bool                        `json:"supported_in_api"`
	Priority                 int                         `json:"priority"`
	AdditionalSpeedTiers     []string                    `json:"additional_speed_tiers"`
	ContextWindow            int64                       `json:"context_window"`
	InputModalities          []string                    `json:"input_modalities"`
}

type codexRemoteReasoningLevel struct {
	Effort string `json:"effort"`
}

func (a CodexSubscriptionAdapter) Models(ctx context.Context, resourceID string) (ProviderCatalogEntry, error) {
	if a.RefreshCredentials == nil {
		return ProviderCatalogEntry{}, NewHTTPError(http.StatusServiceUnavailable, "provider_credentials_unavailable", "Codex Subscription credentials are unavailable")
	}
	creds, err := a.RefreshCredentials(ctx, resourceID, false)
	if err != nil {
		return ProviderCatalogEntry{}, err
	}
	catalog, status, err := a.modelsWithCredentials(ctx, creds)
	if status != http.StatusUnauthorized {
		return catalog, err
	}
	creds, refreshErr := a.RefreshCredentials(ctx, resourceID, true)
	if refreshErr != nil {
		return ProviderCatalogEntry{}, refreshErr
	}
	catalog, _, err = a.modelsWithCredentials(ctx, creds)
	return catalog, err
}

func (a CodexSubscriptionAdapter) ModelsWithCredentials(ctx context.Context, creds ProviderResourceCredentials) (ProviderCatalogEntry, error) {
	catalog, _, err := a.modelsWithCredentials(ctx, creds)
	return catalog, err
}

func (a CodexSubscriptionAdapter) modelsWithCredentials(ctx context.Context, creds ProviderResourceCredentials) (ProviderCatalogEntry, int, error) {
	accessToken := strings.TrimSpace(creds.AccessToken)
	accountID := strings.TrimSpace(creds.AccountID)
	if accessToken == "" {
		return ProviderCatalogEntry{}, 0, NewHTTPError(http.StatusBadRequest, "openai_account_token_missing", "OpenAI account access token is missing")
	}
	if accountID == "" {
		return ProviderCatalogEntry{}, 0, NewHTTPError(http.StatusBadRequest, "openai_account_id_missing", "OpenAI ChatGPT account ID is missing")
	}
	modelsURL := strings.TrimSpace(a.ModelsURL)
	if modelsURL == "" {
		modelsURL = openAICodexModelsURL
	}
	endpoint, err := url.Parse(modelsURL)
	if err != nil {
		return ProviderCatalogEntry{}, 0, NewHTTPError(http.StatusInternalServerError, "codex_models_url_invalid", "Codex models URL is invalid")
	}
	query := endpoint.Query()
	query.Set("client_version", openAICodexVersion)
	endpoint.RawQuery = query.Encode()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ProviderCatalogEntry{}, 0, NewHTTPError(http.StatusBadGateway, "codex_models_request_failed", "Failed to create Codex models request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("ChatGPT-Account-ID", accountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("User-Agent", openAICodexUserAgent)
	req.Header.Set("Version", openAICodexVersion)
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProviderCatalogEntry{}, 0, NewHTTPError(http.StatusBadGateway, "codex_models_request_failed", fmt.Sprintf("Codex models request failed: %v", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return ProviderCatalogEntry{}, resp.StatusCode, NewHTTPError(resp.StatusCode, "codex_models_upstream_error", message)
	}
	var payload codexRemoteModelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return ProviderCatalogEntry{}, resp.StatusCode, NewHTTPError(http.StatusBadGateway, "codex_models_invalid_response", "Codex models response is invalid")
	}
	models := make([]ProviderCatalogModel, 0, len(payload.Models))
	for _, remote := range payload.Models {
		if strings.TrimSpace(remote.Slug) == "" || (remote.Visibility != "" && !strings.EqualFold(remote.Visibility, "list")) {
			continue
		}
		reasoningLevels := make([]string, 0, len(remote.SupportedReasoningLevels))
		for _, level := range remote.SupportedReasoningLevels {
			if effort := strings.TrimSpace(level.Effort); effort != "" {
				reasoningLevels = append(reasoningLevels, effort)
			}
		}
		supportedParameters := []string{"reasoning_effort"}
		if stringInList("fast", remote.AdditionalSpeedTiers) {
			supportedParameters = append(supportedParameters, "service_tier")
		}
		models = append(models, ProviderCatalogModel{
			ID:                  remote.Slug,
			Name:                remote.Slug,
			DisplayName:         firstNonEmpty(remote.DisplayName, remote.Slug),
			CanonicalName:       remote.Slug,
			Category:            "codex",
			Family:              "codex",
			Type:                "chat",
			ContextWindow:       remote.ContextWindow,
			InputModalities:     append([]string(nil), remote.InputModalities...),
			OutputModalities:    []string{"text"},
			Capabilities:        []string{"chat", "reasoning", "streaming", "tools"},
			SupportedParameters: supportedParameters,
			LastUpdated:         time.Now().UTC().Format(time.RFC3339),
			Metadata: map[string]string{
				"source":                     "openai-codex-live",
				"description":                remote.Description,
				"visibility":                 remote.Visibility,
				"supported_in_api":           strconv.FormatBool(remote.SupportedInAPI),
				"priority":                   strconv.Itoa(remote.Priority),
				"display_name":               firstNonEmpty(remote.DisplayName, remote.Slug),
				"default_reasoning_level":    remote.DefaultReasoningLevel,
				"supported_reasoning_levels": strings.Join(reasoningLevels, ","),
				"additional_speed_tiers":     strings.Join(remote.AdditionalSpeedTiers, ","),
			},
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		left, _ := strconv.Atoi(models[i].Metadata["priority"])
		right, _ := strconv.Atoi(models[j].Metadata["priority"])
		return left < right
	})
	categories, counts := catalogCategorySummary(models)
	return ProviderCatalogEntry{
		ID:             codexProviderCatalogID,
		Name:           "OpenAI Codex",
		DisplayName:    "OpenAI Codex",
		Type:           ProviderOpenAICodex,
		BaseURL:        openAICodexBaseURL,
		DocURL:         "https://developers.openai.com/codex",
		Categories:     categories,
		CategoryCounts: counts,
		ModelsCount:    len(models),
		Source:         "openai-codex-live",
		Models:         models,
	}, resp.StatusCode, nil
}

func (s *Server) queryOpenAICodexModels(ctx context.Context, resourceID string) (ProviderCatalogEntry, error) {
	resource, ok := s.providerResourceByID(resourceID)
	if !ok {
		return ProviderCatalogEntry{}, NewHTTPError(http.StatusNotFound, "provider_resource_not_found", "Provider resource not found")
	}
	if !isOpenAIAccountResource(resource.ResourceType) {
		return ProviderCatalogEntry{}, NewHTTPError(http.StatusBadRequest, "provider_resource_models_unsupported", "Codex models are only available for OpenAI subscription resources")
	}
	catalog, err := s.codexSubscription.Models(ctx, resourceID)
	if err == nil {
		if persistErr := s.persistCodexResourceModels(resourceID, catalog.Models, time.Now().UTC()); persistErr != nil {
			return ProviderCatalogEntry{}, persistErr
		}
		s.syncOpenAICodexModels(catalog.Models)
	}
	return catalog, err
}

func (s *Server) persistCodexResourceModels(resourceID string, models []ProviderCatalogModel, fetchedAt time.Time) error {
	modelIDs := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		modelID := strings.TrimSpace(model.ID)
		lookupName := normalizeModelLookupName(modelID)
		if modelID == "" {
			continue
		}
		if _, ok := seen[lookupName]; ok {
			continue
		}
		seen[lookupName] = struct{}{}
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	encoded, err := json.Marshal(modelIDs)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateProviderResourceOptions(resourceID, map[string]string{
		codexResourceSupportedModelsOption: string(encoded),
		codexResourceModelsFetchedAtOption: fetchedAt.UTC().Format(time.RFC3339Nano),
	})
	return err
}

func codexResourceCachedModels(resource *ProviderResource) ([]string, time.Time, bool) {
	if resource == nil || resource.Options == nil {
		return nil, time.Time{}, false
	}
	encoded, ok := resource.Options[codexResourceSupportedModelsOption]
	if !ok {
		return nil, time.Time{}, false
	}
	var models []string
	if err := json.Unmarshal([]byte(encoded), &models); err != nil {
		return nil, time.Time{}, false
	}
	fetchedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(resource.Options[codexResourceModelsFetchedAtOption]))
	if err != nil {
		fetchedAt = time.Time{}
	}
	return models, fetchedAt, true
}

func codexModelInList(modelName string, models []string) bool {
	lookupName := normalizeModelLookupName(modelName)
	for _, candidate := range models {
		if normalizeModelLookupName(candidate) == lookupName {
			return true
		}
	}
	return false
}

func (s *Server) filterCodexRoutesByModel(ctx context.Context, modelName string, routes []RouteSelection) ([]RouteSelection, error) {
	type capabilityResult struct {
		models  []string
		checked bool
		err     error
	}
	capabilities := map[string]capabilityResult{}
	filtered := make([]RouteSelection, 0, len(routes))
	var firstErr error
	checkedCodexResource := false

	for _, route := range routes {
		if route.Resource == nil || !isOpenAIAccountResource(route.Resource.ResourceType) {
			filtered = append(filtered, route)
			continue
		}
		resourceID := routeResourceID(route)
		result, ok := capabilities[resourceID]
		if !ok {
			cachedModels, fetchedAt, cached := codexResourceCachedModels(route.Resource)
			if cached && !fetchedAt.IsZero() && time.Since(fetchedAt) < codexResourceModelsCacheTTL {
				result = capabilityResult{models: cachedModels, checked: true}
			} else {
				catalog, err := s.queryOpenAICodexModels(ctx, resourceID)
				if err == nil {
					result = capabilityResult{
						models:  providerCatalogModelIDs(catalog.Models),
						checked: true,
					}
				} else if cached {
					result = capabilityResult{models: cachedModels, checked: true, err: err}
				} else {
					result = capabilityResult{err: err}
				}
			}
			capabilities[resourceID] = result
		}
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
		if result.checked {
			checkedCodexResource = true
		}
		upstreamModel := firstNonEmpty(route.ProviderModel, modelName)
		if result.checked && codexModelInList(upstreamModel, result.models) {
			filtered = append(filtered, route)
		}
	}

	if len(filtered) > 0 {
		return filtered, nil
	}
	if !checkedCodexResource && firstErr != nil {
		return nil, firstErr
	}
	return nil, NewHTTPError(
		http.StatusServiceUnavailable,
		"codex_model_unavailable",
		fmt.Sprintf("No connected Codex account supports model %q", strings.TrimSpace(modelName)),
	)
}

func providerCatalogModelIDs(models []ProviderCatalogModel) []string {
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		if modelID := strings.TrimSpace(model.ID); modelID != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	return modelIDs
}

func (s *Server) removeCodexResourceModel(resourceID string, modelName string) {
	resource, ok := s.providerResourceByID(resourceID)
	if !ok {
		return
	}
	models, _, cached := codexResourceCachedModels(&resource)
	if !cached {
		return
	}
	filtered := make([]string, 0, len(models))
	for _, candidate := range models {
		if normalizeModelLookupName(candidate) != normalizeModelLookupName(modelName) {
			filtered = append(filtered, candidate)
		}
	}
	catalogModels := make([]ProviderCatalogModel, 0, len(filtered))
	for _, modelID := range filtered {
		catalogModels = append(catalogModels, ProviderCatalogModel{ID: modelID})
	}
	_ = s.persistCodexResourceModels(resourceID, catalogModels, time.Now().UTC())
}

func (s *Server) syncOpenAICodexModels(models []ProviderCatalogModel) {
	existing := map[string]Model{}
	for _, model := range s.store.ListModels() {
		existing[normalizeModelLookupName(model.Name)] = model
	}
	for _, model := range models {
		lookupName := normalizeModelLookupName(model.ID)
		if current, ok := existing[lookupName]; ok {
			current.Category = "codex"
			current.Family = "codex"
			current.ContextWindow = model.ContextWindow
			current.InputModalities = append([]string(nil), model.InputModalities...)
			current.OutputModalities = append([]string(nil), model.OutputModalities...)
			current.Capabilities = append([]string(nil), model.Capabilities...)
			current.SupportedParameters = append([]string(nil), model.SupportedParameters...)
			if current.Metadata == nil {
				current.Metadata = map[string]string{}
			}
			for key, value := range model.Metadata {
				current.Metadata[key] = value
			}
			s.store.AddModel(current)
			continue
		}
		s.store.AddModel(Model{
			ID:                  model.ID,
			Name:                model.ID,
			Category:            "codex",
			Family:              "codex",
			Modality:            "chat",
			ContextWindow:       model.ContextWindow,
			InputModalities:     append([]string(nil), model.InputModalities...),
			OutputModalities:    append([]string(nil), model.OutputModalities...),
			Capabilities:        append([]string(nil), model.Capabilities...),
			SupportedParameters: append([]string(nil), model.SupportedParameters...),
			Metadata:            cloneStringMap(model.Metadata),
			Status:              StatusActive,
		})
		existing[lookupName] = Model{ID: model.ID, Name: model.ID}
	}
}

func (s *Server) codexProviderCatalogFromStandardModels(selected []string) ProviderCatalogEntry {
	modelsByName := map[string]Model{}
	for _, model := range s.store.ListModels() {
		modelsByName[normalizeModelLookupName(model.Name)] = model
	}
	models := make([]ProviderCatalogModel, 0, len(selected))
	for _, modelID := range selected {
		model, ok := modelsByName[normalizeModelLookupName(modelID)]
		if !ok {
			continue
		}
		models = append(models, ProviderCatalogModel{
			ID:                  model.Name,
			Name:                model.Name,
			DisplayName:         firstNonEmpty(model.Metadata["display_name"], model.Name),
			CanonicalName:       model.Name,
			Category:            "codex",
			Family:              model.Family,
			Type:                model.Modality,
			ContextWindow:       model.ContextWindow,
			InputModalities:     append([]string(nil), model.InputModalities...),
			OutputModalities:    append([]string(nil), model.OutputModalities...),
			Capabilities:        append([]string(nil), model.Capabilities...),
			SupportedParameters: append([]string(nil), model.SupportedParameters...),
			Metadata:            cloneStringMap(model.Metadata),
		})
	}
	categories, counts := catalogCategorySummary(models)
	return ProviderCatalogEntry{
		ID:             codexProviderCatalogID,
		Name:           "OpenAI Codex",
		DisplayName:    "OpenAI Codex",
		Type:           ProviderOpenAICodex,
		BaseURL:        openAICodexBaseURL,
		DocURL:         "https://developers.openai.com/codex",
		Categories:     categories,
		CategoryCounts: counts,
		ModelsCount:    len(models),
		Source:         "openai-codex-live",
		Models:         models,
	}
}

func codexCatalogModelByID(models []ProviderCatalogModel, id string) (ProviderCatalogModel, bool) {
	for _, model := range models {
		if model.ID == id {
			return model, true
		}
	}
	return ProviderCatalogModel{}, false
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
