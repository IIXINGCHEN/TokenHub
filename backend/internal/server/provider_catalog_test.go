package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	if source != "local-provider-catalog" || len(summaries) != providerCatalogMinProviders+1 ||
		!providerCatalogContains(summaries, "fresh-provider") {
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

func TestProviderCatalogServiceInitializeReloadsLocalCatalogOnEveryStart(t *testing.T) {
	store := NewMemoryStore()
	catalogFile := filepath.Join(t.TempDir(), "provider-catalog.json")
	writeProviderCatalogFixture(t, catalogFile)

	service := newProviderCatalogService(store, catalogFile)
	initialized, err := service.Initialize(context.Background())
	if err != nil || !initialized {
		t.Fatalf("expected first local catalog refresh, initialized=%v err=%v", initialized, err)
	}
	replaceProviderCatalogFixtureURL(t, catalogFile, "fresh-provider", "https://refreshed-provider.example/v1")

	initialized, err = service.Initialize(context.Background())
	if err != nil || !initialized {
		t.Fatalf("expected second local catalog refresh, initialized=%v err=%v", initialized, err)
	}
	entry, source, ok, err := service.Get(context.Background(), "fresh-provider", false)
	if err != nil || !ok || source != "local-provider-catalog" || entry.BaseURL != "https://refreshed-provider.example/v1" {
		t.Fatalf("expected refreshed provider entry, source=%q entry=%+v ok=%v err=%v", source, entry, ok, err)
	}
}

func TestProviderCatalogServiceInitializeRetainsLocalSnapshotWhenFileIsMissing(t *testing.T) {
	store := NewMemoryStore()
	catalogFile := filepath.Join(t.TempDir(), "provider-catalog.json")
	writeProviderCatalogFixture(t, catalogFile)
	if _, _, err := newProviderCatalogService(store, catalogFile).List(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	service := newProviderCatalogService(store, filepath.Join(t.TempDir(), "missing.json"))
	if initialized, err := service.Initialize(context.Background()); err == nil || initialized {
		t.Fatalf("expected failed initialization, initialized=%v err=%v", initialized, err)
	}
	entries, source, err := service.List(context.Background(), false)
	if err != nil || source != "local-provider-catalog" || !providerCatalogContains(entries, "fresh-provider") {
		t.Fatalf("expected retained local snapshot, source=%q entries=%+v err=%v", source, entries, err)
	}
}

func TestProviderCatalogServiceInitializeSerializesConcurrentRefreshes(t *testing.T) {
	store := NewMemoryStore()
	catalogFile := filepath.Join(t.TempDir(), "provider-catalog.json")
	writeProviderCatalogFixture(t, catalogFile)

	probe := newProviderCatalogConcurrentInitializeProbe()
	services := []*providerCatalogService{
		newProviderCatalogService(&providerCatalogConcurrentInitializeStore{Store: store, probe: probe}, catalogFile),
		newProviderCatalogService(&providerCatalogConcurrentInitializeStore{Store: store, probe: probe}, catalogFile),
	}
	errors := make(chan error, len(services))
	for _, service := range services {
		go func(service *providerCatalogService) {
			_, err := service.Initialize(context.Background())
			errors <- err
		}(service)
	}

	for range services {
		select {
		case err := <-errors:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent catalog initialization did not complete")
		}
	}

	if writes := probe.localSnapshotWriteCount(); writes != len(services) {
		t.Fatalf("expected %d serialized local catalog refreshes, got %d", len(services), writes)
	}
	if max := probe.maxConcurrentLocalSnapshotWrites(); max != 1 {
		t.Fatalf("expected serialized snapshot writes, max concurrent writes=%d", max)
	}
	entries, source, err := services[0].List(context.Background(), false)
	if err != nil || source != "local-provider-catalog" || !providerCatalogContains(entries, "fresh-provider") {
		t.Fatalf("unexpected concurrently upgraded catalog: source=%q entries=%+v err=%v", source, entries, err)
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

func replaceProviderCatalogFixtureURL(t *testing.T, catalogFile string, providerID string, baseURL string) {
	t.Helper()
	content, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatal(err)
	}
	provider, ok := payload.Providers[providerID]
	if !ok {
		t.Fatalf("missing fixture provider %q", providerID)
	}
	provider["api"] = baseURL
	content, err = json.Marshal(payload)
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

type providerCatalogConcurrentInitializeStore struct {
	Store
	probe *providerCatalogConcurrentInitializeProbe
}

func (s *providerCatalogConcurrentInitializeStore) SaveProviderCatalogSnapshot(entries []ProviderCatalogEntry, source string, fetchedAt time.Time) error {
	if source == "local-provider-catalog" {
		s.probe.beginLocalSnapshotWrite()
	}
	err := s.Store.SaveProviderCatalogSnapshot(entries, source, fetchedAt)
	if source == "local-provider-catalog" {
		s.probe.endLocalSnapshotWrite(err == nil)
	}
	return err
}

type providerCatalogConcurrentInitializeProbe struct {
	mu                        sync.Mutex
	localSnapshotWrites       int
	activeLocalSnapshotWrites int
	maxLocalSnapshotWrites    int
}

func newProviderCatalogConcurrentInitializeProbe() *providerCatalogConcurrentInitializeProbe {
	return &providerCatalogConcurrentInitializeProbe{}
}

func (p *providerCatalogConcurrentInitializeProbe) beginLocalSnapshotWrite() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeLocalSnapshotWrites++
	if p.activeLocalSnapshotWrites > p.maxLocalSnapshotWrites {
		p.maxLocalSnapshotWrites = p.activeLocalSnapshotWrites
	}
}

func (p *providerCatalogConcurrentInitializeProbe) endLocalSnapshotWrite(succeeded bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeLocalSnapshotWrites--
	if succeeded {
		p.localSnapshotWrites++
	}
}

func (p *providerCatalogConcurrentInitializeProbe) localSnapshotWriteCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.localSnapshotWrites
}

func (p *providerCatalogConcurrentInitializeProbe) maxConcurrentLocalSnapshotWrites() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxLocalSnapshotWrites
}
