# End-to-End Migration Tests

## Overview

The E2E test harness proves the full migration framework by migrating a real LiteLLM instance into TokenHub and replaying identical requests.

## Prerequisites

- Docker and Docker Compose
- Node.js 20+
- Go 1.26+

## Running Locally

```bash
# Build the CLI
cd backend && go build -o tokenhub-migrate ./cmd/tokenhub-migrate/

# Start the stack
docker compose -f deploy/docker-compose.migration-e2e.yml up -d --wait

# Run E2E tests
cd sdk/migration-e2e
npm ci
TOKENHUB_MIGRATE_BIN=../../backend/tokenhub-migrate npm run test:litellm

# Clean up
docker compose -f deploy/docker-compose.migration-e2e.yml down -v
```

## Test Steps

1. Seed LiteLLM with a fixture config and virtual key
2. Send a chat completion through LiteLLM, verify response
3. Extract bundle from LiteLLM config
4. Plan and apply the bundle to TokenHub
5. Verify the applied state
6. Re-run apply to prove idempotency (zero writes)
7. Run verify to confirm clean state
8. Run rollback to revert

## CI

The E2E test runs in CI on pushes to `main` and on PRs labeled `migration:e2e`.

## Troubleshooting

- Ensure Docker daemon is running
- Check that ports 4000, 8080, 8081, 5432 are not in use
- Review `docker compose logs` for service-level errors
