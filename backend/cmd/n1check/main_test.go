package main

import (
	"testing"

	"tokenhub/backend/internal/dbschema"
)

// TestFixturesParseAndSelfConsistent guards the committed immutable N-1
// legacy fixtures: they must parse as ObjectSet and be internally consistent
// (comparing the fixture against itself yields no violations). The fixtures
// themselves are regenerated with the real v0.4.0 release binary:
//
//	cd backend && go run ./cmd/n1check -dump internal/dbschema/fixtures/n1-legacy-v040-<dialect>.json <v0.4.0-db-url>
func TestFixturesParseAndSelfConsistent(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		fixture, err := loadFixture(driver)
		if err != nil {
			t.Fatalf("load %s fixture: %v", driver, err)
		}
		if len(fixture.Tables) == 0 {
			t.Fatalf("%s fixture holds no tables", driver)
		}
		foundRequestLogs := false
		for _, table := range fixture.Tables {
			if table.Name == "request_logs" {
				foundRequestLogs = true
			}
			if len(table.Columns) == 0 {
				t.Fatalf("%s fixture table %q has no columns", driver, table.Name)
			}
		}
		if !foundRequestLogs {
			t.Fatalf("%s fixture is missing the request_logs table", driver)
		}
		if violations := dbschema.CompareObjects(fixture, fixture); len(violations) > 0 {
			t.Fatalf("%s fixture is not self-consistent: %s", driver, dbschema.FormatViolations(violations))
		}
	}
}
