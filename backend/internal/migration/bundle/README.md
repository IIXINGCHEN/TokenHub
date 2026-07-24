# migration/bundle

CanonicalMigrationBundle types, schema validation, secret references,
and helper utilities shared by source adapters and sinks.

Current contract notes:
- Bundle JSON is versioned by `schema_version` and validated by the
  embedded JSON Schema.
- Secrets are represented as `{"$secretRef":"ENV_NAME"}` and never
  embedded as plaintext bundle fields.
- `quota_policies` is reserved in the v1 bundle shape but is not yet
  consumed by the TokenHub sink foundation.

Owned by issue #2.
