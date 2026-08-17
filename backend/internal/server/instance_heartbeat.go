package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"tokenhub/backend/internal/dbschema"
)

const (
	// InstanceHeartbeatInterval is how often a running instance refreshes its
	// heartbeat row.
	InstanceHeartbeatInterval = 30 * time.Second
	// InstanceHeartbeatTTL is how long a heartbeat row counts as live; the db
	// CLI's cluster preflight uses the same window.
	InstanceHeartbeatTTL = 90 * time.Second
)

// InstanceHeartbeat is one published instance row (ADR 0005: every running
// instance publishes its release through a TTL'd database heartbeat).
type InstanceHeartbeat struct {
	InstanceID string `json:"instance_id"`
	Release    string `json:"release"`
	StartedAt  string `json:"started_at"`
	LastSeen   string `json:"last_seen"`
}

// Heartbeat publication states: off before Start, ok while rows publish,
// failing when refreshes keep failing. A serving instance that stops
// publishing must pull itself out of readiness, otherwise contract
// maintenance would see zero live instances.
const (
	heartbeatOff     = int32(0)
	heartbeatOK      = int32(1)
	heartbeatFailing = int32(2)
)

// StartInstanceHeartbeat publishes this instance and refreshes it until the
// returned stop function removes the row. A non-fatal start keeps the server
// running without a heartbeat: losing the heartbeat only delays contract
// maintenance, it never blocks serving.
func (s *GormStore) StartInstanceHeartbeat(release string) (stop func()) {
	instanceID := NewID("instance")
	if err := s.db.Exec(`CREATE TABLE IF NOT EXISTS instance_heartbeats (
		instance_id TEXT PRIMARY KEY,
		release TEXT NOT NULL,
		started_at TEXT NOT NULL,
		last_seen TEXT NOT NULL
	)`).Error; err != nil {
		log.Printf("[tokenhub] failed to create instance heartbeat table: %v", err)
		return func() {}
	}
	refresh := func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		return s.db.Exec(`INSERT INTO instance_heartbeats (instance_id, release, started_at, last_seen)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (instance_id) DO UPDATE SET release = excluded.release, last_seen = excluded.last_seen`,
			instanceID, release, now, now).Error
	}
	publish := func() {
		if err := refresh(); err != nil {
			if s.heartbeatState != nil {
				s.heartbeatState.Store(heartbeatFailing)
			}
			log.Printf("[tokenhub] failed to publish instance heartbeat: %v", err)
		} else if s.heartbeatState != nil {
			s.heartbeatState.Store(heartbeatOK)
		}
	}
	publish()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(InstanceHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				publish()
			}
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(done)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.db.WithContext(ctx).
				Exec("DELETE FROM instance_heartbeats WHERE instance_id = ?", instanceID).Error; err != nil {
				log.Printf("[tokenhub] failed to remove instance heartbeat: %v", err)
			}
		})
	}
}

// ListInstanceHeartbeats returns every published heartbeat row.
func (s *GormStore) ListInstanceHeartbeats(ctx context.Context) ([]InstanceHeartbeat, error) {
	rows, err := s.db.WithContext(ctx).
		Raw("SELECT instance_id, release, started_at, last_seen FROM instance_heartbeats ORDER BY instance_id").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var heartbeats []InstanceHeartbeat
	for rows.Next() {
		var heartbeat InstanceHeartbeat
		if err := rows.Scan(&heartbeat.InstanceID, &heartbeat.Release, &heartbeat.StartedAt, &heartbeat.LastSeen); err != nil {
			return nil, err
		}
		heartbeats = append(heartbeats, heartbeat)
	}
	return heartbeats, rows.Err()
}

// DatabaseEvolutionStatus reports whether the database evolution state allows
// serving: the migration ledger verifies (no dirty migration, no checksum or
// unknown-version drift) and every registered blocking data backfill is
// complete. Pending online backfills never affect readiness (ADR 0007).
type DatabaseEvolutionStatus struct {
	Ready                    bool
	Reason                   string
	SchemaVersion            int64
	BlockingBackfillsPending []string
}

func (s *GormStore) DatabaseEvolutionStatus(ctx context.Context) DatabaseEvolutionStatus {
	sqlDB, err := s.db.DB()
	if err != nil {
		return DatabaseEvolutionStatus{Reason: fmt.Sprintf("database handle unavailable: %v", err)}
	}
	runner, err := dbschema.NewRunner(sqlDB, dbschema.Dialect(s.dbDriver), SchemaMigrationRegistry())
	if err != nil {
		return DatabaseEvolutionStatus{Reason: fmt.Sprintf("migration runner: %v", err)}
	}
	status, err := runner.Status(ctx)
	if err != nil {
		return DatabaseEvolutionStatus{Reason: fmt.Sprintf("migration ledger unreadable: %v", err)}
	}
	if !status.BaselineRecorded {
		return DatabaseEvolutionStatus{Reason: "database has no adoption baseline; start the server once to adopt it"}
	}
	if s.heartbeatState != nil && s.heartbeatState.Load() == heartbeatFailing {
		return DatabaseEvolutionStatus{
			Reason:        "instance heartbeat is not publishing; contract maintenance cannot see this serving instance",
			SchemaVersion: status.CurrentVersion,
		}
	}
	if status.Dirty {
		return DatabaseEvolutionStatus{
			Reason:        fmt.Sprintf("dirty schema migration at version %d; repair required", status.DirtyVersion),
			SchemaVersion: status.CurrentVersion,
		}
	}
	if err := runner.Verify(ctx); err != nil {
		return DatabaseEvolutionStatus{
			Reason:        fmt.Sprintf("migration ledger verification failed: %v", err),
			SchemaVersion: status.CurrentVersion,
		}
	}
	if len(status.PendingExpand) > 0 {
		return DatabaseEvolutionStatus{
			Reason:        fmt.Sprintf("%d expand migration(s) pending; run tokenhub db migrate or restart the server", len(status.PendingExpand)),
			SchemaVersion: status.CurrentVersion,
		}
	}
	pending, err := pendingBlockingBackfills(ctx, sqlDB, s.dbDriver)
	if err != nil {
		return DatabaseEvolutionStatus{
			Reason:        fmt.Sprintf("data backfill ledger unreadable: %v", err),
			SchemaVersion: status.CurrentVersion,
		}
	}
	if len(pending) > 0 {
		return DatabaseEvolutionStatus{
			Reason:                   fmt.Sprintf("blocking data backfills incomplete: %v", pending),
			SchemaVersion:            status.CurrentVersion,
			BlockingBackfillsPending: pending,
		}
	}
	return DatabaseEvolutionStatus{Ready: true, SchemaVersion: status.CurrentVersion}
}

// pendingBlockingBackfills returns the IDs of incomplete blocking backfills.
// The bridge release registers none; the executor still ensures the ledger
// table exists so status surfaces stay available.
func pendingBlockingBackfills(ctx context.Context, db *sql.DB, driver string) ([]string, error) {
	executor, err := dbschema.NewBackfillExecutor(db, dbschema.Dialect(driver), nil)
	if err != nil {
		return nil, err
	}
	states, err := executor.Status(ctx)
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, state := range states {
		if state.Mode == dbschema.BackfillBlocking && state.State != dbschema.BackfillStateComplete {
			pending = append(pending, state.ID)
		}
	}
	return pending, nil
}
