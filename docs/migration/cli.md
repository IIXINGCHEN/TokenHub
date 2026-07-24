# tokenhub-migrate CLI

## Commands

| Command | Description |
|---------|-------------|
| `sources` | List registered source adapters |
| `inspect [source]` | Inspect a source gateway configuration |
| `extract [source]` | Extract a canonical migration bundle |
| `plan` | Dry-run: show what apply would do |
| `apply` | Apply a bundle to TokenHub |
| `verify` | Verify applied state matches bundle |
| `rollback` | Rollback to pre-apply state |

## Common Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--secret-source` | Secret resolution: env, file, prompt | `env` |
| `--id-strategy` | ID generation: stable, prefixed, source | `prefixed` |
| `--report` | Write structured report to JSON file | — |
| `--log-level` | Log level: debug, info, warn, error | `info` |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Partial success (some resources skipped) |
| 3 | Verify mismatch |
| 4 | Source unreadable |
| 5 | Sink rejected |
| 6 | Bundle schema mismatch |

## LiteLLM Walkthrough

```bash
# Inspect a LiteLLM config
tokenhub-migrate inspect litellm --from proxy_config.yaml

# Extract a bundle
tokenhub-migrate litellm extract --from proxy_config.yaml --out bundle.json

# Plan the migration
tokenhub-migrate plan --bundle bundle.json

# Apply (dry-run)
tokenhub-migrate apply --bundle bundle.json --dry-run

# Apply for real
tokenhub-migrate apply --bundle bundle.json --to https://tokenhub.example.com --token <admin-token>

# Verify
tokenhub-migrate verify --bundle bundle.json --to https://tokenhub.example.com --token <admin-token>

# Rollback if needed
tokenhub-migrate rollback --checkpoint checkpoint.json --to https://tokenhub.example.com --token <admin-token>
```
