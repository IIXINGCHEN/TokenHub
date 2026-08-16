package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"tokenhub/backend/internal/dbschema"
)

// adoptSchemaLedger records or verifies the dbschema adoption baseline around
// the frozen startup schema flow (bridge-release semantics, ADR 0005). The
// caller already serializes schema work across processes, so the runner runs
// under external coordination. Empty databases adopt from the frozen baseline
// SQL (SQLite and PostgreSQL); databases with business tables run the legacy
// callback and are semantically verified against the reference snapshot
// before the baseline is recorded. Ordinary restarts only verify the ledger
// and checksums.
func adoptSchemaLedger(ctx context.Context, db *sql.DB, driver, dsn string, legacy func(ctx context.Context) error) error {
	reference := func(ctx context.Context) (dbschema.ObjectSet, error) {
		return schemaReferenceSnapshot(ctx, db, driver, dsn)
	}
	options := []dbschema.Option{
		dbschema.WithExternalCoordination(),
		dbschema.WithLogger(log.Printf),
		dbschema.WithAdoptionReference(reference),
	}
	var statements []string
	var err error
	switch dbschema.Dialect(driver) {
	case dbschema.DialectSQLite:
		statements, err = dbschema.SQLiteBaselineStatements()
	case dbschema.DialectPostgres:
		statements, err = dbschema.PostgresBaselineStatements()
	default:
		err = fmt.Errorf("unsupported schema ledger driver %q", driver)
	}
	if err != nil {
		return err
	}
	if len(statements) > 0 {
		options = append(options, dbschema.WithFreshBaseline(statements))
	}
	runner, err := dbschema.NewRunner(db, dbschema.Dialect(driver), nil, options...)
	if err != nil {
		return err
	}
	_, err = runner.Adopt(ctx, legacy)
	return err
}

// schemaReferenceCache holds one reference snapshot per driver per process:
// the snapshot depends only on the compiled model set, not the target
// database.
var schemaReferenceCache sync.Map // driver string -> dbschema.ObjectSet

// schemaReferenceSnapshot returns the semantic reference schema for the
// driver, building it once by running the frozen structural flow on a
// throwaway database and introspecting the result (ADR 0006).
func schemaReferenceSnapshot(ctx context.Context, execDB *sql.DB, driver, dsn string) (dbschema.ObjectSet, error) {
	if cached, ok := schemaReferenceCache.Load(driver); ok {
		return cached.(dbschema.ObjectSet), nil
	}
	built, err := buildSchemaReference(ctx, execDB, driver, dsn)
	if err != nil {
		return dbschema.ObjectSet{}, err
	}
	stored, _ := schemaReferenceCache.LoadOrStore(driver, built)
	return stored.(dbschema.ObjectSet), nil
}

func buildSchemaReference(ctx context.Context, execDB *sql.DB, driver, dsn string) (dbschema.ObjectSet, error) {
	switch driver {
	case "sqlite":
		scratchDSN := fmt.Sprintf("file:%s?mode=memory&cache=shared", NewID("schemaref"))
		db, err := gorm.Open(sqlite.Open(scratchDSN), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			return dbschema.ObjectSet{}, fmt.Errorf("open sqlite schema reference: %w", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return dbschema.ObjectSet{}, err
		}
		defer sqlDB.Close()
		if err := migrateSchemaObjects(db, driver); err != nil {
			return dbschema.ObjectSet{}, fmt.Errorf("build sqlite schema reference: %w", err)
		}
		return dbschema.Introspect(ctx, sqlDB, dbschema.DialectSQLite, "")
	case "postgres":
		// PostgreSQL folds unquoted identifiers to lowercase; keep the schema
		// name lowercase so the search_path runtime parameter resolves it.
		schemaName := "tokenhub_schema_ref_" + strings.ToLower(NewID(""))
		if _, err := execDB.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", quotePostgresIdent(schemaName))); err != nil {
			return dbschema.ObjectSet{}, fmt.Errorf("create schema reference schema: %w", err)
		}
		defer func() {
			_, _ = execDB.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quotePostgresIdent(schemaName)))
		}()
		scratchDSN, err := postgresSearchPathDSN(dsn, schemaName)
		if err != nil {
			return dbschema.ObjectSet{}, err
		}
		db, err := gorm.Open(postgres.Open(scratchDSN), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			return dbschema.ObjectSet{}, fmt.Errorf("open postgres schema reference: %w", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return dbschema.ObjectSet{}, err
		}
		defer sqlDB.Close()
		if err := migrateSchemaObjects(db, driver); err != nil {
			return dbschema.ObjectSet{}, fmt.Errorf("build postgres schema reference: %w", err)
		}
		return dbschema.Introspect(ctx, sqlDB, dbschema.DialectPostgres, schemaName)
	default:
		return dbschema.ObjectSet{}, fmt.Errorf("unsupported schema reference driver %q", driver)
	}
}

func quotePostgresIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// postgresSearchPathDSN points a PostgreSQL DSN at the given schema. URL-style
// DSNs carry it as a query parameter; keyword DSNs append it as a runtime
// parameter, which the pgx parser accepts.
func postgresSearchPathDSN(dsn, schema string) (string, error) {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	return dsn + " search_path=" + schema, nil
}
