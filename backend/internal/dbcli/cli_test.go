package dbcli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"tokenhub/backend/internal/server"
)

func cliTestEnv(t *testing.T) string {
	t.Helper()
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "cli.db")
	t.Setenv("TOKENHUB_DATABASE_URL", databaseURL)
	return databaseURL
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	cliTestEnv(t)
	code, output := runCLI(t, "bogus")
	if code != 2 || !strings.Contains(output, "unknown db command") {
		t.Fatalf("expected exit 2 with unknown-command message, got %d %q", code, output)
	}
}

func TestStatusAndMigrateOnUnadoptedDatabase(t *testing.T) {
	cliTestEnv(t)
	code, output := runCLI(t, "status")
	if code != 0 || !strings.Contains(output, "baseline recorded:   false") {
		t.Fatalf("status on empty database: code=%d output=%q", code, output)
	}
	code, output = runCLI(t, "migrate")
	if code != 1 || !strings.Contains(output, "adoption baseline") {
		t.Fatalf("migrate before adoption must point at server startup, got %d %q", code, output)
	}
}

func TestCommandsOnAdoptedDatabase(t *testing.T) {
	databaseURL := cliTestEnv(t)
	// Opening the store once adopts the database and records the baseline.
	// The file handle is released by the test process; nothing reopens it.
	if _, err := server.NewSQLiteStore(databaseURL); err != nil {
		t.Fatalf("open store: %v", err)
	}

	code, output := runCLI(t, "status")
	if code != 0 || !strings.Contains(output, "baseline recorded:   true") || !strings.Contains(output, "current version:     1") {
		t.Fatalf("status on adopted database: code=%d output=%q", code, output)
	}
	code, output = runCLI(t, "verify")
	if code != 0 || !strings.Contains(output, "ledger: verified") || !strings.Contains(output, "schema: verified") {
		t.Fatalf("verify on adopted database: code=%d output=%q", code, output)
	}
	code, output = runCLI(t, "migrate")
	if code != 0 || !strings.Contains(output, "nothing to migrate") {
		t.Fatalf("migrate on adopted database: code=%d output=%q", code, output)
	}
}

func TestRepairRequiresDirtyVersion(t *testing.T) {
	databaseURL := cliTestEnv(t)
	if _, err := server.NewSQLiteStore(databaseURL); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, "repair", "--version", "0")
	if code != 1 || !strings.Contains(output, "requires --version") {
		t.Fatalf("repair without version: code=%d output=%q", code, output)
	}
	code, output = runCLI(t, "repair", "--version", "1")
	if code != 1 || !strings.Contains(output, "not_dirty") {
		t.Fatalf("repair on clean version: code=%d output=%q", code, output)
	}
}

func TestContractDryRunOnAdoptedDatabase(t *testing.T) {
	databaseURL := cliTestEnv(t)
	if _, err := server.NewSQLiteStore(databaseURL); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, "contract", "--dry-run")
	if code != 0 || !strings.Contains(output, "pending contract migrations: 0") || !strings.Contains(output, "dry run") {
		t.Fatalf("contract dry run: code=%d output=%q", code, output)
	}
	// Executing without operator evidence is refused even with nothing pending.
	code, output = runCLI(t, "contract")
	if code != 1 || !strings.Contains(output, "--backup-reference") {
		t.Fatalf("contract without evidence: code=%d output=%q", code, output)
	}
}
