package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openAICodexResponsesURL  = "https://chatgpt.com/backend-api/codex/responses"
	openAICodexVersion       = "0.145.0"
	openAICodexUserAgent     = "codex_cli_rs/0.145.0 (Mac OS 15.0.0; arm64) xterm-256color"
	openAICodexInstructions  = "You are Codex, a coding agent. Follow the user's request and return a clear, accurate result."
	openAICodexFastTestModel = "gpt-5.6-luna"
)

var openAICodexTestModels = []string{
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex-spark",
}

type CodexSubscriptionAdapter struct {
	Client             *http.Client
	RefreshCredentials func(context.Context, string, bool) (ProviderResourceCredentials, error)
}

type codexSubscriptionTestRequest struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Speed           string `json:"speed"`
	Prompt          string `json:"prompt"`
}

type codexSubscriptionTestResponse struct {
	ResourceID          string         `json:"resource_id"`
	Model               string         `json:"model"`
	ReasoningEffort     string         `json:"reasoning_effort"`
	Speed               string         `json:"speed"`
	UpstreamServiceTier string         `json:"upstream_service_tier,omitempty"`
	OutputText          string         `json:"output_text"`
	Usage               Usage          `json:"usage"`
	LatencyMS           int64          `json:"latency_ms"`
	Response            map[string]any `json:"response"`
}

func (a CodexSubscriptionAdapter) Chat(context.Context, Provider, string, ChatCompletionRequest) (any, Usage, error) {
	return nil, Usage{}, NewHTTPError(http.StatusBadRequest, "codex_subscription_endpoint_unsupported", "Codex Subscription supports the Responses API")
}

func (a CodexSubscriptionAdapter) ChatStream(context.Context, Provider, string, ChatCompletionRequest, io.Writer) (Usage, error) {
	return Usage{}, NewHTTPError(http.StatusBadRequest, "codex_subscription_endpoint_unsupported", "Codex Subscription supports the Responses API")
}

func (a CodexSubscriptionAdapter) Embeddings(context.Context, Provider, string, EmbeddingsRequest) (any, Usage, error) {
	return nil, Usage{}, NewHTTPError(http.StatusBadRequest, "codex_subscription_endpoint_unsupported", "Codex Subscription supports the Responses API")
}

func (a CodexSubscriptionAdapter) Responses(ctx context.Context, provider Provider, providerModel string, request ResponsesRequest) (any, Usage, error) {
	resp, err := a.OpenResponses(ctx, provider, providerModel, request, nil)
	if err != nil {
		return nil, Usage{}, err
	}
	defer resp.Body.Close()
	response, outputText, usage, err := consumeCodexResponsesStream(resp.Body, nil)
	if err != nil {
		return nil, usage, err
	}
	if outputText != "" {
		response["output_text"] = outputText
		if output, _ := response["output"].([]any); len(output) == 0 {
			response["output"] = []map[string]any{{
				"type":    "message",
				"role":    "assistant",
				"status":  "completed",
				"content": []map[string]any{{"type": "output_text", "text": outputText, "annotations": []any{}}},
			}}
		}
	}
	return response, usage, nil
}

func (a CodexSubscriptionAdapter) OpenResponses(ctx context.Context, provider Provider, providerModel string, request ResponsesRequest, incoming http.Header) (*http.Response, error) {
	resourceID := strings.TrimSpace(provider.Options["resource_id"])
	if resourceID == "" {
		return nil, NewHTTPError(http.StatusBadRequest, "provider_resource_missing", "Codex Subscription resource is missing")
	}
	if a.RefreshCredentials == nil {
		return nil, NewHTTPError(http.StatusServiceUnavailable, "provider_credentials_unavailable", "Codex Subscription credentials are unavailable")
	}
	creds, err := a.RefreshCredentials(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	resp, err := a.openResponsesWithCredentials(ctx, creds, providerModel, request, incoming)
	if err == nil || AsHTTPError(err).Status != http.StatusUnauthorized {
		return resp, err
	}
	creds, refreshErr := a.RefreshCredentials(ctx, resourceID, true)
	if refreshErr != nil {
		return nil, refreshErr
	}
	return a.openResponsesWithCredentials(ctx, creds, providerModel, request, incoming)
}

func (a CodexSubscriptionAdapter) openResponsesWithCredentials(ctx context.Context, creds ProviderResourceCredentials, providerModel string, request ResponsesRequest, incoming http.Header) (*http.Response, error) {
	accessToken := strings.TrimSpace(creds.AccessToken)
	accountID := strings.TrimSpace(creds.AccountID)
	if accessToken == "" {
		return nil, NewHTTPError(http.StatusBadRequest, "openai_account_token_missing", "OpenAI account access token is missing")
	}
	if accountID == "" {
		return nil, NewHTTPError(http.StatusBadRequest, "openai_account_id_missing", "OpenAI ChatGPT account ID is missing")
	}
	request.Model = strings.TrimSpace(providerModel)
	if inputText, ok := request.Input.(string); ok {
		request.Input = []map[string]any{{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": inputText}},
		}}
	}
	request.Stream = true
	store := false
	request.Store = &store
	if strings.TrimSpace(request.Instructions) == "" {
		request.Instructions = openAICodexInstructions
	}
	if strings.EqualFold(strings.TrimSpace(request.ServiceTier), "fast") {
		request.ServiceTier = "priority"
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, NewHTTPError(http.StatusBadRequest, "invalid_request", err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICodexResponsesURL, bytes.NewReader(payload))
	if err != nil {
		return nil, NewHTTPError(http.StatusBadGateway, "codex_request_failed", err.Error())
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("ChatGPT-Account-ID", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("User-Agent", openAICodexUserAgent)
	req.Header.Set("Version", openAICodexVersion)
	for _, key := range []string{"session_id", "conversation_id", "x-codex-turn-metadata", "accept-language"} {
		if value := strings.TrimSpace(incoming.Get(key)); value != "" {
			req.Header.Set(key, value)
		}
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, NewHTTPError(http.StatusBadGateway, "codex_request_failed", fmt.Sprintf("Codex request failed: %v", err))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, NewHTTPError(resp.StatusCode, "codex_upstream_error", message)
	}
	return resp, nil
}

func consumeCodexResponsesStream(body io.Reader, destination io.Writer) (map[string]any, string, Usage, error) {
	reader := bufio.NewReader(body)
	var response map[string]any
	var textBuilder strings.Builder
	var usage Usage
	completed := false
	for {
		line, err := reader.ReadString('\n')
		if line != "" && destination != nil {
			if _, writeErr := io.WriteString(destination, line); writeErr != nil {
				return response, textBuilder.String(), usage, writeErr
			}
			if flusher, ok := destination.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data != "" && data != "[DONE]" {
				var event map[string]any
				if json.Unmarshal([]byte(data), &event) == nil {
					switch event["type"] {
					case "response.output_text.delta":
						if delta, ok := event["delta"].(string); ok {
							textBuilder.WriteString(delta)
						}
					case "response.completed", "response.done":
						response, _ = event["response"].(map[string]any)
						usage = usageFromMap(response)
						completed = true
					case "response.failed", "error":
						return response, textBuilder.String(), usage, codexStreamEventError(event)
					}
				}
			}
		}
		if completed {
			if textBuilder.Len() == 0 {
				textBuilder.WriteString(codexResponseOutputText(response))
			}
			return response, textBuilder.String(), usage, nil
		}
		if err != nil {
			if err == io.EOF {
				return response, textBuilder.String(), usage, NewHTTPError(http.StatusBadGateway, "codex_stream_incomplete", "Codex stream ended before response.completed")
			}
			return response, textBuilder.String(), usage, NewHTTPError(http.StatusBadGateway, "codex_stream_failed", err.Error())
		}
	}
}

func (s *Server) handleStreamingResponses(w http.ResponseWriter, r *http.Request, routed RoutedCall, request ResponsesRequest) {
	resp, route, _, attempts, err := executeRoutedWithStore(s.store, routed, func(route RouteSelection) (*http.Response, Usage, error) {
		prepared, err := s.prepareRouteForUpstream(r.Context(), route)
		if err != nil {
			return nil, Usage{}, err
		}
		adapter, err := s.responsesAdapterForRoute(prepared)
		if err != nil {
			return nil, Usage{}, err
		}
		codex, ok := adapter.(CodexSubscriptionAdapter)
		if !ok {
			return nil, Usage{}, NewHTTPError(http.StatusBadRequest, "responses_stream_unsupported", "Streaming Responses is currently available through Codex Subscription resources")
		}
		opened, err := codex.OpenResponses(r.Context(), prepared.Provider, prepared.ProviderModel, request, r.Header)
		return opened, Usage{}, err
	})
	if err != nil {
		s.finishFailedRoutedCall(r, routed, attempts, err)
		s.recordRequestPayload(routed.Call.RequestID, request, auditErrorPayload(err, routed.Call.RequestID))
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	w.Header().Set("x-accel-buffering", "no")
	w.Header().Set("x-request-id", routed.Call.RequestID)
	s.writeRouteHeaders(w, routed.Call, route, len(attempts))
	w.WriteHeader(http.StatusOK)
	response, _, usage, streamErr := consumeCodexResponsesStream(resp.Body, w)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	s.store.MarkRouteUsed(route.Route.ID)
	s.store.MarkProviderResourceUsed(routeResourceID(route))
	s.store.RecordRouteAttempts(routed.Call.RequestID, attempts)
	if streamErr != nil {
		httpErr := AsHTTPError(streamErr)
		s.store.FinishCall(routed.Call, route, usage, httpErr.Status, httpErr.Code, s.clientIP(r), r.UserAgent())
		s.recordRequestPayload(routed.Call.RequestID, request, auditErrorPayload(streamErr, routed.Call.RequestID))
		return
	}
	s.store.FinishCall(routed.Call, route, usage, http.StatusOK, "", s.clientIP(r), r.UserAgent())
	s.recordRequestPayload(routed.Call.RequestID, request, response)
}

func codexStreamEventError(event map[string]any) error {
	message := "Codex response failed"
	if errorPayload, ok := event["error"].(map[string]any); ok {
		if value, ok := errorPayload["message"].(string); ok && strings.TrimSpace(value) != "" {
			message = value
		}
	}
	if response, ok := event["response"].(map[string]any); ok {
		if errorPayload, ok := response["error"].(map[string]any); ok {
			if value, ok := errorPayload["message"].(string); ok && strings.TrimSpace(value) != "" {
				message = value
			}
		}
	}
	return NewHTTPError(http.StatusBadGateway, "codex_response_failed", message)
}

func codexResponseOutputText(response map[string]any) string {
	if value, ok := response["output_text"].(string); ok {
		return value
	}
	var result strings.Builder
	outputs, _ := response["output"].([]any)
	for _, output := range outputs {
		item, _ := output.(map[string]any)
		content, _ := item["content"].([]any)
		for _, part := range content {
			payload, _ := part.(map[string]any)
			if payload["type"] == "output_text" {
				if value, ok := payload["text"].(string); ok {
					result.WriteString(value)
				}
			}
		}
	}
	return result.String()
}

func (s *Server) testCodexSubscription(ctx context.Context, resourceID string, request codexSubscriptionTestRequest) (codexSubscriptionTestResponse, error) {
	resource, ok := s.providerResourceByID(resourceID)
	if !ok {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusNotFound, "provider_resource_not_found", "Provider resource not found")
	}
	if !isOpenAIAccountResource(resource.ResourceType) {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "provider_resource_test_unsupported", "This real test is only available for Codex Subscription resources")
	}
	request.Model = strings.TrimSpace(request.Model)
	request.ReasoningEffort = strings.ToLower(strings.TrimSpace(request.ReasoningEffort))
	request.Speed = strings.ToLower(strings.TrimSpace(request.Speed))
	request.Prompt = strings.TrimSpace(request.Prompt)
	if !stringInList(request.Model, openAICodexTestModels) {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "codex_model_invalid", "Select a supported Codex model")
	}
	if !stringInList(request.ReasoningEffort, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}) {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "codex_reasoning_effort_invalid", "Select a supported reasoning effort")
	}
	if request.Speed == "" {
		request.Speed = "standard"
	}
	if request.Speed != "standard" && request.Speed != "fast" {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "codex_speed_invalid", "Speed must be standard or fast")
	}
	if request.Speed == "fast" && request.Model != openAICodexFastTestModel {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "codex_fast_test_model_blocked", "Fast-mode tests are restricted to the lower-cost gpt-5.6-luna model")
	}
	if request.Prompt == "" {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusBadRequest, "codex_prompt_missing", "Enter a real prompt before testing")
	}
	provider, ok := s.providerByID(resource.ProviderID)
	if !ok {
		return codexSubscriptionTestResponse{}, NewHTTPError(http.StatusNotFound, "provider_not_found", "Provider not found")
	}
	if provider.Options == nil {
		provider.Options = map[string]string{}
	}
	provider.Options["resource_id"] = resource.ID
	responsesRequest := ResponsesRequest{
		Model: request.Model,
		Input: []map[string]any{{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": request.Prompt}},
		}},
		Reasoning: &ResponsesReasoning{Effort: request.ReasoningEffort},
	}
	if request.Speed == "fast" {
		responsesRequest.ServiceTier = "priority"
	}
	startedAt := time.Now()
	resp, err := s.codexSubscription.OpenResponses(ctx, provider, request.Model, responsesRequest, nil)
	if err != nil {
		_, _ = s.store.SetProviderResourceHealth(resourceID, false)
		return codexSubscriptionTestResponse{}, err
	}
	defer resp.Body.Close()
	response, outputText, usage, err := consumeCodexResponsesStream(resp.Body, nil)
	if err != nil {
		_, _ = s.store.SetProviderResourceHealth(resourceID, false)
		return codexSubscriptionTestResponse{}, err
	}
	_, _ = s.store.SetProviderResourceHealth(resourceID, true)
	return codexSubscriptionTestResponse{
		ResourceID:          resourceID,
		Model:               request.Model,
		ReasoningEffort:     request.ReasoningEffort,
		Speed:               request.Speed,
		UpstreamServiceTier: stringFromMap(response, "service_tier"),
		OutputText:          outputText,
		Usage:               usage,
		LatencyMS:           time.Since(startedAt).Milliseconds(),
		Response:            response,
	}, nil
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (s *Server) providerByID(providerID string) (Provider, bool) {
	for _, provider := range s.store.ListProviders() {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return Provider{}, false
}

func stringInList(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
