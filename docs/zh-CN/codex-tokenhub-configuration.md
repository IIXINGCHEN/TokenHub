# Codex 接入 TokenHub：四种配置方式与恢复方法

> 本文说明如何让 Codex 通过 TokenHub 调用模型，并分别介绍局部配置、命令窗口临时使用、CLI 全局配置和 Codex 桌面端配置。
>
> 每种方式都附有恢复方法。第一次接入建议先使用“命令窗口临时使用”验证，再决定是否保存配置。

## 先选择适合的方式

| 方式 | 影响范围 | 是否持久 | 适合场景 | 恢复方式 |
| --- | --- | --- | --- | --- |
| 局部配置（Profile） | 仅显式选择该 Profile 的会话 | 是 | 只让部分项目或任务使用 TokenHub | 启动时不带 `--profile tokenhub` |
| 命令窗口临时使用 | 当前命令或当前终端窗口 | 否 | 首次验证、偶尔使用 | 关闭终端，或清除临时环境变量 |
| CLI 全局配置 | 当前用户的本地 Codex 会话 | 是 | CLI 默认使用 TokenHub | 恢复用户级 `config.toml` |
| Codex 桌面端配置 | 桌面端、CLI 和 IDE 扩展 | 是 | 从 Codex 设置中完成长期配置 | 恢复同一份 `config.toml` 并重启 |

需要特别注意：

- **CLI 与 Codex 桌面端共用用户级 `config.toml`。** 第 3、4 种方式只是不同的配置入口，不是两套互相独立的配置。
- 受信任项目中的 `.codex/config.toml` 可以配置模型、沙箱和 MCP 等项目设置，但不能覆盖 `model_provider`、`model_providers` 和 `openai_base_url`。因此，本文使用 Profile 实现局部切换。
- TokenHub 项目 API Key 属于敏感凭证，不要写入仓库、提交到 Git，也不要直接放在命令历史中。

---

## 一、准备真实的 TokenHub 配置

开始前需要准备：

| 配置 | 获取位置 |
| --- | --- |
| TokenHub Base URL | TokenHub 的实际部署地址，以 `/v1` 结尾 |
| TokenHub 项目 API Key | TokenHub 控制台的 **Key 管理** |
| 模型 ID | 使用项目 API Key 调用 `GET /v1/models` 获取 |

控制台登录令牌不能代替项目 API Key。

### 1. 在当前终端读取配置

macOS 默认的 zsh：

```bash
export TOKENHUB_BASE_URL="填写实际的 TokenHub Base URL"
read -r -s "TOKENHUB_API_KEY?TokenHub 项目 API Key: "
export TOKENHUB_API_KEY
echo
```

Bash：

```bash
export TOKENHUB_BASE_URL="填写实际的 TokenHub Base URL"
read -r -s -p "TokenHub 项目 API Key: " TOKENHUB_API_KEY
export TOKENHUB_API_KEY
echo
```

Windows PowerShell：

```powershell
$env:TOKENHUB_BASE_URL = Read-Host "TokenHub Base URL（需要以 /v1 结尾）"
$tokenHubSecureKey = Read-Host "TokenHub 项目 API Key" -AsSecureString
$env:TOKENHUB_API_KEY = [System.Net.NetworkCredential]::new("", $tokenHubSecureKey).Password
Remove-Variable tokenHubSecureKey
```

这些变量只在当前终端会话中生效。

### 2. 查询当前 Key 可见的真实模型

macOS、Linux 或 Git Bash：

```bash
curl --fail-with-body \
  --url "${TOKENHUB_BASE_URL%/}/models" \
  --header "Authorization: Bearer ${TOKENHUB_API_KEY}"
```

Windows PowerShell：

```powershell
$tokenHubModels = Invoke-RestMethod `
  -Uri "$($env:TOKENHUB_BASE_URL.TrimEnd('/'))/models" `
  -Headers @{ Authorization = "Bearer $env:TOKENHUB_API_KEY" }

$tokenHubModels.data | Select-Object id
```

从响应的 `data[].id` 中选择实际模型 ID，不要根据上游模型名称猜测，也不要照搬其他环境的模型 ID。

把选定的模型 ID 保存到当前终端：

macOS、Linux 或 Git Bash：

```bash
read -r "TOKENHUB_MODEL_ID?输入上一步返回的实际模型 ID: "
export TOKENHUB_MODEL_ID
```

如果 Bash 不支持上面的提示语法，使用：

```bash
read -r -p "输入上一步返回的实际模型 ID: " TOKENHUB_MODEL_ID
export TOKENHUB_MODEL_ID
```

Windows PowerShell：

```powershell
$env:TOKENHUB_MODEL_ID = Read-Host "输入上一步返回的实际模型 ID"
```

### 3. 验证 Responses 流式调用

模型出现在 `GET /v1/models` 响应中，只代表当前 Key 可以看到它，不代表此刻一定存在健康路由。Codex 使用 Responses API 的流式响应，因此还需要完成一次真实调用。

macOS、Linux 或 Git Bash：

```bash
curl --fail-with-body --no-buffer \
  --request POST \
  --url "${TOKENHUB_BASE_URL%/}/responses" \
  --header "Authorization: Bearer ${TOKENHUB_API_KEY}" \
  --header "Content-Type: application/json" \
  --data "$(printf '{"model":"%s","input":"仅回复：连接成功","stream":true}' "$TOKENHUB_MODEL_ID")"
```

Windows PowerShell：

```powershell
$tokenHubRequestBody = @{
  model = $env:TOKENHUB_MODEL_ID
  input = "仅回复：连接成功"
  stream = $true
} | ConvertTo-Json -Compress

Invoke-WebRequest `
  -Method Post `
  -Uri "$($env:TOKENHUB_BASE_URL.TrimEnd('/'))/responses" `
  -Headers @{ Authorization = "Bearer $env:TOKENHUB_API_KEY" } `
  -ContentType "application/json" `
  -Body $tokenHubRequestBody
```

如果返回 `responses_stream_unsupported`，需要管理员检查该模型的路由和 Provider 资源类型，不能通过修改本机 Codex 配置绕过。

---

## 二、方式 1：局部配置，只在指定会话使用

推荐使用 Codex 命名 Profile。Profile 文件保存在用户目录，但只有显式传入 `--profile tokenhub` 时才生效，因此可以只让指定项目或任务经过 TokenHub。

### 1. 创建 Profile

Profile 文件路径：

- macOS / Linux：`~/.codex/tokenhub.config.toml`
- Windows：`%USERPROFILE%\.codex\tokenhub.config.toml`

如果该文件已经存在，先备份原文件，不要直接覆盖：

macOS / Linux：

```bash
if [ -f "$HOME/.codex/tokenhub.config.toml" ]; then
  cp -p "$HOME/.codex/tokenhub.config.toml" \
    "$HOME/.codex/tokenhub.config.toml.before-edit.$(date +%Y%m%d-%H%M%S)"
fi
```

Windows PowerShell：

```powershell
if (Test-Path "$env:USERPROFILE\.codex\tokenhub.config.toml") {
  $tokenHubBackupTime = Get-Date -Format "yyyyMMdd-HHmmss"
  Copy-Item `
    "$env:USERPROFILE\.codex\tokenhub.config.toml" `
    "$env:USERPROFILE\.codex\tokenhub.config.toml.before-edit.$tokenHubBackupTime"
}
```

将以下内容写入或合并到文件中：

```toml
model_provider = "tokenhub"
model = "填写 GET /v1/models 返回的实际模型 ID"

[model_providers.tokenhub]
name = "TokenHub"
base_url = "填写实际的 TokenHub Base URL"
env_key = "TOKENHUB_API_KEY"
env_key_instructions = "启动 Codex 前请设置 TOKENHUB_API_KEY"
wire_api = "responses"
```

`base_url` 必须包含 `/v1`。同一个文件中不能重复声明 `model_provider`、`model` 或 `[model_providers.tokenhub]`。

Codex 0.134.0 及以上版本使用独立的 `<profile>.config.toml` 文件，不要再使用旧版 `[profiles.tokenhub]` 表。

### 2. 局部启动

先确认当前终端已经设置 `TOKENHUB_API_KEY`，再进入实际项目：

```bash
codex --profile tokenhub
```

从任意目录指定工作目录：

```bash
codex --profile tokenhub --cd "/填写实际项目绝对路径"
```

执行非交互任务：

```bash
codex exec --profile tokenhub --cd "/填写实际项目绝对路径" "填写本次真实任务"
```

如果项目级 `.codex/config.toml` 设置了另一个模型，可以在本次启动时显式使用 TokenHub 返回的模型 ID：

```bash
codex --profile tokenhub --model "填写 GET /v1/models 返回的实际模型 ID"
```

### 3. 如何恢复

临时恢复原环境时，直接启动 Codex，不选择 Profile：

```bash
codex
```

需要停用该 Profile 时，可以把文件改名，保留可恢复副本：

macOS / Linux：

```bash
mv "$HOME/.codex/tokenhub.config.toml" "$HOME/.codex/tokenhub.config.toml.disabled"
```

Windows PowerShell：

```powershell
Rename-Item `
  "$env:USERPROFILE\.codex\tokenhub.config.toml" `
  "tokenhub.config.toml.disabled"
```

如果创建 Profile 前已经存在同名文件，应恢复当时的备份，而不是使用上述文件改名方式替代原配置。

macOS / Linux：

```bash
ls -1t "$HOME"/.codex/tokenhub.config.toml.before-edit.*
cp -p "填写需要恢复的实际备份文件路径" \
  "$HOME/.codex/tokenhub.config.toml"
```

Windows PowerShell：

```powershell
Get-ChildItem "$env:USERPROFILE\.codex\tokenhub.config.toml.before-edit.*" |
  Sort-Object LastWriteTime -Descending

Copy-Item `
  "填写需要恢复的实际备份文件路径" `
  "$env:USERPROFILE\.codex\tokenhub.config.toml"
```

---

## 三、方式 2：在命令窗口中临时使用

这种方式不修改任何配置文件。Provider、模型和 Base URL 都通过 `-c` 只覆盖本次 Codex 进程。

### 1. macOS、Linux 或 Git Bash

完成第一节的环境变量设置后运行：

```bash
codex \
  -c 'model_provider="tokenhub"' \
  -c "model=\"${TOKENHUB_MODEL_ID}\"" \
  -c 'model_providers.tokenhub.name="TokenHub"' \
  -c "model_providers.tokenhub.base_url=\"${TOKENHUB_BASE_URL}\"" \
  -c 'model_providers.tokenhub.env_key="TOKENHUB_API_KEY"' \
  -c 'model_providers.tokenhub.env_key_instructions="启动 Codex 前请设置 TOKENHUB_API_KEY"' \
  -c 'model_providers.tokenhub.wire_api="responses"'
```

非交互任务可以把 `codex` 换成 `codex exec`，并在最后添加真实任务内容。

### 2. Windows PowerShell

完成第一节的环境变量设置后运行：

```powershell
codex `
  -c 'model_provider="tokenhub"' `
  -c "model=`"$env:TOKENHUB_MODEL_ID`"" `
  -c 'model_providers.tokenhub.name="TokenHub"' `
  -c "model_providers.tokenhub.base_url=`"$env:TOKENHUB_BASE_URL`"" `
  -c 'model_providers.tokenhub.env_key="TOKENHUB_API_KEY"' `
  -c 'model_providers.tokenhub.env_key_instructions="启动 Codex 前请设置 TOKENHUB_API_KEY"' `
  -c 'model_providers.tokenhub.wire_api="responses"'
```

### 3. 如何恢复

退出本次 Codex 后，`-c` 覆盖会自动失效。再次直接运行 `codex`，就会使用原配置。

如果还要清除当前终端中的临时变量：

macOS、Linux 或 Git Bash：

```bash
unset TOKENHUB_BASE_URL TOKENHUB_API_KEY TOKENHUB_MODEL_ID
```

Windows PowerShell：

```powershell
Remove-Item Env:TOKENHUB_BASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:TOKENHUB_API_KEY -ErrorAction SilentlyContinue
Remove-Item Env:TOKENHUB_MODEL_ID -ErrorAction SilentlyContinue
```

关闭当前终端窗口也会清除这些临时变量。

---

## 四、方式 3：配置 Codex CLI 的全局默认环境

适合当前用户的大多数本地 Codex CLI 会话都需要经过 TokenHub 的情况。

### 1. 备份并打开用户配置

用户配置文件：

- macOS / Linux：`~/.codex/config.toml`
- Windows：`%USERPROFILE%\.codex\config.toml`

如果文件已经存在，先复制一份带时间戳的备份。

macOS / Linux：

```bash
if [ -f "$HOME/.codex/config.toml" ]; then
  cp -p "$HOME/.codex/config.toml" \
    "$HOME/.codex/config.toml.before-tokenhub.$(date +%Y%m%d-%H%M%S)"
fi
```

Windows PowerShell：

```powershell
if (Test-Path "$env:USERPROFILE\.codex\config.toml") {
  $tokenHubBackupTime = Get-Date -Format "yyyyMMdd-HHmmss"
  Copy-Item `
    "$env:USERPROFILE\.codex\config.toml" `
    "$env:USERPROFILE\.codex\config.toml.before-tokenhub.$tokenHubBackupTime"
}
```

如果原文件不存在，可以直接新建，不需要执行备份命令。

### 2. 写入全局配置

将以下内容合并到 `config.toml`：

```toml
model_provider = "tokenhub"
model = "填写 GET /v1/models 返回的实际模型 ID"

[model_providers.tokenhub]
name = "TokenHub"
base_url = "填写实际的 TokenHub Base URL"
env_key = "TOKENHUB_API_KEY"
env_key_instructions = "启动 Codex 前请设置 TOKENHUB_API_KEY"
wire_api = "responses"
```

如果文件中已经存在顶层 `model_provider`、`model` 或 `[model_providers.tokenhub]`，应修改原配置，不能再次追加同名键或同名表。

API Key 不要写进 `config.toml`。每次从终端启动 CLI 前，应通过第一节的安全输入方式设置 `TOKENHUB_API_KEY`，或使用组织批准的密钥管理工具向 Codex 进程注入该变量。

### 3. 启动与验证

```bash
codex doctor --summary
codex
```

进入 Codex 后可以使用 `/status` 检查当前模型和 Provider。还可以在 TokenHub 的 **请求日志** 中按时间、项目和模型确认请求已经进入网关。

### 4. 如何恢复

最稳妥的恢复方式，是用配置前创建的备份替换当前 `config.toml`。

macOS / Linux：

```bash
ls -1t "$HOME"/.codex/config.toml.before-tokenhub.*
cp -p "填写需要恢复的实际备份文件路径" \
  "$HOME/.codex/config.toml"
```

Windows PowerShell：

```powershell
Get-ChildItem "$env:USERPROFILE\.codex\config.toml.before-tokenhub.*" |
  Sort-Object LastWriteTime -Descending

Copy-Item `
  "填写需要恢复的实际备份文件路径" `
  "$env:USERPROFILE\.codex\config.toml"
```

如果只想手动恢复：

1. 将顶层 `model_provider` 和 `model` 改回原值；如果原文件没有这两项，则删除本次新增的两行。
2. 删除本次新增的整个 `[model_providers.tokenhub]` 配置块。
3. 完全退出 Codex，再重新启动。
4. 不再需要当前终端中的 Key 时，按方式 2 的恢复命令清除环境变量。

仅仅删除 `[model_providers.tokenhub]`，却保留 `model_provider = "tokenhub"`，会导致 Codex 找不到已选择的 Provider。

---

## 五、方式 4：在 Codex 桌面端中配置

Codex 桌面端、CLI 和 IDE 扩展共用 `~/.codex/config.toml`。因此，在桌面端中修改配置后，CLI 的默认环境也会一起改变。

### 1. 打开配置文件

在 Codex 中打开：

**Settings → Configuration → Open config.toml**

先备份原文件，再将方式 3 中的 TokenHub 配置块合并进去。不要重复声明同名 TOML 键或 `[model_providers.tokenhub]` 表。

### 2. 让桌面端读取 API Key

从 Dock、启动台或开始菜单启动的桌面应用，通常不会继承终端中的临时环境变量。要让本地桌面端读取自定义 Provider 的 Key，可以把下面一行合并到 `~/.codex/.env`，并填入真实项目 API Key：

```dotenv
TOKENHUB_API_KEY=填写真实的 TokenHub 项目 API Key
```

不要覆盖 `.env` 中已有的其他变量。macOS / Linux 建议限制文件权限：

```bash
chmod 600 "$HOME/.codex/.env"
```

该文件包含明文敏感凭证，不要上传、分享或提交到代码仓库。企业环境应优先遵循组织的凭证管理要求。

### 3. 重启与验证

完全退出 Codex 后重新打开，新建一个本地任务，再检查：

- 当前任务使用的模型是否为 TokenHub 返回的实际模型 ID；
- TokenHub 请求日志中是否出现对应请求；
- 请求返回的项目、模型和状态是否符合预期。

Codex 云端任务的默认模型不由本机 `config.toml` 控制，本文配置面向本地桌面端、CLI 和 IDE 扩展。

### 4. 如何恢复

1. 完全退出 Codex。
2. 恢复配置前备份的 `~/.codex/config.toml`；或者按方式 3 的手动恢复步骤移除 TokenHub 配置。
3. 如果本次在 `~/.codex/.env` 中新增了 `TOKENHUB_API_KEY`，只删除这一行，不要删除文件中的其他变量。
4. 重新启动 Codex。

---

## 六、常见错误

### 提示缺少 `TOKENHUB_API_KEY`

当前 Codex 进程没有读取到环境变量。不要输出 Key 本身，可以只检查变量是否存在。

macOS、Linux 或 Git Bash：

```bash
test -n "${TOKENHUB_API_KEY:-}" && echo "TOKENHUB_API_KEY 已注入"
```

Windows PowerShell：

```powershell
if ($env:TOKENHUB_API_KEY) { "TOKENHUB_API_KEY 已注入" }
```

如果是桌面端，检查 `~/.codex/.env` 后完全退出并重启应用。

### 返回 401

通常对应 `invalid_api_key`：请求没有携带项目 API Key、`Authorization` 格式错误，或者 Key 无法识别。确认使用的是 TokenHub **项目 API Key**，而不是控制台登录令牌。

### 返回 403

常见错误码包括 `api_key_disabled`、`api_key_expired` 和 `model_not_allowed`。检查项目状态、Key 是否启用或过期、模型白名单和调用策略。

### 返回 404

优先检查 Base URL 是否以 `/v1` 结尾，并确认模型 ID 来自当前 Key 的 `GET /v1/models` 响应。

### 返回 429

通常对应 `quota_exceeded`：项目或 Key 的请求数、Token、成本、并发额度，或者 Provider 资源限制已经触发。等待额度窗口恢复，或由管理员调整策略。

### 返回 503

通常对应 `provider_unavailable`：当前模型没有可用 Provider。请管理员确认模型存在启用且健康的路由，并检查 Provider 和账号资源状态。

### 返回 400 `responses_stream_unsupported`

所选路由不能提供 Codex 所需的流式 Responses API。请管理员调整模型路由或 Provider 资源；改用 Chat Completions 路由不能解决 Codex 客户端的协议要求。

### 修改后仍使用旧配置

完全退出当前 Codex 进程后重新启动，并检查启动命令是否带有 `--profile`、`--model` 或 `--config`。配置优先级从高到低为：

1. CLI 参数和 `--config`
2. 受信任项目中的 `.codex/config.toml`
3. `--profile` 选择的 Profile 文件
4. 用户级 `~/.codex/config.toml`
5. 系统配置
6. Codex 内置默认值

---

## 七、官方配置依据

- [Codex Config basics](https://learn.chatgpt.com/docs/config-file/config-basic)
- [Codex Advanced Configuration：Profile、单次覆盖与自定义 Provider](https://learn.chatgpt.com/docs/config-file/config-advanced)
- [Codex Environment variables](https://learn.chatgpt.com/docs/config-file/environment-variables)
- [Codex Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference)
