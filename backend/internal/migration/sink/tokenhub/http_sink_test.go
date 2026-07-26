package tokenhub

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	configureMigrationTestSMTP(t, store)
	return httptest.NewServer(server.NewWithConfig(store, config).Handler())
}

// configureMigrationTestSMTP registers an in-process fake SMTP channel so
// that the user import endpoint, which mails password resets, is usable.
func configureMigrationTestSMTP(t *testing.T, store *server.GormStore) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveMigrationTestSMTP(conn)
		}
	}()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split smtp addr: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse smtp port: %v", err)
	}
	store.CreateResource("notification-channels", server.AdminResource{
		Name:   "Migration Test SMTP",
		Status: server.StatusActive,
		Fields: map[string]any{
			"type":      "email",
			"smtp_host": host,
			"smtp_port": port,
			"smtp_from": "tokenhub@example.com",
		},
	})
}

func serveMigrationTestSMTP(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 migration-test ready")
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		text := strings.TrimSpace(line)
		if inData {
			if text == "." {
				inData = false
				write("250 OK")
			}
			continue
		}
		upper := strings.ToUpper(text)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			write("250 migration-test")
		case strings.HasPrefix(upper, "DATA"):
			inData = true
			write("354 end data with <CR><LF>.<CR><LF>")
		case strings.HasPrefix(upper, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
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

// TestHTTPSinkApplyUserCreatesAndUpdates covers the HTTP user path: a new
// user must be created with its team resolved from TeamRef and registered in
// the ref index under the server-assigned ID, and a subsequent apply with a
// changed spec must issue a real update instead of only reporting one.
func TestHTTPSinkApplyUserCreatesAndUpdates(t *testing.T) {
	ts := newHTTPMigrationTestServer(t)
	defer ts.Close()

	client := NewAdminAPIClient(ts.URL, "test-admin-token", http.DefaultClient)
	sink := NewHTTPSink(client, bundle.StaticResolver{})
	migrationBundle := &bundle.CanonicalMigrationBundle{
		SchemaVersion: bundle.SchemaVersion,
		Source:        bundle.Source{Adapter: "litellm", AdapterVersion: "1.60.0"},
		GeneratedAt:   time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		Teams: []bundle.TeamRef{{
			ExternalRef: bundle.ExternalRef{System: "litellm", ID: "team/eng"},
			ID:          "team-eng",
			Name:        "Engineering",
		}},
		Users: []bundle.UserRef{{
			ExternalRef: bundle.ExternalRef{System: "litellm", ID: "user/alice"},
			TeamRef:     "team/eng",
			Spec:        server.AdminUser{Username: "alice", Name: "Alice", Email: "alice@example.com", Role: "user", Status: server.StatusActive},
		}},
	}

	if _, err := sink.Apply(context.Background(), migrationBundle); err != nil {
		t.Fatalf("apply: %v", err)
	}
	users, err := client.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	var created server.AdminUser
	for _, user := range users {
		if user.Email == "alice@example.com" {
			created = user
		}
	}
	if created.ID == "" {
		t.Fatal("expected imported user to exist")
	}
	if created.TeamID != "team-eng" {
		t.Fatalf("expected team resolved from TeamRef, got %q", created.TeamID)
	}

	// Second apply with a changed spec must apply the update for real.
	migrationBundle.Users[0].Spec.Name = "Alice Zhang"
	secondSink := NewHTTPSink(client, bundle.StaticResolver{})
	result, err := secondSink.Apply(context.Background(), migrationBundle)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if result.Report.Updated == 0 {
		t.Fatalf("expected user update to be reported, got %+v", result.Report)
	}
	users, err = client.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users after update: %v", err)
	}
	for _, user := range users {
		if user.Email == "alice@example.com" && user.Name != "Alice Zhang" {
			t.Fatalf("expected user name updated on server, got %q", user.Name)
		}
	}
}
