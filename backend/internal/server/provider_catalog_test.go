package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderCatalogServiceReloadsTrackedLocalFile(t *testing.T) {
	store := NewMemoryStore()
	catalogFile := filepath.Join(t.TempDir(), "provider-catalog.json")
	writeProviderCatalogFixture(t, catalogFile)

	service := newProviderCatalogService(store, catalogFile)
	summaries, source, err := service.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if source != "local-provider-catalog" || len(summaries) != providerCatalogMinProviders+1 || !providerCatalogContains(summaries, "fresh-provider") {
		t.Fatalf("unexpected local catalog summary: source=%q entries=%+v", source, summaries)
	}
	if len(summaries[0].Models) != 0 {
		t.Fatalf("list should return summaries without models: %+v", summaries[0])
	}

	entry, source, ok, err := service.Get(context.Background(), "fresh-provider", false)
	if err != nil || !ok {
		t.Fatalf("expected local provider entry, ok=%v err=%v", ok, err)
	}
	if source != "local-provider-catalog" || len(entry.Models) != 5 {
		t.Fatalf("unexpected local provider entry: source=%q entry=%+v", source, entry)
	}

	restarted := newProviderCatalogService(store, filepath.Join(t.TempDir(), "missing.json"))
	persisted, source, err := restarted.List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if source != "local-provider-catalog" || !providerCatalogContains(persisted, "fresh-provider") {
		t.Fatalf("expected persisted local catalog, source=%q entries=%+v", source, persisted)
	}
}

func TestProviderCatalogServiceRetainsBuiltinSnapshotWhenLocalFileIsMissing(t *testing.T) {
	store := NewMemoryStore()
	service := newProviderCatalogService(store, filepath.Join(t.TempDir(), "missing.json"))

	entries, source, err := service.List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if source != "builtin" || len(entries) == 0 {
		t.Fatalf("expected builtin catalog, source=%q entries=%d", source, len(entries))
	}
	if initialized, err := service.Initialize(context.Background()); err == nil || initialized {
		t.Fatalf("expected missing local catalog to keep builtin snapshot, initialized=%v err=%v", initialized, err)
	}

	entries, source, err = service.List(context.Background(), false)
	if err != nil || source != "builtin" || len(entries) == 0 {
		t.Fatalf("expected builtin snapshot to remain available, source=%q entries=%d err=%v", source, len(entries), err)
	}
}

func TestBootstrapSeedsProviderCatalogSnapshot(t *testing.T) {
	store := NewMemoryStore()
	config := Config{
		BootstrapAdminPassword: "provider-catalog-bootstrap-password",
		ModelCatalogFile:       "../../../data/model-catalog.yaml",
	}
	if err := BootstrapBaseDataWithConfig(store, config); err != nil {
		t.Fatal(err)
	}

	entries, source, _, found, err := store.LoadProviderCatalogSnapshot(false)
	if err != nil {
		t.Fatal(err)
	}
	if !found || source != "builtin" || len(entries) < 5 {
		t.Fatalf("expected builtin provider catalog, found=%v source=%q entries=%d", found, source, len(entries))
	}
}

func writeProviderCatalogFixture(t *testing.T, catalogFile string) {
	t.Helper()
	providers := map[string]any{}
	ids := []string{"openai", "anthropic", "google", "fresh-provider"}
	for index := len(ids); index < providerCatalogMinProviders; index++ {
		ids = append(ids, fmt.Sprintf("provider-%02d", index))
	}
	for _, id := range ids {
		models := make([]map[string]any, 0, 5)
		for modelIndex := 0; modelIndex < 5; modelIndex++ {
			modelID := fmt.Sprintf("%s-model-%d", id, modelIndex)
			models = append(models, map[string]any{"id": modelID, "name": modelID})
		}
		providers[id] = map[string]any{
			"name":   id,
			"api":    "https://" + id + ".example/v1",
			"models": models,
		}
	}
	content, err := json.Marshal(map[string]any{"providers": providers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func providerCatalogContains(entries []ProviderCatalogEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
