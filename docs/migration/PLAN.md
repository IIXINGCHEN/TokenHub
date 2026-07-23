# TokenHub Migration Framework Plan

Branch: `feat/migration-framework`

## 1. Goals

- Ship a reusable migration framework so that competing AI gateways (starting with LiteLLM) can be moved into TokenHub with a repeatable, idempotent workflow.
- Decouple **source adapters** (read side) from the **TokenHub sink** (write side) through a versioned intermediate representation, `CanonicalMigrationBundle`.
- Provide a first-class CLI (`tokenhub-migrate`) plus documentation and end-to-end tests so external teams can trust and extend the framework.

## 2. Non-Goals

- Migrating historical usage/spend logs. Only configuration and identity data are in scope for v1.
- Preserving raw API key secrets from the source gateway. New secrets are issued by TokenHub and reported for rotation.
- Runtime traffic mirroring or dual-write. This is a one-shot (repeatable) configuration migration, not a proxy.

## 3. Architecture Overview

```
+-------------------+     extract       +------------------------------+     apply       +-------------------+
|  Source Gateway   |  --------------> |  CanonicalMigrationBundle   ----> |  TokenHub Admin   |
|  (LiteLLM, ...)  |  Source Adapter  |  versioned JSON, no secrets  |     Sink        |     API (HTTP)    |
+-------------------+                  +-------------------------+                +-------------------+
                                                 ^        |
                                                 |        v
                                              plan     verify (diff + consistency report)
```

- **Source Adapter** implements a small interface, reads competitor state, emits a bundle.
- **Sink** consumes a bundle and performs idempotent upserts through TokenHub Admin API and writes a checkpoint for rollback.
- **Engine** wraps extract / plan / apply / verify / rollback with structured reports and consistent exit codes.
- **CLI** (`backend/cmd/tokenhub-migrate`) is a thin cobra shell over the engine.

## 4. Repository Layout (new)

```
backend/
  cmd/tokenhub-migrate/               # cobra CLI entrypoint
  internal/migration/
    bundle/                        # CanonicalMigrationBundle types, JSON schema, secret refs
    source/                           # Source interface + registry
      litellm/                        # First source adapter
    sink/tokenhub/                    # Admin API client, plan / apply / verify / rollback
    engine/                           # Pipeline orchestration, reports, errors
    cli/                              # Command implementations
docs/migration/
  README.md                           # Index (EN)
  architecture.md                     # Framework architecture and how to add a source
  bundle-schema.md                    # Canonical bundle fields and compatibility policy
  litellm.md                          # LiteLLM specifics and supported versions
  zh-CN/, ja/                         # Mirrored per AGENTS.md
sdk/migration-e2e/                    # Node-based E2E harness
deploy/docker-compose.migration-e2e.yml
```

## 5. Canonical Bundle (v1.0)

- `schema_version` uses SemVer. CLI refuses mismatched majors.
- `source { type, version, captured_at }` records provenance.
- Resource arrays: `providers`, `provider_resources`, `models`, `routes`, `teams`, `projects`, `users`, `api_keys`, `quota_policies`.
- Every item carries an `external_ref` (source-side stable id) and a `spec` that mirrors backend types in `backend/internal/server/types.go`.
- Cross references use `*_ref` strings resolved at apply time. Ids are minted via `--id-strategy stable|prefixed|source` (default `prefixed`, for example `ll_<sha1(external_ref)[:12]>`).
- Secrets are never stored inline. Fields use `{"$secretRef": "ENV_NAME"}` and are resolved by `--secret-source env|file|prompt`.
- `notes[]` carries `severity` (info | warn | blocker), `code`, `source_ref`, `message`. `blocker` requires `--allow-blockers` on apply.

## 6. CLI Surface

```
tokenhub-migrate sources
tokenhub-migrate <source> inspect --from <path|dsn>
tokenhub-migrate <source> extract --from <path|dsn> --out bundle.json
tokenhub-migrate plan   --bundle bundle.json --to <api> --token <t>
tokenhub-migrate apply  --bundle bundle.json --to <api> --token <t> [--dry-run] [--only ...] [--skip ...] [--conflict skip|update|fail] [--checkpoint chk.json]
tokenhub-migrate verify --bundle bundle.json --to <api> --token <t>
tokenhub-migrate rollback --checkpoint chk.json --to <api> --token <t>
```

Common flags: `--concurrency`, `--rate-limit`, `--timeout`, `--secret-source`, `--id-strategy`, `--report report.json`, `--log-level`.

Exit codes: `0` ok, `2` partial (HTTP 207 semantics), `3` verify mismatch, `4` source unreadable, `5` sink rejected, `6` bundle schema mismatch.

## 7. Sink Idempotency Rules

| Resource | Match key | Write |
| --- | --- | --- |
| Provider | `(name, type)` | `POST /api/admin/providers` or `PATCH /api/admin/providers/{id}` |
| ProviderResource | `(provider_id, name)` | `POST /api/admin/provider-resources[?action=import]` |
| Model | `name` | `POST /api/admin/models` or `PATCH /api/admin/models/{name}` |
| ModelRoute | `(model_name, provider_resource_id, provider_model)` | `POST /api/admin/routing-rules` or `PATCH` |
| Project / Team | `name` scoped by parent | `POST` / `PATCH` |
| APIKey | `(project_id, name)` | `POST /api/admin/api-keys`; new secret returned in `report.new_keys[]` |
| User | email or username | `POST /api/admin/users/import` |
| Quota | attached to `APIKey.Limits` or `POST /api/admin/quota-policies` | idem |

Every apply writes a checkpoint list of `(resource, id, action)` so `rollback` can delete or patch back.

## 8. LiteLLM Adapter Scope

- Input: `proxy_config.yaml` plus optional LiteLLM Postgres DSN.
- LiteLLM versions (initial): `>=1.52.0, <1.70.0`. The adapter probes `LiteLLM_Config` and the schema hash, and refuses unknown majors.
- Mapping table lives in `backend/internal/migration/source/litellm/mapping.go` and is documented in `docs/migration/litellm.md`.
- Unsupported features (guardrails, audio and image endpoints, prompt caching, callbacks, pass-through routes, non-standard providers) become `notes[]` entries with actionable messages.

## 9. Testing Strategy

- **Unit**: bundle schema round-trip, secret redaction, id strategies, per-resource sink upsert against `MemoryStore` and `httptest`.
- **Adapter fixtures**: `backend/internal/migration/source/litellm/testdata/<version>/{proxy_config.yaml, dump.sql, expected-bundle.json}`.
- **Sink integration**: full bundle apply against an in-process TokenHub server (SQLite tmp), then verify and rollback.
- **E2E**: `deploy/docker-compose.migration-e2e.yml` boots TokenHub, an official LiteLLM image, Postgres, and a mock upstream. `sdk/migration-e2e/litellm-e2e.mjs`:
  1. Seed LiteLLM with a fixture config and a virtual key.
  2. Send a chat completion through LiteLLM to prove the fixture works.
  3. Run `tokenhub-migrate litellm extract | plan | apply | verify` against TokenHub.
  4. Rotate the emitted key, send the same request to TokenHub, and assert an identical response contract from the mock upstream.
  5. Re-run `apply` to prove idempotency (zero writes on the second run).

## 10. Delivery Plan (Sub-Issues)

1. **Bundle + Sink foundation** — canonical types, JSON schema, sink client, plan / apply / verify / rollback against `MemoryStore`, unit tests, docs skeleton.
2. **LiteLLM source adapter** — config and DB readers, mapping, fixtures, adapter tests, `docs/migration/litellm.md` (EN / zh-CN / ja).
3. **CLI (`tokenhub-migrate`)** — cobra commands, flags, exit codes, report writers, CLI tests, user-facing docs.
4. **LiteLLM E2E** — compose stack, Node harness, CI job, troubleshooting doc.
5. **Supporting: docs and AGENTS updates** — three-language sync, AGENTS.md guidance for adding new sources, PR template check items.
6. **Supporting: CI wiring** — `go test ./backend/internal/migration/...`, `go vet`, `gofmt`, optional E2E job gated on a label.

Each sub-issue completes with:

- Passing `gofmt`, `go vet`, `go test ./...` (and CLI or E2E where applicable).
- A subagent-driven code review before the PR opens.
- Updated documentation in all three languages when user-facing.
- An explicit note in the PR body about which parts of the framework are wired end-to-end.
