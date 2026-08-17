// Command n1check verifies that a live database created by the N-1 (previous)
// release matches the committed immutable legacy schema fixture (ADR 0005:
// CI maintains the immutable N-1 schema fixture). It is dialect-neutral: the
// same semantic fixture pins the SQLite and PostgreSQL legacy shapes.
//
// Usage:
//
//	n1check <database-url>             verify against the committed fixture
//	n1check -dump <out.json> <url>     write the introspected shape as a fixture
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"tokenhub/backend/internal/dbschema"
	"tokenhub/backend/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "n1check:", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	dumpPath := ""
	if len(args) >= 2 && args[0] == "-dump" {
		dumpPath = args[1]
		args = args[2:]
	}
	databaseURL := ""
	if len(args) > 0 {
		databaseURL = args[0]
	}
	if databaseURL == "" {
		databaseURL = os.Getenv("TOKENHUB_DATABASE_URL")
	}
	if databaseURL == "" {
		return fmt.Errorf("usage: n1check [-dump <out.json>] <database-url>")
	}
	driver, db, err := server.OpenRawDatabase(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	actual, err := dbschema.Introspect(context.Background(), db, dbschema.Dialect(driver), "")
	if err != nil {
		return err
	}
	if dumpPath != "" {
		raw, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		return os.WriteFile(dumpPath, raw, 0o644)
	}
	fixture, err := loadFixture(driver)
	if err != nil {
		return err
	}
	if violations := dbschema.CompareObjects(fixture, actual); len(violations) > 0 {
		return fmt.Errorf("legacy database does not match the immutable N-1 schema fixture:\n%s", dbschema.FormatViolations(violations))
	}
	fmt.Fprintf(os.Stdout, "N-1 legacy schema fixture verified (%d tables, driver=%s)\n", len(actual.Tables), driver)
	return nil
}

// fixturePath resolves the committed N-1 legacy fixture for the driver from
// this file's location so the tool works regardless of the working directory.
// The semantic shape is the same across dialects but column type strings are
// not, so each dialect pins its own immutable fixture.
func fixturePath(driver string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "dbschema", "fixtures")
	if driver == "postgres" {
		return filepath.Join(base, "n1-legacy-v040-postgres.json")
	}
	return filepath.Join(base, "n1-legacy-v040-sqlite.json")
}

func loadFixture(driver string) (dbschema.ObjectSet, error) {
	raw, err := os.ReadFile(fixturePath(driver))
	if err != nil {
		return dbschema.ObjectSet{}, fmt.Errorf("read N-1 legacy fixture: %w", err)
	}
	var fixture dbschema.ObjectSet
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return dbschema.ObjectSet{}, fmt.Errorf("parse N-1 legacy fixture: %w", err)
	}
	return fixture, nil
}
