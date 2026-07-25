package litellm

import "testing"

func TestSplitProviderModelPreservesNestedModelPath(t *testing.T) {
	providerType, providerName := splitProviderModel("openai/BohrClaw/qwen3.5-flash")
	if providerType != "openai" {
		t.Fatalf("unexpected provider type %q", providerType)
	}
	if providerName != "BohrClaw/qwen3.5-flash" {
		t.Fatalf("unexpected provider name %q", providerName)
	}
}
