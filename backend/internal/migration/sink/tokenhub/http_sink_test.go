package tokenhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/server"
)

func writeTestCatalog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model-catalog.yaml")
	content := []byte("version: 1\nmodels:\n  - name: seeded-test-model\n    category: test\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

func newHTTPMigrationTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	config := server.Config{
		AdminToken:             "test-admin-token",
		BootstrapAdminPassword: "admin123456",
		ModelCatalogFile:       writeTestCatalog(t),
	}
	store := server.NewMemoryStoreWithConfig(config)
	if err := server.SeedDemoDataWithConfig(store, config); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}
	return httptest.NewServer(server.NewWithConfig(store, config).Handler())
}

func TestHTTPSinkApplyAndVerifyModelOnly(t *testing.T) {
	ts := newHTTPMigrationTestServer(t)
	defer ts.Close()

	sink := NewHTTPSink(NewAdminAPIClient(ts.URL, "test-admin-token", http.DefaultClient), bundle.StaticResolver{})
	migrationBundle := &bundle.CanonicalMigrationBundle{
		SchemaVersion: bundle.SchemaVersion,
		Source:        bundle.Source{Adapter: "litellm", AdapterVersion: "1.60.0"},
		GeneratedAt:   time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		Models: []bundle.ModelRef{{
			ExternalRef: bundle.ExternalRef{System: "litellm", ID: "model/http-gpt-4o-mini"},
			Spec:        server.Model{Name: "http-gpt-4o-mini", Family: "gpt-4o", Modality: "text", Status: server.StatusActive},
		}},
	}

	applyResult, err := sink.Apply(context.Background(), migrationBundle)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applyResult.Report.Created == 0 {
		t.Fatalf("expected created resources, got %+v", applyResult.Report)
	}

	verifyResult, err := sink.Verify(context.Background(), migrationBundle)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verifyResult.OK {
		t.Fatalf("expected verify success, got %+v", verifyResult)
	}
}

func TestHTTPSinkVerifyDetectsDrift(t *testing.T) {
	ts := newHTTPMigrationTestServer(t)
	defer ts.Close()

	client := NewAdminAPIClient(ts.URL, "test-admin-token", http.DefaultClient)
	sink := NewHTTPSink(client, bundle.StaticResolver{})
	migrationBundle := &bundle.CanonicalMigrationBundle{
		SchemaVersion: bundle.SchemaVersion,
		Source:        bundle.Source{Adapter: "litellm", AdapterVersion: "1.60.0"},
		GeneratedAt:   time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		Models: []bundle.ModelRef{{
			ExternalRef: bundle.ExternalRef{System: "litellm", ID: "model/http-drift"},
			Spec:        server.Model{Name: "http-drift-model", Family: "gpt-4o", Modality: "text", Status: server.StatusActive},
		}},
	}

	if _, err := sink.Apply(context.Background(), migrationBundle); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := client.UpdateModel(context.Background(), "http-drift-model", server.Model{Name: "http-drift-model", Family: "changed-family", Modality: "text", Status: server.StatusActive}); err != nil {
		t.Fatalf("update drift: %v", err)
	}

	verifyResult, err := sink.Verify(context.Background(), migrationBundle)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verifyResult.OK {
		t.Fatal("expected drift verification failure")
	}
	if len(verifyResult.Issues) == 0 {
		t.Fatal("expected verify issues")
	}
	if !strings.Contains(verifyResult.Issues[0].Message, "differs") {
		t.Fatalf("unexpected verify issues: %+v", verifyResult.Issues)
	}
}
