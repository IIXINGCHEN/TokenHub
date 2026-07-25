# tokenhub-migrate CLI

## Commands

| Command | Description |
|---------|-------------|
| `sources` | List registered source adapters |
| `inspect [source]` | Inspect a source gateway configuration |
| `extract [source]` | Extract a canonical migration bundle |
| `plan` | Dry-run: show what apply would do using the current store-backed sink |
| `apply` | Apply a bundle using the current store-backed sink |
| `verify` | Verify bundle consistency against the current store-backed sink instance |
| `rollback` | Rollback from a checkpoint file |

## Common Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--secret-source` | Secret resolution: env, file, prompt | `env` |
| `--id-strategy` | ID generation: stable, prefixed, source | `prefixed` |
| `--report` | Reserved for structured report output | — |
| `--log-level` | Reserved for future logging control | `info` |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 3 | Verify mismatch |
| 4 | Source unreadable |
| 5 | Sink rejected |
| 6 | Bundle schema mismatch |

## LiteLLM Walkthrough

```bash
# Inspect a LiteLLM config
tokenhub-migrate inspect litellm --from proxy_config.yaml

# Extract a bundle
tokenhub-migrate extract litellm --from proxy_config.yaml --out bundle.json

# Plan the migration
tokenhub-migrate plan --bundle bundle.json

# Apply (dry-run)
tokenhub-migrate apply --bundle bundle.json --dry-run

# Apply for real (current implementation is store-backed)
tokenhub-migrate apply --bundle bundle.json

# Verify command behavior
tokenhub-migrate verify --bundle bundle.json

# Rollback if needed
tokenhub-migrate rollback --checkpoint checkpoint.json
```
