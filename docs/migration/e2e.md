# End-to-End Migration Tests

## Overview

The current E2E harness validates the migration fixture flow around the current store-backed CLI implementation, the LiteLLM fixture, and the Docker Compose stack assets.

## Prerequisites

- Docker and Docker Compose
- Node.js 20+
- Go toolchain matching the repository

## Running Locally

```bash
cd backend && go build -o tokenhub-migrate ./cmd/tokenhub-migrate/
docker compose -f deploy/docker-compose.migration-e2e.yml config
docker compose -f deploy/docker-compose.migration-e2e.yml up -d --wait
cd sdk/migration-e2e
npm ci
TOKENHUB_MIGRATE_BIN=../../backend/tokenhub-migrate npm run test:litellm
docker compose -f ../../deploy/docker-compose.migration-e2e.yml down -v
```

## Fixture Assets

- Compose fixture: `deploy/litellm-config.yaml`
- Mock upstream config: `deploy/mock-upstream.conf`
- Extraction fixture: `sdk/migration-e2e/fixtures/proxy_config.yaml`
- Harness: `sdk/migration-e2e/litellm-e2e.mjs`

## Current Scope

The harness currently proves:
1. LiteLLM stack boots from the checked-in fixture
2. LiteLLM can answer a mocked chat-completion request
3. `extract`, `plan`, `apply`, `verify`, and `rollback` commands execute against the current CLI implementation
4. Compose and CI wiring remain consistent with the fixture layout

It does not yet prove a remote TokenHub Admin API apply/verify/rollback cycle.

## CI

The workflow runs migration unit checks on relevant backend, docs, SDK, deploy, and workflow changes. The E2E job runs on pushes and on PRs labeled `migration:e2e`.

## Troubleshooting

- Ensure Docker daemon is running
- Check that ports 4000, 8080, 8081, and 5432 are available
- Review `docker compose logs` for service-level errors
