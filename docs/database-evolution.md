# Database Evolution

TokenHub evolves its database with explicit, forward-only migrations. This page explains the model, the maintenance commands, and how upgrades and rollbacks interact with the database.

## Model

- **Adoption baseline.** Every database carries a migration ledger (`schema_migrations`). Databases created by older releases are adopted on the next start: the frozen schema flow completes them, they are semantically verified against a reference snapshot, and the baseline row is recorded. Fresh databases are created from frozen baseline SQL, not from the ORM flow.
- **Expand migrations** add compatible structure and run automatically at startup. **Contract migrations** remove old structure and never run at startup; they only run through the maintenance command with verified preconditions.
- **Checksums and dirty state.** Every applied migration's checksum is verified on each start. A failed non-transactional migration leaves a dirty marker that refuses startup until it is repaired; a failed transactional migration rolls back only its own version.
- **Data backfills** live in their own ledger (`data_backfills`). Blocking backfills must finish before the instance reports ready; online backfills run in idempotent batches while the service keeps serving, coordinated cluster-wide through leases so a crashed executor's work is resumed by another instance.
- **Instance heartbeats.** Every running instance publishes its release with a TTL. Contract maintenance refuses to run while instances with fresh heartbeats are live.
- **Rollback compatibility.** Each release declares the database state range it can fully run on. Managed rollback runs a read-only preflight first: releases without a verified compatibility record are refused as `unknown`, and a database state outside the target release's range is refused as `incompatible`. The admin console marks rollback targets as database compatible, incompatible, or unknown.

Readiness (`/readyz`, `/healthz`) fails while the ledger is dirty or unverifiable or a blocking backfill is incomplete. Pending online backfills never affect readiness.

## Maintenance commands

The main binary ships a `db` subcommand:

```bash
tokenhub db status                                  # ledger, backfills, live instances
tokenhub db verify                                  # ledger checksums + semantic schema check
tokenhub db migrate                                 # apply pending expand migrations
tokenhub db repair --version <n>                    # clear a dirty migration (verified repair only)
tokenhub db contract --dry-run                      # preflight a contract migration
tokenhub db contract --backup-reference <ref> --maintenance
```

The database is resolved from `TOKENHUB_DATABASE_URL` (or the default SQLite path).

`contract` requires all of the following before it executes anything: every data backfill complete, no live instance heartbeats, an operator-verified backup reference, and an explicit maintenance assertion. For SQLite, create the built-in backup first; for PostgreSQL, provide the external backup reference that you verified yourself.

## Operational notes

- `tokenhub db migrate` on a database without an adoption baseline points you at a normal server start: adoption happens there, inside the serialized schema section.
- A refused contract tells you which precondition failed; nothing was executed.
- After a rollback to a previous release, the previous release keeps working on the current database; when the newer release returns, it re-verifies the ledger and continues.
- The admin console shows the read-only database evolution section (state version, readiness, compatibility range, backfills, live instances); contract and repair operations are CLI-only by design.

## For developers

- The migration runner lives in `backend/internal/dbschema`. Frozen baseline SQL is embedded per dialect under `backend/internal/dbschema/migrations/`.
- Regenerate the SQLite baseline after model changes with `UPDATE_BASELINE=1 go test ./internal/server -run TestSQLiteBaselineSQLIsCurrent`; the PostgreSQL baseline regenerates the same way with `TEST_POSTGRES_URL` set and the `integration` build tag. Tests fail on a stale baseline.
- CI runs the PostgreSQL integration suite and an N-1 two-way contract: the previous release binary creates a database, the current release adopts it and reports ready, the previous release boots again on the adopted database, and the current release returns to serving it.
