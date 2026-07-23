package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ProviderAdapter interface {
	Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error)
	ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error)
	Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error)
	Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error)
}

type ResponsesEnvelopeAdapter interface {
	ResponsesWithHeaders(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest, incoming http.Header) (any, Usage, error)
}

type ResponsesInvoker interface {
	Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error)
}

type ResponsesStreamOpener interface {
	OpenResponses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest, incoming http.Header) (*http.Response, error)
}

type ProviderResourceProber interface {
	DefaultProbeRequest() ProviderProbeRequest
	Probe(ctx context.Context, provider Provider, resource ProviderResource, request ProviderProbeRequest) (ProviderProbeResult, error)
}

type ResponsesCompactAdapter interface {
	CompactWithHeaders(ctx context.Context, provider Provider, providerModel string, body map[string]json.RawMessage, incoming http.Header) (any, Usage, error)
}

type MockAdapter struct{}

func (a MockAdapter) Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error) {
	text := "Echo: " + ChatPromptText(req.Messages)
	usage := Usage{
		PromptTokens:     EstimateTextTokens(ChatPromptText(req.Messages)),
		CompletionTokens: EstimateTextTokens(text),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return map[string]any{
		"id":      NewID("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	}, usage, nil
}

func (a MockAdapter) ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error) {
	prompt := ChatPromptText(req.Messages)
	text := "Echo: " + prompt
	parts := []string{text}
	if len([]rune(text)) > 24 {
		runes := []rune(text)
		parts = []string{string(runes[:24]), string(runes[24:])}
	}
	for _, part := range parts {
		chunk := map[string]any{
			"id":      NewID("chatcmpl"),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": part,
					},
					"finish_reason": nil,
				},
			},
		}
		payload, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return Usage{}, err
		}
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return Usage{}, err
	}
	usage := Usage{
		PromptTokens:     EstimateTextTokens(prompt),
		CompletionTokens: EstimateTextTokens(text),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage, nil
}

func (a MockAdapter) Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error) {
	input := ResponsesInputText(req.Input)
	text := "Echo: " + input
	usage := Usage{
		PromptTokens:     EstimateTextTokens(input),
		CompletionTokens: EstimateTextTokens(text),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return map[string]any{
		"id":          NewID("resp"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"model":       req.Model,
		"output_text": text,
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": text},
				},
			},
		},
		"usage": usage,
	}, usage, nil
}

func (a MockAdapter) Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error) {
	input := EmbeddingInputText(req.Input)
	vector := deterministicEmbedding(input, 8)
	usage := Usage{PromptTokens: EstimateTextTokens(input)}
	usage.TotalTokens = usage.PromptTokens
	return map[string]any{
		"object": "list",
		"model":  req.Model,
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": vector,
			},
		},
		"usage": map[string]any{
			"prompt_tokens": usage.PromptTokens,
			"total_tokens":  usage.TotalTokens,
		},
	}, usage, nil
}

type OpenAICompatibleAdapter struct {
	Client *http.Client
}

func (a OpenAICompatibleAdapter) Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error) {
	req.Model = providerModel
	var body map[string]any
	if err := a.doJSON(ctx, provider, http.MethodPost, "/chat/completions", req, &body); err != nil {
		return nil, Usage{}, err
	}
	return body, usageFromMap(body), nil
}

func (a OpenAICompatibleAdapter) ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error) {
	req.Model = providerModel
	req.Stream = true
	resp, err := a.doRaw(ctx, provider, http.MethodPost, "/chat/completions", req)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return Usage{}, err
}

func (a OpenAICompatibleAdapter) Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error) {
	req.Model = providerModel
	var body map[string]any
	if err := a.doJSON(ctx, provider, http.MethodPost, "/responses", req, &body); err != nil {
		return nil, Usage{}, err
	}
	return body, usageFromMap(body), nil
}

func (a OpenAICompatibleAdapter) Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error) {
	req.Model = providerModel
	var body map[string]any
	if err := a.doJSON(ctx, provider, http.MethodPost, "/embeddings", req, &body); err != nil {
		return nil, Usage{}, err
	}
	return body, usageFromMap(body), nil
}

func (a OpenAICompatibleAdapter) doJSON(ctx context.Context, provider Provider, method, endpoint string, payload any, target any) error {
	resp, err := a.doRaw(ctx, provider, method, endpoint, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func (a OpenAICompatibleAdapter) doRaw(ctx context.Context, provider Provider, method, endpoint string, payload any) (*http.Response, error) {
	if provider.BaseURL == "" {
		return nil, NewHTTPError(503, "provider_not_configured", "Provider base_url is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, joinURL(provider.BaseURL, endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+provider.APIKey)
	}
	applyOpenAICompatibleAccountHeaders(req, provider)
	for key, value := range provider.Headers {
		req.Header.Set(key, value)
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, NewHTTPError(statusForProvider(resp.StatusCode), "provider_error", strings.TrimSpace(string(data)))
	}
	return resp, nil
}

func applyOpenAICompatibleAccountHeaders(req *http.Request, provider Provider) {
	if req == nil || provider.Options == nil {
		return
	}
	if value := strings.TrimSpace(provider.Options["organization_id"]); value != "" && req.Header.Get("OpenAI-Organization") == "" {
		req.Header.Set("OpenAI-Organization", value)
	}
	if value := strings.TrimSpace(provider.Options["openai_project_id"]); value != "" && req.Header.Get("OpenAI-Project") == "" {
		req.Header.Set("OpenAI-Project", value)
	}
	if value := strings.TrimSpace(provider.Options["account_id"]); value != "" && req.Header.Get("X-TokenHub-Upstream-Account") == "" {
		req.Header.Set("X-TokenHub-Upstream-Account", value)
	}
}

type AzureOpenAIAdapter struct {
	Client *http.Client
}

func (a AzureOpenAIAdapter) Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error) {
	req.Model = providerModel
	var body map[string]any
	if err := a.doJSON(ctx, provider, providerModel, "/chat/completions", req, &body); err != nil {
		return nil, Usage{}, err
	}
	return body, usageFromMap(body), nil
}

func (a AzureOpenAIAdapter) ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error) {
	req.Model = providerModel
	req.Stream = true
	resp, err := a.doRaw(ctx, provider, providerModel, "/chat/completions", req)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return Usage{}, err
}

func (a AzureOpenAIAdapter) Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error) {
	return nil, Usage{}, NewHTTPError(501, "provider_capability_not_supported", "Azure responses adapter is not implemented in MVP")
}

func (a AzureOpenAIAdapter) Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error) {
	req.Model = providerModel
	var body map[string]any
	if err := a.doJSON(ctx, provider, providerModel, "/embeddings", req, &body); err != nil {
		return nil, Usage{}, err
	}
	return body, usageFromMap(body), nil
}

func (a AzureOpenAIAdapter) doJSON(ctx context.Context, provider Provider, deployment string, endpoint string, payload any, target any) error {
	resp, err := a.doRaw(ctx, provider, deployment, endpoint, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func (a AzureOpenAIAdapter) doRaw(ctx context.Context, provider Provider, deployment string, endpoint string, payload any) (*http.Response, error) {
	apiVersion := provider.Options["api_version"]
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}
	if provider.BaseURL == "" {
		return nil, NewHTTPError(503, "provider_not_configured", "Azure OpenAI base_url is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s", strings.TrimRight(provider.BaseURL, "/"), url.PathEscape(deployment), endpoint, url.QueryEscape(apiVersion))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("api-key", provider.APIKey)
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, NewHTTPError(statusForProvider(resp.StatusCode), "provider_error", strings.TrimSpace(string(data)))
	}
	return resp, nil
}

type AnthropicAdapter struct {
	Client *http.Client
}

func (a AnthropicAdapter) Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error) {
	payload := anthropicPayload(providerModel, req.Messages, req.MaxTokens)
	var body map[string]any
	if err := a.doJSON(ctx, provider, "/v1/messages", payload, &body); err != nil {
		return nil, Usage{}, err
	}
	text := anthropicText(body)
	usage := anthropicUsage(body)
	return chatResponse(req.Model, text, usage), usage, nil
}

func (a AnthropicAdapter) ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error) {
	resp, usage, err := a.Chat(ctx, provider, providerModel, req)
	if err != nil {
		return Usage{}, err
	}
	text := ""
	if asMap, ok := resp.(map[string]any); ok {
		text = choiceText(asMap)
	}
	payload, _ := json.Marshal(map[string]any{
		"id":      NewID("chatcmpl"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
	})
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return Usage{}, err
	}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	return usage, err
}

func (a AnthropicAdapter) Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error) {
	chatReq := ChatCompletionRequest{
		Model:     req.Model,
		Messages:  []ChatMessage{{Role: "user", Content: req.Input}},
		MaxTokens: req.MaxTokens,
	}
	resp, usage, err := a.Chat(ctx, provider, providerModel, chatReq)
	if err != nil {
		return nil, Usage{}, err
	}
	text := ""
	if asMap, ok := resp.(map[string]any); ok {
		text = choiceText(asMap)
	}
	return responseObject(req.Model, text, usage), usage, nil
}

func (a AnthropicAdapter) Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error) {
	return nil, Usage{}, NewHTTPError(501, "provider_capability_not_supported", "Anthropic embeddings are not supported")
}

func (a AnthropicAdapter) doJSON(ctx context.Context, provider Provider, endpoint string, payload any, target any) error {
	if provider.BaseURL == "" {
		provider.BaseURL = "https://api.anthropic.com"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(provider.BaseURL, "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	version := provider.Options["anthropic_version"]
	if version == "" {
		version = "2023-06-01"
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", provider.APIKey)
	req.Header.Set("anthropic-version", version)
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return NewHTTPError(statusForProvider(resp.StatusCode), "provider_error", strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

type GeminiAdapter struct {
	Client *http.Client
}

func (a GeminiAdapter) Chat(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest) (any, Usage, error) {
	payload := geminiPayload(req.Messages, req.MaxTokens)
	var body map[string]any
	if err := a.doJSON(ctx, provider, providerModel, ":generateContent", payload, &body); err != nil {
		return nil, Usage{}, err
	}
	text := geminiText(body)
	usage := geminiUsage(body)
	return chatResponse(req.Model, text, usage), usage, nil
}

func (a GeminiAdapter) ChatStream(ctx context.Context, provider Provider, providerModel string, req ChatCompletionRequest, w io.Writer) (Usage, error) {
	resp, usage, err := a.Chat(ctx, provider, providerModel, req)
	if err != nil {
		return Usage{}, err
	}
	text := ""
	if asMap, ok := resp.(map[string]any); ok {
		text = choiceText(asMap)
	}
	payload, _ := json.Marshal(map[string]any{
		"id":      NewID("chatcmpl"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
	})
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return Usage{}, err
	}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	return usage, err
}

func (a GeminiAdapter) Responses(ctx context.Context, provider Provider, providerModel string, req ResponsesRequest) (any, Usage, error) {
	chatReq := ChatCompletionRequest{
		Model:     req.Model,
		Messages:  []ChatMessage{{Role: "user", Content: req.Input}},
		MaxTokens: req.MaxTokens,
	}
	resp, usage, err := a.Chat(ctx, provider, providerModel, chatReq)
	if err != nil {
		return nil, Usage{}, err
	}
	text := ""
	if asMap, ok := resp.(map[string]any); ok {
		text = choiceText(asMap)
	}
	return responseObject(req.Model, text, usage), usage, nil
}

func (a GeminiAdapter) Embeddings(ctx context.Context, provider Provider, providerModel string, req EmbeddingsRequest) (any, Usage, error) {
	payload := map[string]any{
		"content": map[string]any{
			"parts": []map[string]any{{"text": EmbeddingInputText(req.Input)}},
		},
	}
	var body map[string]any
	if err := a.doJSON(ctx, provider, providerModel, ":embedContent", payload, &body); err != nil {
		return nil, Usage{}, err
	}
	values := []any{}
	if embedding, ok := body["embedding"].(map[string]any); ok {
		if raw, ok := embedding["values"].([]any); ok {
			values = raw
		}
	}
	usage := Usage{PromptTokens: EstimateTextTokens(EmbeddingInputText(req.Input))}
	usage.TotalTokens = usage.PromptTokens
	return map[string]any{
		"object": "list",
		"model":  req.Model,
		"data": []map[string]any{
			{"object": "embedding", "index": 0, "embedding": values},
		},
		"usage": map[string]any{"prompt_tokens": usage.PromptTokens, "total_tokens": usage.TotalTokens},
	}, usage, nil
}

func (a GeminiAdapter) doJSON(ctx context.Context, provider Provider, model string, action string, payload any, target any) error {
	if provider.BaseURL == "" {
		provider.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/models/%s%s?key=%s", strings.TrimRight(provider.BaseURL, "/"), url.PathEscape(model), action, url.QueryEscape(provider.APIKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return NewHTTPError(statusForProvider(resp.StatusCode), "provider_error", strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func anthropicPayload(model string, messages []ChatMessage, maxTokens int) map[string]any {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	system := []string{}
	converted := []map[string]any{}
	for _, message := range messages {
		if message.Role == "system" {
			system = append(system, contentToText(message.Content))
			continue
		}
		role := message.Role
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}
		converted = append(converted, map[string]any{"role": role, "content": contentToText(message.Content)})
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   converted,
	}
	if len(system) > 0 {
		payload["system"] = strings.Join(system, "\n")
	}
	return payload
}

func geminiPayload(messages []ChatMessage, maxTokens int) map[string]any {
	contents := []map[string]any{}
	for _, message := range messages {
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		if message.Role == "system" {
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": contentToText(message.Content)}}})
			continue
		}
		contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": contentToText(message.Content)}}})
	}
	payload := map[string]any{"contents": contents}
	if maxTokens > 0 {
		payload["generationConfig"] = map[string]any{"maxOutputTokens": maxTokens}
	}
	return payload
}

func anthropicText(body map[string]any) string {
	content, _ := body["content"].([]any)
	parts := []string{}
	for _, item := range content {
		asMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := asMap["text"].(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

func anthropicUsage(body map[string]any) Usage {
	usageMap, _ := body["usage"].(map[string]any)
	usage := Usage{
		PromptTokens:     int64FromAny(usageMap["input_tokens"]),
		CompletionTokens: int64FromAny(usageMap["output_tokens"]),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage
}

func geminiText(body map[string]any) string {
	candidates, _ := body["candidates"].([]any)
	if len(candidates) == 0 {
		return ""
	}
	candidate, _ := candidates[0].(map[string]any)
	content, _ := candidate["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	text := []string{}
	for _, part := range parts {
		asMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := asMap["text"].(string); ok {
			text = append(text, value)
		}
	}
	return strings.Join(text, "")
}

func geminiUsage(body map[string]any) Usage {
	usageMap, _ := body["usageMetadata"].(map[string]any)
	usage := Usage{
		PromptTokens:     int64FromAny(usageMap["promptTokenCount"]),
		CompletionTokens: int64FromAny(usageMap["candidatesTokenCount"]),
		TotalTokens:      int64FromAny(usageMap["totalTokenCount"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func usageFromMap(body map[string]any) Usage {
	usageMap, _ := body["usage"].(map[string]any)
	inputDetails, _ := usageMap["input_tokens_details"].(map[string]any)
	outputDetails, _ := usageMap["output_tokens_details"].(map[string]any)
	usage := Usage{
		PromptTokens:          int64FromAny(firstNonNil(usageMap["prompt_tokens"], usageMap["input_tokens"])),
		CachedInputTokens:     int64FromAny(firstNonNil(usageMap["cached_input_tokens"], inputDetails["cached_tokens"])),
		CacheWriteInputTokens: int64FromAny(firstNonNil(usageMap["cache_write_input_tokens"], inputDetails["cache_write_tokens"])),
		CompletionTokens:      int64FromAny(firstNonNil(usageMap["completion_tokens"], usageMap["output_tokens"])),
		ReasoningOutputTokens: int64FromAny(firstNonNil(usageMap["reasoning_output_tokens"], outputDetails["reasoning_tokens"])),
		TotalTokens:           int64FromAny(usageMap["total_tokens"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func chatResponse(model string, text string, usage Usage) map[string]any {
	return map[string]any{
		"id":      NewID("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": text},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	}
}

func responseObject(model string, text string, usage Usage) map[string]any {
	return map[string]any{
		"id":          NewID("resp"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"model":       model,
		"output_text": text,
		"output": []map[string]any{
			{
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]any{{"type": "output_text", "text": text}},
			},
		},
		"usage": usage,
	}
}

func choiceText(resp map[string]any) string {
	choices, _ := resp["choices"].([]map[string]any)
	if len(choices) == 0 {
		rawChoices, _ := resp["choices"].([]any)
		if len(rawChoices) == 0 {
			return ""
		}
		first, _ := rawChoices[0].(map[string]any)
		message, _ := first["message"].(map[string]any)
		text, _ := message["content"].(string)
		return text
	}
	message, _ := choices[0]["message"].(map[string]any)
	text, _ := message["content"].(string)
	return text
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		v, _ := typed.Int64()
		return v
	default:
		return 0
	}
}

func deterministicEmbedding(text string, dims int) []float64 {
	if dims <= 0 {
		dims = 8
	}
	vector := make([]float64, dims)
	if text == "" {
		return vector
	}
	for idx, r := range []rune(text) {
		slot := idx % dims
		vector[slot] += float64((int(r)%97)+1) / 100
	}
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return vector
	}
	for idx := range vector {
		vector[idx] = math.Round((vector[idx]/norm)*1_000_000) / 1_000_000
	}
	return vector
}

func joinURL(base string, endpoint string) string {
	base = strings.TrimRight(base, "/")
	endpoint = "/" + strings.TrimLeft(endpoint, "/")
	return base + endpoint
}

func statusForProvider(status int) int {
	if status == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	if status >= 500 {
		return http.StatusBadGateway
	}
	return http.StatusBadGateway
}
