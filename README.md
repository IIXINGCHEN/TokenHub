<p align="center">
  <img src="frontend/public/brand/tokenhub-logo.png" alt="TokenHub" width="96" />
</p>

<h1 align="center">TokenHub</h1>

<p align="center">
  TokenHub is a private enterprise AI gateway with role-based workspaces for users, team leaders, and administrators.
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26" />
  <img src="https://img.shields.io/badge/Next.js-16.2.9-black?logo=nextdotjs" alt="Next.js 16.2.9" />
  <img src="https://img.shields.io/badge/React-19.2.7-61DAFB?logo=react&logoColor=111111" alt="React 19.2.7" />
  <img src="https://img.shields.io/badge/SQLite-first-003B57?logo=sqlite&logoColor=white" alt="SQLite first" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker Compose" />
  <img src="https://img.shields.io/badge/OpenAI-Compatible-10A37F" alt="OpenAI Compatible" />
  <img src="https://img.shields.io/badge/i18n-ZH%20%7C%20EN%20%7C%20JA-6f42c1" alt="i18n ZH EN JA" />
</p>

<p align="center">
  English | <a href="README.zh-CN.md">简体中文</a> | <a href="README.ja.md">日本語</a>
</p>

## Screenshots

| Login Console | Gateway Overview |
| --- | --- |
| ![Login Console](docs/assets/screenshots/login-en.png) | ![Gateway Overview](docs/assets/screenshots/overview-en.png) |
| API Documentation | Provider Channels |
| ![API Documentation](docs/assets/screenshots/gateway-en.png) | ![Provider Channels](docs/assets/screenshots/providers-en.png) |
| Model Catalog | Routing Policies |
| ![Model Catalog](docs/assets/screenshots/models-en.png) | ![Routing Policies](docs/assets/screenshots/routes-en.png) |
| Usage Analytics | System Settings |
| ![Usage Analytics](docs/assets/screenshots/usage-en.png) | ![System Settings](docs/assets/screenshots/settings-en.png) |

## Designed Around Three Roles

TokenHub separates everyday model usage, team governance, and platform administration so enterprise users see the workflows that match their responsibility.

| Role | Workspace Focus | Guide |
| --- | --- | --- |
| User | Find available models, create project-scoped API keys, call the model API, and review personal usage | [User Guide](docs/user-guide.md) |
| Team Leader | Manage project spaces, project members, project keys, team reports, and project cost attribution | [Team Leader Guide](docs/team-leader-guide.md) |
| Administrator | Configure providers, model catalog, routing policies, identity sources, RBAC, audit, and cost controls | [Administrator Guide](docs/administrator-guide.md) |

## Platform Capabilities

- OpenAI-compatible model APIs: `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`.
- Provider channels for OpenAI-compatible, Azure OpenAI, Anthropic, Gemini, DeepSeek, Qwen, local vLLM/Ollama, and custom upstreams.
- Model catalog and routing policies with priority, weight, failover order, and route health diagnostics.
- Project-scoped key management with team ownership, member permissions, quotas, and concurrency controls.
- Usage analytics and request logs attributed to user, project, team, model, and cost center.
- Identity source configuration for OAuth/OIDC enterprise sign-in, plus RBAC and audit trails.
- Clean console with compact role-aware navigation, global search, light/dark mode, and split-view API documentation.
- SQLite-first private deployment with Docker Compose support.
- PostgreSQL support for production deployments with connection pooling. See the [PostgreSQL setup guide](docs/postgresql-setup.md).
- Console language switching for English, Chinese, and Japanese.

## Multi-Instance Architecture

The default installation starts one frontend and one backend with SQLite. For horizontal scaling, use `deploy/docker-compose.remote-postgres.yml`: it places Nginx in front of scalable frontend and backend replicas and stores shared state in a remote PostgreSQL database. A SQLite file must never be shared by multiple backend replicas.

<p align="center">
  <img src="docs/assets/architecture/tokenhub-multi-instance.png" alt="TokenHub multi-instance architecture" width="1200" />
</p>

In multi-instance mode:

- Nginx load-balances console, API, and health-check traffic across healthy replicas.
- Backend replicas keep durable configuration, OAuth sessions, quota buckets, audit data, cluster locks, and in-flight concurrency leases in PostgreSQL.
- Lease expiry and ownership decisions use the PostgreSQL clock, avoiding early takeover caused by clock skew between hosts. Heartbeats cancel work when lease ownership is lost.
- The mounted model catalog is synchronized on every backend startup; a cluster lease serializes the idempotent synchronization across replicas.
- Coordination failures release provider capacity without incorrectly marking a healthy model provider as failed.

All backend replicas must use the same `TOKENHUB_SECRET_KEY`. Size `TOKENHUB_DB_MAX_OPEN_CONNS` per replica so the combined connection pool stays below the PostgreSQL limit.

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.remote-postgres.yml up -d \
  --scale tokenhub-backend=3 \
  --scale tokenhub-frontend=2
```

See the [deployment guide](docs/deployment.md#multi-instance-deployment-with-remote-postgresql) for configuration requirements, probes, and the real PostgreSQL E2E test.

## Quick Start

```bash
cp deploy/.env.example deploy/.env
# Replace every change-me value in deploy/.env with a strong secret.
./deploy/install.sh
```

Open:

- Admin console: `http://localhost:3000`
- Backend API: `http://localhost:8080`
- Health check: `http://localhost:8080/healthz`

Initial admin login:

- Username: `admin`
- Password: the value of `TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD`

The deployment script validates production credentials before building. It reports each unsafe variable without printing secret values. If Compose fails because a backend container created or restarted by that attempt is unhealthy, the script automatically shows only that attempt's recent backend logs.

## Local Development

Backend:

```bash
cd backend
go run ./cmd/tokenhub
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

Smoke-test the model API with the included SDK example:

```bash
cd sdk
npm install
npm run test:deepseek
```

## Documentation

- [Documentation home](docs/README.md)
- [User Guide](docs/user-guide.md)
- [Team Leader Guide](docs/team-leader-guide.md)
- [Administrator Guide](docs/administrator-guide.md)
- [简体中文文档](docs/zh-CN/README.md)
- [日本語ドキュメント](docs/ja/README.md)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=astaxie%2FTokenHub&type=Date&legend=top-left)](https://www.star-history.com/?repos=astaxie%2FTokenHub&type=date&legend=top-left)

## License

TokenHub is licensed under the [Apache License 2.0](LICENSE).
