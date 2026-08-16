// Package dbcli implements the `tokenhub db` maintenance commands. The
// commands expose the migration ledger, semantic verification, pending expand
// migrations, dirty-migration repair, and contract execution with the
// operator-verified preconditions (ADR 0005). They never run the server.
package dbcli

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"tokenhub/backend/internal/dbschema"
	"tokenhub/backend/internal/server"
)

// heartbeatTTL matches the instance heartbeat published by the server; live
// heartbeats newer than this window block contract execution.
const heartbeatTTL = 90 * time.Second

// AppRelease stamps the executing release into ledger rows; the binary sets
// it from its build version.
var AppRelease string

// session bundles the handles one command needs.
type session struct {
	driver string
	db     *sql.DB
	runner *dbschema.Runner
	close  func()
}

func openSession(databaseURL, appRelease string) (*session, error) {
	driver, db, err := server.OpenRawDatabase(databaseURL)
	if err != nil {
		return nil, err
	}
	runner, err := dbschema.NewRunner(db, dbschema.Dialect(driver), server.SchemaMigrationRegistry(),
		dbschema.WithAppRelease(AppRelease))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &session{driver: driver, db: db, runner: runner, close: func() { _ = db.Close() }}, nil
}

// Run executes one `tokenhub db <command>` invocation and returns the process
// exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	config := server.ConfigFromEnv()
	var err error
	switch args[0] {
	case "status":
		err = runStatus(ctx, config, stdout)
	case "verify":
		err = runVerify(ctx, config, stdout)
	case "migrate":
		err = runMigrate(ctx, config, stdout)
	case "repair":
		err = runRepair(ctx, args[1:], config, stdout)
	case "contract":
		err = runContract(ctx, args[1:], config, stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown db command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "tokenhub db %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func runStatus(ctx context.Context, config server.Config, stdout io.Writer) error {
	s, err := openSession(config.DatabaseURL, config.AppVersion)
	if err != nil {
		return err
	}
	defer s.close()
	status, err := s.runner.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "driver:              %s\n", s.driver)
	fmt.Fprintf(stdout, "baseline recorded:   %t\n", status.BaselineRecorded)
	fmt.Fprintf(stdout, "current version:     %d\n", status.CurrentVersion)
	fmt.Fprintf(stdout, "dirty:               %t", status.Dirty)
	if status.Dirty {
		fmt.Fprintf(stdout, " (version %d; repair required)", status.DirtyVersion)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "pending expand:      %d\n", len(status.PendingExpand))
	for _, m := range status.PendingExpand {
		fmt.Fprintf(stdout, "  %d %s\n", m.Version, m.Name)
	}
	fmt.Fprintf(stdout, "pending contract:    %d\n", len(status.PendingContract))
	for _, m := range status.PendingContract {
		fmt.Fprintf(stdout, "  %d %s\n", m.Version, m.Name)
	}
	if err := printBackfillStatus(ctx, s, stdout); err != nil {
		return err
	}
	return printHeartbeatStatus(ctx, s.db, stdout)
}

func printBackfillStatus(ctx context.Context, s *session, stdout io.Writer) error {
	executor, err := dbschema.NewBackfillExecutor(s.db, dbschema.Dialect(s.driver), nil)
	if err != nil {
		return err
	}
	states, err := executor.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "data backfills:      %d\n", len(states))
	for _, state := range states {
		fmt.Fprintf(stdout, "  %s [%s] state=%s remaining=%d\n", state.ID, state.Mode, state.State, state.Remaining)
	}
	return nil
}

func printHeartbeatStatus(ctx context.Context, db *sql.DB, stdout io.Writer) error {
	rows, err := db.QueryContext(ctx,
		"SELECT instance_id, release, last_seen FROM instance_heartbeats ORDER BY instance_id")
	if err != nil {
		// The heartbeat table ships with the managed-upgrade rollout; treat
		// its absence as no live instances.
		fmt.Fprintln(stdout, "live instances:      0")
		return nil
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var instanceID, release, lastSeen string
		if err := rows.Scan(&instanceID, &release, &lastSeen); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "  %s release=%s last_seen=%s\n", instanceID, release, lastSeen)
		count++
	}
	fmt.Fprintf(stdout, "live instances:      %d\n", count)
	return rows.Err()
}

func runVerify(ctx context.Context, config server.Config, stdout io.Writer) error {
	s, err := openSession(config.DatabaseURL, config.AppVersion)
	if err != nil {
		return err
	}
	if err := s.runner.Verify(ctx); err != nil {
		s.close()
		return err
	}
	fmt.Fprintln(stdout, "ledger: verified (checksums, versions, dirty state)")
	s.close()
	if err := server.VerifySchemaSemantics(ctx, config.DatabaseURL); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "schema: verified against the frozen reference snapshot")
	return nil
}

func runMigrate(ctx context.Context, config server.Config, stdout io.Writer) error {
	s, err := openSession(config.DatabaseURL, config.AppVersion)
	if err != nil {
		return err
	}
	defer s.close()
	result, err := s.runner.Migrate(ctx)
	if err != nil {
		var schemaErr *dbschema.Error
		if errors.As(err, &schemaErr) && schemaErr.Code == dbschema.ErrCodeBaselineMissing {
			return errors.New("database has no adoption baseline; start the server once to adopt it, then retry")
		}
		return err
	}
	if len(result.Applied) == 0 {
		fmt.Fprintln(stdout, "nothing to migrate")
		return nil
	}
	for _, record := range result.Applied {
		fmt.Fprintf(stdout, "applied %d %s\n", record.Version, record.Name)
	}
	return nil
}

func runRepair(ctx context.Context, args []string, config server.Config, stdout io.Writer) error {
	flags := flag.NewFlagSet("repair", flag.ContinueOnError)
	versionFlag := flags.Int64("version", 0, "dirty migration version to repair")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *versionFlag <= 0 {
		return errors.New("repair requires --version <n>")
	}
	s, err := openSession(config.DatabaseURL, config.AppVersion)
	if err != nil {
		return err
	}
	defer s.close()
	outcome, err := s.runner.Repair(ctx, *versionFlag)
	if err != nil {
		return err
	}
	switch outcome {
	case dbschema.RepairVerifiedComplete:
		fmt.Fprintf(stdout, "version %d: target state verified, dirty marker cleared\n", *versionFlag)
	case dbschema.RepairRetried:
		fmt.Fprintf(stdout, "version %d: dirty row dropped and migration re-applied\n", *versionFlag)
	}
	return nil
}

func runContract(ctx context.Context, args []string, config server.Config, stdout io.Writer) error {
	flags := flag.NewFlagSet("contract", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "verify preconditions and report the plan without executing")
	backupReference := flags.String("backup-reference", "", "operator-verified backup reference (required to execute)")
	maintenance := flags.Bool("maintenance", false, "assert drain or maintenance conditions are met (required to execute)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := openSession(config.DatabaseURL, config.AppVersion)
	if err != nil {
		return err
	}
	defer s.close()
	plan, err := s.runner.PlanContract(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "pending contract migrations: %d\n", len(plan.Migrations))
	for _, m := range plan.Migrations {
		fmt.Fprintf(stdout, "  %d %s\n", m.Version, m.Name)
	}
	options := dbschema.ContractOptions{
		DryRun:           *dryRun,
		RequireBackfills: func(ctx context.Context) error { return requireBackfillsComplete(ctx, s) },
		RequireCluster:   func(ctx context.Context) error { return requireNoLiveInstances(ctx, s.db) },
	}
	if !*dryRun {
		if *backupReference == "" {
			return errors.New("executing contract migrations requires --backup-reference <verified backup>")
		}
		if !*maintenance {
			return errors.New("executing contract migrations requires --maintenance to assert drain or maintenance conditions")
		}
		fmt.Fprintf(stdout, "backup reference: %s\n", *backupReference)
	}
	result, err := s.runner.ApplyContract(ctx, options)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintln(stdout, "dry run: preconditions verified, nothing executed")
		return nil
	}
	if len(result.Applied) == 0 {
		fmt.Fprintln(stdout, "nothing to execute")
		return nil
	}
	for _, record := range result.Applied {
		fmt.Fprintf(stdout, "executed %d %s\n", record.Version, record.Name)
	}
	return nil
}

// requireBackfillsComplete refuses contract execution while any data backfill
// is unfinished.
func requireBackfillsComplete(ctx context.Context, s *session) error {
	executor, err := dbschema.NewBackfillExecutor(s.db, dbschema.Dialect(s.driver), nil)
	if err != nil {
		return err
	}
	states, err := executor.Status(ctx)
	if err != nil {
		return err
	}
	for _, state := range states {
		if state.State != dbschema.BackfillStateComplete {
			return fmt.Errorf("data backfill %q is %s", state.ID, state.State)
		}
	}
	return nil
}

// requireNoLiveInstances refuses contract execution while any instance
// publishes a fresh heartbeat. Compatibility-range comparison arrives with
// the release manifest; until then any live instance counts as potentially
// incompatible, the conservative reading of ADR 0005.
func requireNoLiveInstances(ctx context.Context, db *sql.DB) error {
	var count int
	cutoff := time.Now().UTC().Add(-heartbeatTTL).Format(time.RFC3339)
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM instance_heartbeats WHERE last_seen > ?", cutoff).Scan(&count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || missingHeartbeatTable(err) {
			return nil
		}
		return err
	}
	if count > 0 {
		return fmt.Errorf("%d live instance(s) with fresh heartbeats; drain them before contracting", count)
	}
	return nil
}

func missingHeartbeatTable(err error) bool {
	message := err.Error()
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: tokenhub db <command> [flags]

commands:
  status                      show ledger, backfill, and instance state
  verify                      verify ledger checksums and semantic schema
  migrate                     apply pending expand migrations
  repair --version <n>        clear a dirty migration via verified repair
  contract [--dry-run]
            [--backup-reference <ref>]
            [--maintenance]   execute contract migrations with preflight

database: resolved from TOKENHUB_DATABASE_URL (or the default SQLite path)`)
}
