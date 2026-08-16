package server

import (
	"context"
	"net/http"

	"tokenhub/backend/internal/dbschema"
)

// SchemaLedgerStatus exposes the migration ledger view for read-only status
// surfaces.
func (s *GormStore) SchemaLedgerStatus(ctx context.Context) (dbschema.Status, error) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return dbschema.Status{}, err
	}
	runner, err := dbschema.NewRunner(sqlDB, dbschema.Dialect(s.dbDriver), SchemaMigrationRegistry())
	if err != nil {
		return dbschema.Status{}, err
	}
	return runner.Status(ctx)
}

// DataBackfillStates exposes the data backfill ledger for read-only status
// surfaces.
func (s *GormStore) DataBackfillStates(ctx context.Context) ([]dbschema.BackfillState, error) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, err
	}
	executor, err := dbschema.NewBackfillExecutor(sqlDB, dbschema.Dialect(s.dbDriver), nil)
	if err != nil {
		return nil, err
	}
	return executor.Status(ctx)
}

// handleAdminSchemaStatus serves the read-only database evolution status for
// the admin console: ledger state, readiness reason, pending migrations,
// data-backfill progress, live instances, and this release's compatibility
// manifest (ADR 0005: the Admin API only ever shows status; contract and
// repair stay on the CLI).
func (s *Server) handleAdminSchemaStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r, "system", r.Method); !ok {
		return
	}
	payload := map[string]any{
		"compatibility":              CurrentCompatibilityManifest(),
		"ready":                      true,
		"schema_version":             int64(0),
		"reason":                     "",
		"pending_expand":             []dbschema.Migration{},
		"pending_contract":           []dbschema.Migration{},
		"blocking_backfills_pending": []string{},
		"backfills":                  []dbschema.BackfillState{},
		"instances":                  []InstanceHeartbeat{},
	}
	if gormStore, ok := s.store.(*GormStore); ok {
		ctx := r.Context()
		state := gormStore.DatabaseEvolutionStatus(ctx)
		payload["ready"] = state.Ready
		payload["reason"] = state.Reason
		payload["schema_version"] = state.SchemaVersion
		payload["blocking_backfills_pending"] = state.BlockingBackfillsPending
		if ledger, err := gormStore.SchemaLedgerStatus(ctx); err == nil {
			payload["schema_version"] = ledger.CurrentVersion
			payload["pending_expand"] = ledger.PendingExpand
			payload["pending_contract"] = ledger.PendingContract
		}
		if backfills, err := gormStore.DataBackfillStates(ctx); err == nil {
			payload["backfills"] = backfills
		}
		if heartbeats, err := gormStore.ListInstanceHeartbeats(ctx); err == nil {
			payload["instances"] = heartbeats
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

// annotateRollbackCompatibility marks each rollback candidate with its
// database compatibility verdict so the admin UI can disable unknown or
// incompatible targets (ADR 0005).
func (s *Server) annotateRollbackCompatibility(ctx context.Context, versions []rollbackVersionInfo) {
	for i := range versions {
		verdict := s.rollbackCompatibility(ctx, versions[i].Version)
		versions[i].Compatibility = verdict.Compatibility
		versions[i].CompatibilityReason = verdict.Reason
	}
}
