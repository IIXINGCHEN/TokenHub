package dbschema

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStatementBudgetRejectsOversizedSQLMigration(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runner := mustRunner(t, db, nil)
	if _, err := runner.Adopt(ctx, nil); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	runner = mustRunner(t, db, []Migration{
		{
			Version:         2,
			Name:            "oversized",
			Statements:      []string{"CREATE TABLE budget_a (id INTEGER)", "CREATE TABLE budget_b (id INTEGER)"},
			StatementBudget: 1,
		},
	})
	_, err := runner.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "statement budget exceeded") {
		t.Fatalf("expected statement budget failure, got %v", err)
	}
	requireErrorCode(t, err, ErrCodeApplyFailed)
	if tableExists(t, db, "budget_a") {
		t.Fatal("transactional budget failure must roll the statements back")
	}
	var recorded int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&recorded); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if recorded != 0 {
		t.Fatal("oversized migration must not be recorded as applied")
	}
}

func TestStatementBudgetBoundsGoMigrationStatements(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runner := mustRunner(t, db, nil)
	if _, err := runner.Adopt(ctx, nil); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	runner = mustRunner(t, db, []Migration{
		{
			Version: 2,
			Name:    "go overspending",
			Go: func(ctx context.Context, db Execer) error {
				for i := 0; i < 3; i++ {
					if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
						return err
					}
				}
				return nil
			},
			StatementBudget:  2,
			ChecksumOverride: "test-go-budget-checksum",
		},
	})
	_, err := runner.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "statement budget exceeded") {
		t.Fatalf("expected statement budget failure, got %v", err)
	}
	requireErrorCode(t, err, ErrCodeApplyFailed)
	var recorded int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&recorded); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if recorded != 0 {
		t.Fatal("budget-exceeding Go migration must not be recorded as applied")
	}
}

func TestLockBudgetDeadlineFailsMigration(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	runner := mustRunner(t, db, nil)
	if _, err := runner.Adopt(ctx, nil); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	runner = mustRunner(t, db, []Migration{
		{
			Version:            2,
			Name:               "slow",
			LockTimeoutSeconds: 1,
			Go: func(ctx context.Context, db Execer) error {
				select {
				case <-time.After(1500 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			ChecksumOverride: "test-lock-budget-checksum",
		},
	})
	started := time.Now()
	_, err := runner.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "lock budget") {
		t.Fatalf("expected lock budget failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("migration outlived its lock budget: %s", elapsed)
	}
	if outcome := lastAttemptOutcome(t, db, 2); outcome != "rolled_back" {
		t.Fatalf("expected rolled_back attempt outcome, got %s", outcome)
	}
	var recorded int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&recorded); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if recorded != 0 {
		t.Fatal("deadline-exceeded migration must not be recorded as applied")
	}
}
