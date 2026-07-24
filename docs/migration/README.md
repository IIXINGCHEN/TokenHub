# TokenHub Migration Framework

The TokenHub migration framework provides a repeatable, idempotent workflow for moving competing AI gateways into TokenHub.

## Architecture

See [architecture.md](./architecture.md) for the framework design and extension guide.

## Supported Sources

| Source | Adapter | Supported Versions | Status |
|--------|---------|-------------------|--------|
| LiteLLM | `litellm` | ≥1.52.0, <1.70.0 | Foundation |

See [litellm.md](./litellm.md) for LiteLLM specifics.

## Canonical Bundle

The intermediate representation used between source adapters and the TokenHub sink. See [bundle-schema.md](./bundle-schema.md) for the schema and compatibility policy.

## CLI

```bash
tokenhub-migrate litellm extract --from proxy_config.yaml --out bundle.json
tokenhub-migrate plan --bundle bundle.json --to https://tokenhub.example.com --token <admin-token>
tokenhub-migrate apply --bundle bundle.json --to https://tokenhub.example.com --token <admin-token>
tokenhub-migrate verify --bundle bundle.json --to https://tokenhub.example.com --token <admin-token>
tokenhub-migrate rollback --checkpoint checkpoint.json --to https://tokenhub.example.com --token <admin-token>
```

## Secret Handling

Secrets in the bundle are stored as `{"$secretRef": "ENV_NAME"}` references. The sink resolves them at apply time from environment variables, a file, or an interactive prompt. No plaintext secrets are embedded in the bundle.

## Documentation

- [Architecture](./architecture.md)
- [Bundle Schema](./bundle-schema.md)
- [LiteLLM Adapter](./litellm.md)
- [CLI Reference](./cli.md)
- [E2E Testing](./e2e.md)
