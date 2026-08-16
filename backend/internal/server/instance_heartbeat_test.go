package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tokenhub/backend/internal/dbschema"
)

func TestInstanceHeartbeatLifecycle(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "heartbeat.db")
	store, err := NewSQLiteStore("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	stop := store.StartInstanceHeartbeat("v0.5.0-heartbeat-test")
	heartbeats, err := store.ListInstanceHeartbeats(context.Background())
	if err != nil {
		t.Fatalf("list heartbeats: %v", err)
	}
	if len(heartbeats) != 1 || heartbeats[0].Release != "v0.5.0-heartbeat-test" || heartbeats[0].LastSeen == "" {
		t.Fatalf("unexpected heartbeat rows: %+v", heartbeats)
	}
	stop()
	heartbeats, err = store.ListInstanceHeartbeats(context.Background())
	if err != nil {
		t.Fatalf("list heartbeats after stop: %v", err)
	}
	if len(heartbeats) != 0 {
		t.Fatalf("stopped instance must remove its row, got %+v", heartbeats)
	}
	// Stopping twice must not panic.
	stop()
}

func TestReadinessGatesOnEvolutionState(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ready.db")
	store, err := NewSQLiteStore("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(store)

	request := func() *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return recorder
	}
	if recorder := request(); recorder.Code != http.StatusOK {
		t.Fatalf("expected ready on adopted database, got %d %s", recorder.Code, recorder.Body.String())
	}
	status := store.DatabaseEvolutionStatus(context.Background())
	if !status.Ready || status.SchemaVersion != dbschema.BaselineVersion {
		t.Fatalf("unexpected evolution status: %+v", status)
	}

	// A tampered ledger must pull the instance out of rotation.
	if err := store.db.Exec("UPDATE schema_migrations SET checksum = 'tampered' WHERE version = ?", dbschema.BaselineVersion).Error; err != nil {
		t.Fatal(err)
	}
	recorder := request()
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable on tampered ledger, got %d %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "verification failed") {
		t.Fatalf("expected a reason in the unready body, got %s", body)
	}
	status = store.DatabaseEvolutionStatus(context.Background())
	if status.Ready || !strings.Contains(status.Reason, "verification failed") {
		t.Fatalf("unexpected evolution status after tamper: %+v", status)
	}
}
