package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openAIAccountQuotaURL     = "https://chatgpt.com/backend-api/wham/usage"
	openAIAccountQuotaBeta    = "codex-1"
	openAIAccountQuotaTimeout = 20 * time.Second
)

type OpenAIAccountQuotaWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type OpenAIAccountRateLimit struct {
	Allowed         bool                      `json:"allowed"`
	LimitReached    bool                      `json:"limit_reached"`
	PrimaryWindow   *OpenAIAccountQuotaWindow `json:"primary_window,omitempty"`
	SecondaryWindow *OpenAIAccountQuotaWindow `json:"secondary_window,omitempty"`
}

type OpenAIAccountAdditionalRateLimit struct {
	LimitName      string                  `json:"limit_name"`
	MeteredFeature string                  `json:"metered_feature"`
	RateLimit      *OpenAIAccountRateLimit `json:"rate_limit,omitempty"`
}

type OpenAIAccountRateLimitResetCredits struct {
	AvailableCount int `json:"available_count"`
}

type OpenAIAccountQuota struct {
	UserID                string                              `json:"user_id,omitempty"`
	AccountID             string                              `json:"account_id,omitempty"`
	Email                 string                              `json:"email,omitempty"`
	PlanType              string                              `json:"plan_type,omitempty"`
	RateLimit             *OpenAIAccountRateLimit             `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []OpenAIAccountAdditionalRateLimit  `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *OpenAIAccountRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	FetchedAt             int64                               `json:"fetched_at"`
}

func (s *Server) queryOpenAIAccountQuota(ctx context.Context, resourceID string) (OpenAIAccountQuota, error) {
	resource, ok := s.providerResourceByID(resourceID)
	if !ok {
		return OpenAIAccountQuota{}, NewHTTPError(http.StatusNotFound, "provider_resource_not_found", "Provider resource not found")
	}
	if !isOpenAIAccountResource(resource.ResourceType) {
		return OpenAIAccountQuota{}, NewHTTPError(http.StatusBadRequest, "provider_resource_quota_unsupported", "Quota is only available for OpenAI subscription resources")
	}

	creds, err := s.store.RefreshProviderResourceCredentials(ctx, resourceID, false)
	if err != nil {
		return OpenAIAccountQuota{}, err
	}
	quota, status, err := fetchOpenAIAccountQuota(ctx, creds)
	if status != http.StatusUnauthorized {
		return quota, err
	}

	refreshed, refreshErr := s.store.RefreshProviderResourceCredentials(ctx, resourceID, true)
	if refreshErr != nil {
		return OpenAIAccountQuota{}, refreshErr
	}
	quota, _, err = fetchOpenAIAccountQuota(ctx, refreshed)
	return quota, err
}

func (s *Server) providerResourceByID(resourceID string) (ProviderResource, bool) {
	for _, resource := range s.store.ListProviderResources() {
		if resource.ID == resourceID {
			return resource, true
		}
	}
	return ProviderResource{}, false
}

func fetchOpenAIAccountQuota(ctx context.Context, creds ProviderResourceCredentials) (OpenAIAccountQuota, int, error) {
	accessToken := strings.TrimSpace(creds.AccessToken)
	accountID := strings.TrimSpace(creds.AccountID)
	if accessToken == "" {
		return OpenAIAccountQuota{}, 0, NewHTTPError(http.StatusBadRequest, "openai_account_token_missing", "OpenAI account access token is missing")
	}
	if accountID == "" {
		return OpenAIAccountQuota{}, 0, NewHTTPError(http.StatusBadRequest, "openai_account_id_missing", "OpenAI ChatGPT account ID is missing")
	}

	callCtx, cancel := context.WithTimeout(ctx, openAIAccountQuotaTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, openAIAccountQuotaURL, nil)
	if err != nil {
		return OpenAIAccountQuota{}, 0, NewHTTPError(http.StatusBadGateway, "openai_quota_request_failed", "Failed to create OpenAI quota request")
	}
	req.Host = "chatgpt.com"
	req.Header.Set("authorization", "Bearer "+accessToken)
	req.Header.Set("chatgpt-account-id", accountID)
	req.Header.Set("openai-beta", openAIAccountQuotaBeta)
	req.Header.Set("oai-language", "zh-CN")
	req.Header.Set("originator", "Codex Desktop")
	req.Header.Set("accept", "application/json")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("sec-fetch-mode", "no-cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("priority", "u=4, i")

	resp, err := (&http.Client{Timeout: openAIAccountQuotaTimeout}).Do(req)
	if err != nil {
		return OpenAIAccountQuota{}, 0, NewHTTPError(http.StatusBadGateway, "openai_quota_request_failed", fmt.Sprintf("OpenAI quota request failed: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenAIAccountQuota{}, resp.StatusCode, openAIQuotaUpstreamError(resp.StatusCode, resp.Body)
	}
	var quota OpenAIAccountQuota
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&quota); err != nil {
		return OpenAIAccountQuota{}, resp.StatusCode, NewHTTPError(http.StatusBadGateway, "openai_quota_invalid_response", "OpenAI quota endpoint returned an invalid response")
	}
	quota.FetchedAt = time.Now().UTC().Unix()
	return quota, resp.StatusCode, nil
}

func openAIQuotaUpstreamError(status int, body io.Reader) error {
	message := "OpenAI quota endpoint rejected the request"
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&payload); err == nil {
		for _, key := range []string{"detail", "message", "error"} {
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				message = strings.TrimSpace(value)
				break
			}
		}
	}
	switch status {
	case http.StatusUnauthorized:
		return NewHTTPError(http.StatusBadGateway, "openai_quota_unauthorized", message)
	case http.StatusForbidden:
		return NewHTTPError(http.StatusBadGateway, "openai_quota_forbidden", message)
	case http.StatusTooManyRequests:
		return NewHTTPError(http.StatusTooManyRequests, "openai_quota_rate_limited", message)
	default:
		return NewHTTPError(http.StatusBadGateway, "openai_quota_upstream_error", fmt.Sprintf("OpenAI quota endpoint returned %d: %s", status, message))
	}
}
