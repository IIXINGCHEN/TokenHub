//go:build integration

package dbschema

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openPostgresTestDB opens a PostgreSQL handle in a dedicated throwaway schema
// so repeated runs never collide.
func openPostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()
	pgURL := strings.TrimSpace(os.Getenv("TEST_POSTGRES_URL"))
	if pgURL == "" {
		t.Skip("TEST_POSTGRES_URL not set, skipping PostgreSQL integration test")
	}
	gormDB, err := gorm.Open(postgres.Open(pgURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	schema := fmt.Sprintf("dbschema_it_%d", time.Now().UnixNano())
	if err := gormDB.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = gormDB.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		sqlDB, _ := gormDB.DB()
		_ = sqlDB.Close()
	})
	parsed, err := url.Parse(pgURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	raw, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open postgres handle: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func TestPostgresAdoptMigrateAndVerify(t *testing.T) {
	db := openPostgresTestDB(t)
	ctx := context.Background()
	registry := []Migration{
		{Version: 2, Name: "create-demo", Statements: []string{"CREATE TABLE demo (id BIGINT PRIMARY KEY)"}},
	}
	adoptRunner, err := NewRunner(db, DialectPostgres, nil, WithAppRelease("test-release"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := adoptRunner.Adopt(ctx, nil)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !result.Adopted {
		t.Fatal("expected adoption on fresh postgres schema")
	}
	migrateRunner, err := NewRunner(db, DialectPostgres, registry, WithAppRelease("test-release"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = migrateRunner.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(result.Applied) != 1 || result.Applied[0].Version != 2 {
		t.Fatalf("expected version 2 applied, got %+v", result.Applied)
	}
	var appliedRelease string
	if err := db.QueryRowContext(ctx, "SELECT applied_release FROM schema_migrations WHERE version = 2").Scan(&appliedRelease); err != nil {
		t.Fatal(err)
	}
	if appliedRelease != "test-release" {
		t.Fatalf("expected applied_release test-release, got %q", appliedRelease)
	}
	// A reopened runner only verifies and finds nothing pending.
	status, err := migrateRunner.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.PendingExpand) != 0 || status.Dirty {
		t.Fatalf("expected clean fully migrated status, got %+v", status)
	}
}
