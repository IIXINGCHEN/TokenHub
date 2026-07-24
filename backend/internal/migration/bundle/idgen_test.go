package bundle

import "testing"

func TestIDStrategyValid(t *testing.T) {
	if !IDStrategyStable.Valid() || !IDStrategyPrefixed.Valid() || !IDStrategySource.Valid() {
		t.Fatal("expected built-in strategies to be valid")
	}
	if IDStrategy("unknown").Valid() {
		t.Fatal("expected unknown strategy to be invalid")
	}
}

func TestMintIDStable(t *testing.T) {
	first, err := MintID(IDStrategyStable, "litellm", "openai")
	if err != nil {
		t.Fatalf("mint stable id: %v", err)
	}
	second, err := MintID(IDStrategyStable, "litellm", "openai")
	if err != nil {
		t.Fatalf("mint stable id again: %v", err)
	}
	if first != second {
		t.Fatalf("stable ids differ: %q vs %q", first, second)
	}
}

func TestMintIDPrefixed(t *testing.T) {
	got, err := MintID(IDStrategyPrefixed, "litellm", "openai")
	if err != nil {
		t.Fatalf("mint prefixed id: %v", err)
	}
	if got != "litellm/openai" {
		t.Fatalf("prefixed id mismatch: got %q", got)
	}
}

func TestMintIDSource(t *testing.T) {
	got, err := MintID(IDStrategySource, "litellm", "openai")
	if err != nil {
		t.Fatalf("mint source id: %v", err)
	}
	if got != "openai" {
		t.Fatalf("source id mismatch: got %q", got)
	}
}

func TestMintIDRejectsInvalidInput(t *testing.T) {
	if _, err := MintID(IDStrategyStable, "", "openai"); err == nil {
		t.Fatal("expected missing system to fail")
	}
	if _, err := MintID(IDStrategyStable, "litellm", ""); err == nil {
		t.Fatal("expected missing external id to fail")
	}
	if _, err := MintID(IDStrategy("bad"), "litellm", "openai"); err == nil {
		t.Fatal("expected unknown strategy to fail")
	}
}
