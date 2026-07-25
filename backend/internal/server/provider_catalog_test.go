package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProviderCatalogUsesTrackedLocalFile(t *testing.T) {
	catalogFile := filepath.Join(t.TempDir(), "provider-catalog.json")
	content := []byte(`{
  "updated_at": "2026-07-25T00:00:00Z",
  "providers": {
    "test-provider": {
      "id": "test-provider",
      "name": "Test Provider",
      "display_name": "Test Provider",
      "api": "https://example.test/v1",
      "models": [{"id": "test-model", "name": "Test Model", "type": "chat"}]
    }
  }
}`)
	if err := os.WriteFile(catalogFile, content, 0o600); err != nil {
		t.Fatal(err)
	}

	entries, source, err := LoadProviderCatalog(catalogFile, true)
	if err != nil {
		t.Fatal(err)
	}
	if source != "local-provider-catalog" {
		t.Fatalf("expected local catalog source, got %q", source)
	}
	if len(entries) != 2 { // test-provider plus the built-in custom template
		t.Fatalf("expected local provider and custom template, got %#v", entries)
	}

	entry, source, ok, err := GetProviderCatalogEntry(catalogFile, "test-provider", false)
	if err != nil || !ok {
		t.Fatalf("expected local provider entry, ok=%v err=%v", ok, err)
	}
	if source != "local-provider-catalog" || entry.BaseURL != "https://example.test/v1" || len(entry.Models) != 1 || entry.Models[0].ID != "test-model" {
		t.Fatalf("unexpected local provider entry: source=%q entry=%+v", source, entry)
	}
}

func TestLoadProviderCatalogFallsBackToBuiltinTemplates(t *testing.T) {
	entries, source, err := LoadProviderCatalog(filepath.Join(t.TempDir(), "missing.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	if source != "builtin" || len(entries) == 0 {
		t.Fatalf("expected builtin fallback, source=%q entries=%d", source, len(entries))
	}
}
