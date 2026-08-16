package dbschema

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// verifyAgainstReference builds the reference snapshot and compares it with
// the actual schema; a non-nil error means adoption must be refused.
func (r *Runner) verifyAgainstReference(ctx context.Context) error {
	reference, err := r.adoptionReference(ctx)
	if err != nil {
		return err
	}
	actual, err := Introspect(ctx, r.db, r.dialect, "")
	if err != nil {
		return err
	}
	if violations := CompareObjects(reference, actual); len(violations) > 0 {
		return errors.New(FormatViolations(violations))
	}
	return nil
}

// lockName keys the runner's own PostgreSQL advisory lock. It is distinct from
// the store's startup advisory lock so nesting the two can never self-deadlock.
const lockName = "tokenhub:dbschema:migrate"

// Adopt bridges a database that has no adoption baseline: it runs the frozen
// adoption callback (if any), then records the baseline row. A database that
// already carries a baseline is only verified, after which pending expand
// migrations run. Adopt never applies contract migrations.
func (r *Runner) Adopt(ctx context.Context, frozen func(ctx context.Context) error) (Result, error) {
	if err := r.ensureLedger(ctx); err != nil {
		return Result{}, err
	}
	applied, err := r.loadApplied(ctx)
	if err != nil {
		return Result{}, err
	}
	if findApplied(applied, BaselineVersion) != nil {
		if err := r.verifyApplied(applied); err != nil {
			return Result{}, err
		}
		return r.Migrate(ctx)
	}
	release, err := r.acquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	// Re-read under the lock: another executor may have adopted concurrently.
	applied, err = r.loadApplied(ctx)
	if err != nil {
		return Result{}, err
	}
	if baseline := findApplied(applied, BaselineVersion); baseline != nil {
		if err := r.verifyApplied(applied); err != nil {
			return Result{}, err
		}
		return r.migrateLocked(ctx)
	}
	// A database without business tables adopts from the frozen baseline SQL
	// instead of the legacy flow (ADR 0005).
	if len(r.freshBaseline) > 0 {
		fresh, freshErr := r.databaseIsFresh(ctx)
		if freshErr != nil {
			return Result{}, freshErr
		}
		if fresh {
			return r.adoptFreshLocked(ctx)
		}
	}
	attemptID, err := r.beginAttempt(ctx, BaselineVersion)
	if err != nil {
		return Result{}, err
	}
	started := time.Now()
	if frozen != nil {
		if err := frozen(ctx); err != nil {
			r.finishAttempt(ctx, attemptID, "failed", time.Since(started), ErrCodeApplyFailed)
			return Result{}, newError(ErrCodeApplyFailed, BaselineVersion, err)
		}
	}
	// The frozen flow completed; before the baseline is recorded the database
	// must semantically match the reference schema (ADR 0005).
	if r.adoptionReference != nil {
		if verifyErr := r.verifyAgainstReference(ctx); verifyErr != nil {
			r.finishAttempt(ctx, attemptID, "failed", time.Since(started), ErrCodeSchemaVerification)
			return Result{}, newError(ErrCodeSchemaVerification, BaselineVersion, verifyErr)
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

// Migrate verifies the ledger and applies pending expand migrations under a
// bounded cross-process lock. It refuses dirty ledgers, unknown applied
// versions, checksum drift, and databases without an adoption baseline.
func (r *Runner) Migrate(ctx context.Context) (Result, error) {
	if err := r.ensureLedger(ctx); err != nil {
		return Result{}, err
	}
	applied, err := r.loadApplied(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := r.verifyApplied(applied); err != nil {
		return Result{}, err
	}
	if findApplied(applied, BaselineVersion) == nil {
		return Result{}, newError(ErrCodeBaselineMissing, BaselineVersion, errNoBaseline)
	}
	if len(r.pendingExpands(applied)) == 0 {
		return Result{}, nil
	}
	release, err := r.acquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	return r.migrateLocked(ctx)
}

// migrateLocked applies pending expand migrations; the caller holds the
// coordination lock (or runs under external coordination).
func (r *Runner) migrateLocked(ctx context.Context) (Result, error) {
	applied, err := r.loadApplied(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := r.verifyApplied(applied); err != nil {
		return Result{}, err
	}
	if findApplied(applied, BaselineVersion) == nil {
		return Result{}, newError(ErrCodeBaselineMissing, BaselineVersion, errNoBaseline)
	}
	pending := r.pendingExpands(applied)
	if len(pending) == 0 {
		return Result{}, nil
	}
	result := Result{}
	for _, m := range pending {
		record, outcome, err := r.applyMigration(ctx, m)
		if err != nil {
			return result, err
		}
		if outcome == "success" {
			result.Applied = append(result.Applied, record)
		}
	}
	return result, nil
}

func (r *Runner) pendingExpands(applied []Applied) []Migration {
	appliedSet := make(map[int64]bool, len(applied))
	for _, row := range applied {
		appliedSet[row.Version] = true
	}
	var pending []Migration
	for _, m := range r.registry {
		if appliedSet[m.Version] || m.Phase != PhaseExpand || !m.appliesTo(r.dialect) {
			continue
		}
		pending = append(pending, m)
	}
	return pending
}

// applyMigration executes one migration and records the attempt. A
// transactional failure rolls the version back; a non-transactional failure
// leaves the dirty marker in the ledger and refuses startup until repair. An
// outcome of "skipped" means another executor completed the version first.
func (r *Runner) applyMigration(ctx context.Context, m Migration) (Applied, string, error) {
	attemptID, err := r.beginAttempt(ctx, m.Version)
	if err != nil {
		return Applied{}, "failed", err
	}
	started := time.Now()
	record, outcome, applyErr := r.runMigration(ctx, m)
	r.finishAttempt(ctx, attemptID, outcome, time.Since(started), errorCode(applyErr))
	if applyErr != nil {
		return Applied{}, outcome, newError(ErrCodeApplyFailed, m.Version, applyErr)
	}
	return record, outcome, nil
}

func runMigrationBody(ctx context.Context, db Execer, m Migration) error {
	if m.Go != nil {
		return m.Go(ctx, db)
	}
	for _, statement := range m.Statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("statement %q: %w", firstLine(statement), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return ErrCodeApplyFailed
}

// runMigration executes the migration on its dialect and reports the ledger
// outcome: success, rolled_back (transactional failure), dirty
// (non-transactional failure, dirty marker stays), or failed (no migration
// content ran).
func (r *Runner) runMigration(ctx context.Context, m Migration) (Applied, string, error) {
	if r.dialect == DialectSQLite {
		return r.runOnSQLite(ctx, m)
	}
	return r.runOnPostgres(ctx, m)
}

// runOnSQLite executes the migration on a dedicated connection. BEGIN
// IMMEDIATE takes the SQLite write lock up front so concurrent executors in
// other processes fail fast instead of mid-transaction.
func (r *Runner) runOnSQLite(ctx context.Context, m Migration) (Applied, string, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return Applied{}, "failed", err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return Applied{}, "failed", err
	}
	open := true
	defer func() {
		if open {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	// BEGIN IMMEDIATE is the SQLite cross-process lock, so the version must be
	// re-checked under it: another executor may have committed while this
	// runner was reading the ledger.
	already, err := r.versionRecorded(ctx, conn, m.Version)
	if err != nil {
		return Applied{}, "failed", err
	}
	if already {
		open = false
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return Applied{}, "failed", err
		}
		return Applied{}, "skipped", nil
	}
	if m.transactional() {
		if err := runMigrationBody(ctx, conn, m); err != nil {
			return Applied{}, "rolled_back", err
		}
		if err := r.insertAppliedOn(ctx, conn, m.Version, m.Name, m.Phase, m.Checksum(), false); err != nil {
			return Applied{}, "rolled_back", err
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return Applied{}, "rolled_back", err
		}
		open = false
		return r.appliedRecord(m), "success", nil
	}
	// Non-transactional: persist the dirty marker in its own committed
	// transaction first, so a crash mid-run leaves a ledger that refuses
	// startup instead of an unknown state.
	if err := r.insertAppliedOn(ctx, conn, m.Version, m.Name, m.Phase, m.Checksum(), true); err != nil {
		return Applied{}, "failed", err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return Applied{}, "failed", err
	}
	open = false
	if err := runMigrationBody(ctx, conn, m); err != nil {
		return Applied{}, "dirty", err
	}
	if err := m.Postcondition(ctx, conn); err != nil {
		return Applied{}, "dirty", fmt.Errorf("postcondition: %w", err)
	}
	if err := r.clearDirty(ctx, m.Version); err != nil {
		return Applied{}, "dirty", err
	}
	return r.appliedRecord(m), "success", nil
}

// runOnPostgres executes the migration on a dedicated session. The caller
// holds the advisory lock, so the extra connection only scopes the
// transaction.
func (r *Runner) runOnPostgres(ctx context.Context, m Migration) (Applied, string, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return Applied{}, "failed", err
	}
	defer conn.Close()
	if m.transactional() {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return Applied{}, "failed", err
		}
		defer func() { _ = tx.Rollback() }()
		if err := runMigrationBody(ctx, tx, m); err != nil {
			return Applied{}, "rolled_back", err
		}
		if err := r.insertAppliedOn(ctx, tx, m.Version, m.Name, m.Phase, m.Checksum(), false); err != nil {
			return Applied{}, "rolled_back", err
		}
		if err := tx.Commit(); err != nil {
			return Applied{}, "rolled_back", err
		}
		return r.appliedRecord(m), "success", nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Applied{}, "failed", err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.insertAppliedOn(ctx, tx, m.Version, m.Name, m.Phase, m.Checksum(), true); err != nil {
		return Applied{}, "failed", err
	}
	if err := tx.Commit(); err != nil {
		return Applied{}, "failed", err
	}
	if err := runMigrationBody(ctx, r.db, m); err != nil {
		return Applied{}, "dirty", err
	}
	if err := m.Postcondition(ctx, r.db); err != nil {
		return Applied{}, "dirty", fmt.Errorf("postcondition: %w", err)
	}
	if err := r.clearDirty(ctx, m.Version); err != nil {
		return Applied{}, "dirty", err
	}
	return r.appliedRecord(m), "success", nil
}

func (r *Runner) appliedRecord(m Migration) Applied {
	return Applied{
		Version:        m.Version,
		Name:           m.Name,
		Phase:          m.Phase,
		Checksum:       m.Checksum(),
		AppliedAt:      r.nowText(),
		AppliedRelease: "",
	}
}

// acquireLock serializes migration executors across processes. PostgreSQL uses
// a session advisory lock with a bounded poll; SQLite writer exclusion comes
// from per-migration BEGIN IMMEDIATE transactions, so the SQLite path is a
// no-op here. Runners created with WithExternalCoordination skip locking
// because the caller already serializes schema work.
func (r *Runner) acquireLock(ctx context.Context) (func(), error) {
	if r.externalCoordination || r.dialect == DialectSQLite {
		return func() {}, nil
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(r.lockWait)
	for {
		var acquired bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", lockName).Scan(&acquired); err != nil {
			conn.Close()
			return nil, err
		}
		if acquired {
			break
		}
		if time.Now().After(deadline) {
			_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1, 0))", lockName)
			conn.Close()
			return nil, newError(ErrCodeLockTimeout, 0, fmt.Errorf("schema migration lock %q still held after %s", lockName, r.lockWait))
		}
		r.log("dbschema: waiting for schema migration lock %q (bounded at %s)", lockName, r.lockWait)
		select {
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1, 0))", lockName)
		conn.Close()
	}, nil
}
