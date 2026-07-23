<p align="center">
  <img src="frontend/public/brand/tokenhub-logo.png" alt="TokenHub" width="96" />
</p>

<h1 align="center">TokenHub</h1>

<p align="center">
  TokenHub 是面向企业的私有化 AI 网关，围绕普通用户、团队负责人和平台管理员提供分角色工作台。
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
  <a href="README.md">English</a> | 简体中文 | <a href="README.ja.md">日本語</a>
</p>

## 产品截图

| 登录控制台 | 网关概览 |
| --- | --- |
| ![Login Console](docs/assets/screenshots/login-en.png) | ![Gateway Overview](docs/assets/screenshots/overview-en.png) |
| 接口文档 | Provider 渠道 |
| ![API Documentation](docs/assets/screenshots/gateway-en.png) | ![Provider Channels](docs/assets/screenshots/providers-en.png) |
| 模型目录 | 路由策略 |
| ![Model Catalog](docs/assets/screenshots/models-en.png) | ![Routing Policies](docs/assets/screenshots/routes-en.png) |
| 用量统计 | 系统设置 |
| ![Usage Analytics](docs/assets/screenshots/usage-en.png) | ![System Settings](docs/assets/screenshots/settings-en.png) |

## 围绕三大角色设计

TokenHub 将日常模型使用、团队治理和平台运维拆成清晰的角色入口，让企业用户只看到和自己职责相关的工作流。

| 角色 | 工作台重点 | 指南 |
| --- | --- | --- |
| 普通用户 | 查看可用模型、创建项目 Key、调用模型 API、查看个人用量 | [普通用户指南](docs/zh-CN/user-guide.md) |
| 团队负责人 | 管理项目空间、项目成员、项目 Key、团队报表和项目成本归因 | [团队负责人指南](docs/zh-CN/team-leader-guide.md) |
| 平台管理员 | 配置 Provider、模型目录、路由策略、身份源、RBAC、审计和成本管控 | [管理员指南](docs/zh-CN/administrator-guide.md) |

## 平台能力

- OpenAI-Compatible 模型 API：`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`。
- Provider 渠道：OpenAI-Compatible、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、本地 vLLM/Ollama 和自定义上游。
- 模型目录和路由策略：支持优先级、权重、失败回退顺序和路由健康诊断。
- 按项目归属的 Key 管理：支持团队归属、成员权限、额度和并发限制。
- 用量统计和请求日志：可归因到用户、项目、团队、模型和成本中心。
- 身份源配置：支持 OAuth/OIDC 企业登录，并配合 RBAC 和审计追踪。
- 简洁控制台：分角色导航、全局搜索、黑白主题，以及左侧 API 导航 + 右侧详情的接口文档。
- SQLite-first 私有化部署，内置 Docker Compose 一键部署。
- PostgreSQL 支持生产环境部署，提供连接池配置。
- 管理后台支持英文、中文、日文切换。

## 多实例架构

默认安装使用 SQLite 启动一个前端实例和一个后端实例。需要横向扩容时，使用 `deploy/docker-compose.remote-postgres.yml`：由 Nginx 为可扩展的前后端副本提供统一入口，并将共享状态存储在远端 PostgreSQL 中。不得让多个后端副本共享同一个 SQLite 文件。

<p align="center">
  <img src="docs/assets/architecture/tokenhub-multi-instance.png" alt="TokenHub 多实例架构" width="1200" />
</p>

多实例模式下：

- Nginx 将管理后台、API 和健康检查流量负载均衡到健康副本。
- 后端副本将持久化配置、OAuth 会话、配额计数、审计数据、集群锁和请求并发租约统一存储在 PostgreSQL 中。
- 租约过期和归属判断使用 PostgreSQL 时钟，避免不同宿主机的时钟偏差导致租约被提前接管；失去租约后，心跳会取消对应任务或请求。
- 每个后端启动时都会同步挂载的模型目录，并通过集群租约串行执行幂等同步。
- 数据库协调故障只释放 Provider 容量，不会把健康的模型 Provider 错误计为失败。

所有后端副本必须使用相同的 `TOKENHUB_SECRET_KEY`。`TOKENHUB_DB_MAX_OPEN_CONNS` 按单副本配置，所有副本的连接池总和必须低于 PostgreSQL 的连接数上限。

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.remote-postgres.yml up -d \
  --scale tokenhub-backend=3 \
  --scale tokenhub-frontend=2
```

配置要求、健康探针和真实 PostgreSQL E2E 测试见[部署指南](docs/zh-CN/deployment.md#使用远端-postgresql-的多实例部署)。

## 快速开始

```bash
cp deploy/.env.example deploy/.env
# 将 deploy/.env 中所有 change-me 值替换为强密钥。
./deploy/install.sh
```

访问地址：

- 管理后台：`http://localhost:3000`
- 后端 API：`http://localhost:8080`
- 健康检查：`http://localhost:8080/healthz`

初始管理员账号：

- 用户名：`admin`
- 密码：`TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD` 的配置值

部署脚本会在构建前校验生产凭证，不输出敏感值地逐项提示不安全的变量。如果本次创建或重启的后端容器异常导致 Compose 失败，脚本只会自动显示本次启动产生的后端近期日志。

## 本地开发

后端：

```bash
cd backend
go run ./cmd/tokenhub
```

前端：

```bash
cd frontend
npm install
npm run dev
```

使用 SDK 示例测试模型 API 链路：

```bash
cd sdk
npm install
npm run test:deepseek
```

## 文档

- [文档首页](docs/zh-CN/README.md)
- [普通用户指南](docs/zh-CN/user-guide.md)
- [团队负责人指南](docs/zh-CN/team-leader-guide.md)
- [管理员指南](docs/zh-CN/administrator-guide.md)
- [English documentation](docs/README.md)
- [日本語ドキュメント](docs/ja/README.md)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=astaxie%2FTokenHub&type=Date&legend=top-left)](https://www.star-history.com/?repos=astaxie%2FTokenHub&type=date&legend=top-left)

## License

TokenHub 采用 [Apache License 2.0](LICENSE) 协议。
