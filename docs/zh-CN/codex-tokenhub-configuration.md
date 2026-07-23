# Codex 接入 TokenHub：全局切换与局部切换

本文说明如何让 Codex CLI 通过 TokenHub 调用模型，并区分两种使用方式：

- **全局切换**：TokenHub 成为当前用户启动 Codex 时的默认模型服务。
- **局部切换**：保留现有默认服务，只在指定命令或项目会话中使用 TokenHub。

Codex 桌面端、CLI 和 IDE 扩展共用用户级 `config.toml`。本文涉及 `--profile` 的局部切换命令时，以 Codex CLI 为准。

## 1. 接入前准备

准备以下真实信息：

| 配置 | 获取位置 |
| --- | --- |
| TokenHub Base URL | TokenHub 部署地址，并以 `/v1` 结尾 |
| TokenHub 项目 API Key | TokenHub 控制台的 **Key 管理** |
| 模型 ID | 使用项目 API Key 调用 `GET /v1/models` 获取 |

控制台登录令牌不能代替项目 API Key。

先在当前终端注入真实配置。下面的变量只在当前 Shell 会话中生效：

```bash
export TOKENHUB_BASE_URL="填写实际的 TokenHub Base URL"
read -s "TOKENHUB_API_KEY?TokenHub 项目 API Key: "
export TOKENHUB_API_KEY
echo
```

查询当前项目 Key 实际可用的模型：

```bash
curl --fail-with-body \
  --url "${TOKENHUB_BASE_URL%/}/models" \
  --header "Authorization: Bearer ${TOKENHUB_API_KEY}"
```

从响应的 `data[].id` 中选择一个已经配置健康路由的模型 ID。不要根据上游模型名称猜测，也不要直接照搬其他环境的模型 ID。

Codex 使用 Responses API 的流式响应。正式切换前，应确认所选模型能够通过 TokenHub 完成真实的 `POST /v1/responses` 流式调用：

```bash
curl --fail-with-body --no-buffer \
  --request POST \
  --url "${TOKENHUB_BASE_URL%/}/responses" \
  --header "Authorization: Bearer ${TOKENHUB_API_KEY}" \
  --header "Content-Type: application/json" \
  --data '{
    "model": "填写 GET /v1/models 返回的实际模型 ID",
    "input": "仅回复：连接成功",
    "stream": true
  }'
```

当前 TokenHub 仅对支持 Codex Responses 流式转发的资源开放该能力。如果返回 `responses_stream_unsupported`，需要管理员检查该模型的路由和 Provider 资源类型，不能只修改本机 Codex 配置绕过。

## 2. 全局切换：TokenHub 作为默认环境

适合当前用户的大多数 Codex 会话都需要经过 TokenHub 的场景。

### 2.1 打开用户配置

用户配置默认位于：

- macOS / Linux：`~/.codex/config.toml`
- Windows：`%USERPROFILE%\.codex\config.toml`

也可以从 Codex 设置中选择 **Open config.toml**。

### 2.2 配置 TokenHub Provider

将下面内容合并到用户级 `config.toml`。`model` 和 `base_url` 必须替换为第 1 节确认过的真实值：

```toml
model_provider = "tokenhub"
model = "填写实际模型 ID"

[model_providers.tokenhub]
name = "TokenHub"
base_url = "填写实际的 TokenHub Base URL"
env_key = "TOKENHUB_API_KEY"
env_key_instructions = "启动 Codex 前请设置 TOKENHUB_API_KEY"
wire_api = "responses"
```

注意：

- `base_url` 应包含 `/v1`，末尾是否有 `/` 均可。
- API Key 不要写进 `config.toml`，Codex 会读取 `env_key` 指定的 `TOKENHUB_API_KEY`。
- 自定义 Provider 名称使用 `tokenhub`，不要尝试覆盖 Codex 内置的 `openai` Provider。
- 如果文件中已经存在 `model_provider`、`model` 或 `[model_providers.tokenhub]`，应修改原配置，不能重复声明同一个 TOML 键或表。

### 2.3 启动与验证

确保启动 Codex 的进程能读取项目 Key：

```bash
export TOKENHUB_API_KEY="填写真实的 TokenHub 项目 API Key"
codex
```

不建议把真实 Key 明文提交到仓库，也不建议写入仓库内的启动脚本。长期使用时，应由组织批准的密钥管理工具向启动进程注入 `TOKENHUB_API_KEY`。

切换成功后，可在 TokenHub 的 **请求日志** 中按时间、项目和模型确认请求已经进入网关。

### 2.4 恢复原默认环境

要恢复 Codex 原有默认 Provider：

1. 从用户级 `config.toml` 中移除或修改顶层的 `model_provider = "tokenhub"`。
2. 将顶层 `model` 恢复为原来的模型配置，或删除该行以使用 Codex 默认值。
3. `[model_providers.tokenhub]` 可以保留，未被 `model_provider` 选中时不会生效。
4. 完全退出并重新启动 Codex。

## 3. 局部切换：只在指定会话使用 TokenHub

适合需要在官方默认环境与 TokenHub 之间频繁切换，或只让某个项目的 Codex 会话经过 TokenHub 的场景。

推荐使用 Codex 命名 Profile。Profile 不改变基础用户配置，只有显式传入 `--profile tokenhub` 时才生效。

### 3.1 创建 TokenHub Profile

在 Codex 用户目录中新建：

- macOS / Linux：`~/.codex/tokenhub.config.toml`
- Windows：`%USERPROFILE%\.codex\tokenhub.config.toml`

文件内容如下，仍然必须填写实际 Base URL 和模型 ID：

```toml
model_provider = "tokenhub"
model = "填写实际模型 ID"

[model_providers.tokenhub]
name = "TokenHub"
base_url = "填写实际的 TokenHub Base URL"
env_key = "TOKENHUB_API_KEY"
env_key_instructions = "启动 Codex 前请设置 TOKENHUB_API_KEY"
wire_api = "responses"
```

Codex 0.134.0 及以上版本使用独立的 `<profile>.config.toml` 文件。不要把配置写成旧版的 `[profiles.tokenhub]` 表。

### 3.2 在当前项目局部启动

进入实际项目目录后启动：

```bash
export TOKENHUB_API_KEY="填写真实的 TokenHub 项目 API Key"
codex --profile tokenhub
```

也可以从任意目录指定 Codex 工作目录：

```bash
export TOKENHUB_API_KEY="填写真实的 TokenHub 项目 API Key"
codex --profile tokenhub --cd "/填写实际项目绝对路径"
```

非交互任务使用：

```bash
export TOKENHUB_API_KEY="填写真实的 TokenHub 项目 API Key"
codex exec --profile tokenhub --cd "/填写实际项目绝对路径" "填写本次真实任务"
```

不带 `--profile tokenhub` 启动时，Codex 继续使用原来的默认环境：

```bash
codex
```

### 3.3 为什么不把 Provider 写进项目 `.codex/config.toml`

Codex 会读取受信任仓库中的 `.codex/config.toml`，但出于凭证重定向安全考虑，项目级配置不能覆盖以下 Provider 相关键：

- `openai_base_url`
- `model_provider`
- `model_providers`
- `profile` / `profiles`

Codex 会忽略这些键并在启动时警告。因此，“局部切换”应使用用户目录下的 Profile，再通过 `--profile tokenhub` 按次选择；不要把 TokenHub Provider 配置提交进项目仓库。

项目级 `.codex/config.toml` 仍可配置允许的项目设置，例如模型、沙箱或 MCP。配置优先级从高到低为：

1. CLI 参数和 `--config`
2. 受信任项目中的 `.codex/config.toml`
3. `--profile` 选中的 Profile 文件
4. 用户级 `~/.codex/config.toml`
5. 系统配置和 Codex 内置默认值

如果项目级配置声明了另一个 `model`，它会覆盖 Profile 中的模型。此时可以在本次启动时显式指定从 TokenHub 查询到的实际模型 ID：

```bash
codex --profile tokenhub --model "填写实际模型 ID"
```

## 4. 常见问题

### Codex 提示缺少 `TOKENHUB_API_KEY`

当前 Codex 进程没有继承环境变量。请在启动 Codex 的同一个终端中检查：

```bash
test -n "${TOKENHUB_API_KEY:-}" && echo "TOKENHUB_API_KEY 已注入"
```

不要输出 Key 本身。通过桌面图标启动的应用通常不会自动继承终端临时设置的变量。

### 返回 401

确认使用的是 TokenHub **项目 API Key**，并检查 Key 是否启用、过期或被轮换。控制台登录令牌不能调用 `/v1/*`。

### 返回 403

检查项目状态、成员权限、Key 的模型白名单和调用策略。

### 返回 404 或 503

检查配置中的模型 ID 是否来自当前 Key 的 `GET /v1/models` 响应，并请管理员确认模型存在启用且健康的路由。

### 返回 `responses_stream_unsupported`

所选路由不能提供 Codex 所需的流式 Responses API。请管理员调整模型路由或 Provider 资源；改用 Chat Completions 路由不能解决 Codex 客户端的协议要求。

### 修改配置后仍使用旧环境

完全退出当前 Codex 进程后重新启动，并确认启动命令是否带有 `--profile`、`--model` 或 `--config`。这些 CLI 参数的优先级高于用户配置和 Profile。

## 5. 官方配置依据

- [Codex Config basics](https://learn.chatgpt.com/docs/config-file/config-basic)
- [Codex Advanced Configuration：Profiles 与自定义 Provider](https://learn.chatgpt.com/docs/config-file/config-advanced)
- [Codex Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference)
