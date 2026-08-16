package dbschema

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"
)

//go:embed migrations
var migrationFS embed.FS

const sqliteBaselinePath = "migrations/sqlite/000001_baseline.json"
const postgresBaselinePath = "migrations/postgres/000001_baseline.json"

type baselineFile struct {
	Version    int64    `json:"version"`
	Dialect    string   `json:"dialect"`
	Statements []string `json:"statements"`
}

// SQLiteBaselineStatements returns the frozen SQL statements that create the
// adoption-baseline schema on a fresh SQLite database (ADR 0005: fresh
// databases are created from explicit SQL; only databases that already carry
// business tables run the frozen legacy-adoption flow). Regenerate the file
// with UPDATE_BASELINE=1 go test ./internal/server -run TestSQLiteBaselineSQLIsCurrent.
func SQLiteBaselineStatements() ([]string, error) {
	return readBaselineFile(sqliteBaselinePath, DialectSQLite)
}

// PostgresBaselineStatements returns the frozen SQL statements that create
// the adoption-baseline schema on a fresh PostgreSQL database. Regenerate the
// file with UPDATE_BASELINE=1 and TEST_POSTGRES_URL set while running
// ./internal/server -run TestPostgresBaselineSQLIsCurrent (integration tag).
func PostgresBaselineStatements() ([]string, error) {
	return readBaselineFile(postgresBaselinePath, DialectPostgres)
}

func readBaselineFile(path string, dialect Dialect) ([]string, error) {
	raw, err := migrationFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dbschema: read %s baseline: %w", dialect, err)
	}
	var file baselineFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("dbschema: parse %s baseline: %w", dialect, err)
	}
	if file.Version != BaselineVersion || file.Dialect != string(dialect) {
		return nil, fmt.Errorf("dbschema: %s baseline declares version %d dialect %q", dialect, file.Version, file.Dialect)
	}
	return file.Statements, nil
}

// databaseIsFresh reports whether the database holds no user tables beyond
// the runner's own ledger tables.
func (r *Runner) databaseIsFresh(ctx context.Context) (bool, error) {
	query := "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	if r.dialect == DialectPostgres {
		query = "SELECT table_name FROM information_schema.tables WHERE table_type = 'BASE TABLE' AND table_schema = current_schema()"
	}
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return false, fmt.Errorf("dbschema: list tables for freshness check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if !isLedgerTable(name) {
			return false, nil
		}
	}
	return true, rows.Err()
}

func isLedgerTable(name string) bool {
	return name == "schema_migrations" || name == "migration_attempts"
}

// adoptFreshLocked creates the schema from the frozen baseline SQL on an empty
// database, semantically verifies it against the reference snapshot, and only
// then records the baseline. The caller holds the coordination lock.
func (r *Runner) adoptFreshLocked(ctx context.Context) (Result, error) {
	attemptID, err := r.beginAttempt(ctx, BaselineVersion)
	if err != nil {
		return Result{}, err
	}
	started := time.Now()
	if err := r.execStatements(ctx, r.freshBaseline); err != nil {
		r.finishAttempt(ctx, attemptID, "failed", time.Since(started), ErrCodeApplyFailed)
		return Result{}, newError(ErrCodeApplyFailed, BaselineVersion, err)
	}
	if r.adoptionReference != nil {
		if err := r.verifyAgainstReference(ctx); err != nil {
			r.finishAttempt(ctx, attemptID, "failed", time.Since(started), ErrCodeSchemaVerification)
			return Result{}, newError(ErrCodeSchemaVerification, BaselineVersion, err)
		}
	}
	record, err := r.insertAppliedTx(ctx, BaselineVersion, baselineName, PhaseExpand, AdoptionChecksum, false)
	if err != nil {
		r.finishAttempt(ctx, attemptID, "failed", time.Since(started), ErrCodeApplyFailed)
		return Result{}, newError(ErrCodeApplyFailed, BaselineVersion, err)
	}
	r.finishAttempt(ctx, attemptID, "success", time.Since(started), "")
	result := Result{Adopted: true, Applied: []Applied{record}}
	migrated, err := r.migrateLocked(ctx)
	if err != nil {
		return result, err
	}
	result.Applied = append(result.Applied, migrated.Applied...)
	return result, nil
}

// execStatements runs the frozen baseline statements. SQLite runs them in one
// transaction; PostgreSQL runs each statement standalone because the baseline
// includes CREATE INDEX CONCURRENTLY, which cannot run inside a transaction.
// A failed fresh PostgreSQL adoption self-heals: the half-built database no
// longer counts as fresh, so the next start completes it through the frozen
// legacy flow before recording the baseline.
func (r *Runner) execStatements(ctx context.Context, statements []string) error {
	if r.dialect == DialectPostgres {
		for _, statement := range statements {
			if _, err := r.db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("baseline statement %q: %w", firstLine(statement), err)
			}
		}
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("baseline statement %q: %w", firstLine(statement), err)
		}
	}
	return tx.Commit()
}
