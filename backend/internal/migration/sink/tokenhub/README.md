# migration/sink/tokenhub

Store-backed and future HTTP-backed TokenHub sink implementations for
planning, applying, verifying, and rolling back migration bundles.

Current foundation scope:
- Applies providers, provider resources, models, routes, users,
  projects, and API keys against a TokenHub store.
- Verifies bundle presence by business keys and supports checkpoint-
  based rollback for resources created during apply.
- Uses canonical reference fields such as `provider_ref`, `team_ref`,
  and `project_ref` instead of requiring source external IDs inside the
  embedded TokenHub specs.
- Enforces zero-write idempotency on a second apply when the target
  state already matches the bundle.
- Keeps resolved raw API key secrets only for keys created during the
  current sink instance lifecycle via `NewKeys()`.
- Does not yet implement quota policy materialization or update/delete
  rollback for pre-existing resources.

Owned by issue #2.

Sink that applies a CanonicalMigrationBundle through TokenHub Admin API.

Owned by issue #2.
