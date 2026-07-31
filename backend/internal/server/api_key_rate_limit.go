package server

import (
	"encoding/json"
	"strings"
)

const defaultOutputTokenReservation int64 = 4096

func requestTokenReservation(payload any) int64 {
	switch request := payload.(type) {
	case ChatCompletionRequest:
		input := estimateChatMessagesTokens(request.Messages) + estimateJSONTokens(request.Tools) +
			estimateJSONTokens(request.ToolChoice) + estimateJSONTokens(request.ResponseFormat)
		return input + outputTokenReservation(chatMaximumOutputTokens(request))
	case ResponsesRequest:
		input := EstimateTextTokens(strings.TrimSpace(request.Instructions+"\n"+ResponsesInputText(request.Input))) +
			estimateRawJSONTokens(request.raw["tools"]) + estimateRawJSONTokens(request.raw["text"])
		return input + outputTokenReservation(int64(request.MaxTokens))
	case EmbeddingsRequest:
		return EstimateTextTokens(EmbeddingInputText(request.Input))
	case map[string]json.RawMessage:
		return compactResponsesTokenReservation(request)
	default:
		return estimateJSONTokens(payload)
	}
}

func compactResponsesTokenReservation(request map[string]json.RawMessage) int64 {
	var maxOutput int64
	if raw := request["max_output_tokens"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &maxOutput)
	}
	input := estimateRawJSONTokens(request["input"]) + estimateRawJSONTokens(request["tools"]) + estimateRawJSONTokens(request["text"])
	if instructions := request["instructions"]; len(instructions) > 0 {
		input += estimateJSONTokens(instructions)
	}
	return input + outputTokenReservation(maxOutput)
}

func estimateChatMessagesTokens(messages []ChatMessage) int64 {
	var total int64
	for _, message := range messages {
		total += estimateJSONTokens(message)
	}
	return total
}

func chatMaximumOutputTokens(request ChatCompletionRequest) int64 {
	maximum := int64(request.MaxTokens)
	if raw := request.raw["max_completion_tokens"]; len(raw) > 0 {
		var compatibleMaximum int64
		if json.Unmarshal(raw, &compatibleMaximum) == nil && compatibleMaximum > maximum {
			maximum = compatibleMaximum
		}
	}
	return maximum
}

func anthropicTokenReservation(request anthropicMessagesRequest) int64 {
	return estimateAnthropicInputTokens(request.Raw) + outputTokenReservation(int64(request.MaxTokens))
}

func outputTokenReservation(requested int64) int64 {
	if requested > 0 {
		return requested
	}
	return defaultOutputTokenReservation
}

func estimateJSONTokens(value any) int64 {
	if value == nil {
		return 0
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return EstimateTextTokens(string(encoded))
}

func estimateRawJSONTokens(value json.RawMessage) int64 {
	if len(value) == 0 {
		return 0
	}
	return estimateJSONTokens(value)
}

// meteredTokens treats the Provider's total as authoritative. When an adapter
// cannot provide it, prompt and completion totals are the next-best portable
// definition: cached input is already part of prompt tokens and reasoning is
// already part of completion tokens for OpenAI-compatible usage payloads.
func meteredTokens(usage Usage) int64 {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		return maxInt64(usage.PromptTokens, 0) + maxInt64(usage.CompletionTokens, 0)
	}
	return maxInt64(usage.CachedInputTokens, 0) +
		maxInt64(usage.CacheWriteInputTokens, 0) +
		maxInt64(usage.InputAudioTokens, 0) +
		maxInt64(usage.ReasoningOutputTokens, 0) +
		maxInt64(usage.OutputAudioTokens, 0) +
		maxInt64(usage.AcceptedPredictionTokens, 0) +
		maxInt64(usage.RejectedPredictionTokens, 0)
}

func accumulateProviderUsage(total Usage, current Usage) Usage {
	combinedTotalTokens := meteredTokens(total) + meteredTokens(current)
	current.PromptTokens += total.PromptTokens
	current.CachedInputTokens += total.CachedInputTokens
	current.CacheWriteInputTokens += total.CacheWriteInputTokens
	current.InputAudioTokens += total.InputAudioTokens
	current.CompletionTokens += total.CompletionTokens
	current.ReasoningOutputTokens += total.ReasoningOutputTokens
	current.OutputAudioTokens += total.OutputAudioTokens
	current.AcceptedPredictionTokens += total.AcceptedPredictionTokens
	current.RejectedPredictionTokens += total.RejectedPredictionTokens
	current.TotalTokens = combinedTotalTokens
	current.CostUSD += total.CostUSD
	current.ProviderCostUSD += total.ProviderCostUSD
	if current.UpstreamRequestID == "" {
		current.UpstreamRequestID = total.UpstreamRequestID
	}
	if current.ServedModel == "" {
		current.ServedModel = total.ServedModel
	}
	if current.ModelETag == "" {
		current.ModelETag = total.ModelETag
	}
	if current.Transport == "" {
		current.Transport = total.Transport
	}
	if current.ResponseHeaders == nil {
		current.ResponseHeaders = total.ResponseHeaders
	}
	return current
}
