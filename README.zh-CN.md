<p align="center">
  <img src="frontend/public/brand/tokenhub-logo.png" alt="TokenHub" width="96" />
</p>

<h1 align="center">TokenHub</h1>

<p align="center">
  TokenHub 让企业通过一个私有化网关统一接入和治理 AI 模型，让每一次调用都可控、可追踪、可归因。
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker Compose" />
  <img src="https://img.shields.io/badge/OpenAI-Compatible-10A37F" alt="OpenAI Compatible" />
</p>

<p align="center">
  <a href="README.md">English</a> | 简体中文 | <a href="README.ja.md">日本語</a>
</p>

## 企业级 Token 治理

企业接入 AI 模型时，难点通常不只是“能不能调通某个模型 API”。真正麻烦的是：如何把 Token 安全分发给不同团队而不泄露 Provider 密钥，如何在上游故障或价格变化时把请求路由到合适模型，如何把 Provider 账单和内部项目、团队、成本中心对齐。

TokenHub 将这些控制能力放到每一次模型调用之前：

- 按项目发放 Key，让团队获得可用权限，而不是直接复制 Provider 密钥。
- 通过统一策略管理模型渠道的优先级、权重、失败回退和健康诊断。
- 用量统计和请求日志可归因到用户、项目、团队、模型和成本中心。
- RBAC、OAuth/OIDC 身份源、审计追踪、额度和并发限制让私有化 AI 访问真正可治理。

## 为什么选择 TokenHub

许多开源 AI 网关主要解决的是 Provider 聚合：用一个接口调用多个上游。这对开发者接入模型很有帮助，但单靠它并不能解决企业运营里的治理问题。TokenHub 关注的正是这层缺失的治理能力：

- Token 分发按项目和团队管理，不需要把原始 Provider Key 散落到每个应用里。
- 模型访问、路由和失败回退由管理员通过策略统一调整，不必改客户端代码。
- 账单和请求记录可以回到内部归属关系中，帮助财务、平台和业务团队解释 AI 成本。
- 普通用户、团队负责人和管理员拥有分离的工作台，让日常调用、审批、成本归因和平台运维各归其位。

## 产品截图

<p align="center">
  <img src="docs/assets/screenshots/tokenhub-tour.webp" alt="TokenHub 产品导览：登录、概览、接口文档、Provider 渠道、模型目录、路由策略、用量统计和系统设置" width="100%">
</p>

## 围绕三大角色设计

TokenHub 将日常模型使用、团队治理和平台运维拆成清晰的角色入口，让企业用户只看到和自己职责相关的工作流。

| 角色 | 工作台重点 | 指南 |
| --- | --- | --- |
| 普通用户 | 查看可用模型、创建项目 Key、调用模型 API、查看个人用量 | [普通用户指南](docs/zh-CN/user-guide.md) |
| 团队负责人 | 管理项目空间、项目成员、项目 Key、团队报表和项目成本归因 | [团队负责人指南](docs/zh-CN/team-leader-guide.md) |
| 平台管理员 | 配置 Provider、模型目录、路由策略、身份源、RBAC、审计和成本管控 | [管理员指南](docs/zh-CN/administrator-guide.md) |

## 平台能力

- 按项目归属的 Key 管理：支持团队归属、成员权限、额度和并发限制。
- 模型目录和路由策略：支持优先级、权重、失败回退顺序和路由健康诊断。
- 用量统计和请求日志：可归因到用户、项目、团队、模型和成本中心。
- 身份源配置：支持 OAuth/OIDC 企业登录，并配合 RBAC 和审计追踪。
- OpenAI-Compatible 模型 API：`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`；Anthropic Messages API：`/v1/messages`、`/v1/messages/count_tokens`。
- OpenAI-Compatible 生图与参考图编辑 API：`/v1/images/generations`、`/v1/images/edits`，支持异步任务和服务端图片留存。
- 简洁控制台：分角色导航、全局搜索、黑白主题，以及左侧 API 导航 + 右侧详情的接口文档。
- SQLite-first 私有化部署，提供原生 systemd 和 Docker Compose 两种方式。
- PostgreSQL 支持多实例部署：通过远端 PostgreSQL 共享状态，实现前后端实例横向扩展，并提供连接池配置。参见[部署指南](docs/zh-CN/deployment.md)。
- 管理后台支持英文、中文、日文切换。

## Provider 生态

多 Provider 支持只是 TokenHub 的其中一层能力，不是项目的核心卖点。当前面的企业级 Token 治理、路由、归因和审计机制就位之后，TokenHub 才把这些受控工作流连接到 OpenAI、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、Codex 订阅、本地模型和自定义 OpenAI-Compatible 上游。

TokenHub 原生适配 OpenAI、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、Codex 订阅和本地模型，并内置 150+ Provider 模板。常用接入包括：

<p align="center">
  <img src="docs/assets/provider-showcase.svg" alt="TokenHub 常用 Provider 接入，覆盖商业模型、订阅模型、本地模型和自定义上游。" width="100%">
</p>

Provider 模板会优先使用对应的原生适配器，其余模板通过 OpenAI-Compatible 接口接入；实际可用模型和能力以对应上游服务及账号权限为准。

## 快速开始

Linux systemd 主机使用原生 Release：

```bash
curl -fsSL https://raw.githubusercontent.com/astaxie/TokenHub/main/deploy/native/install.sh \
  -o /tmp/tokenhub-install.sh
sudo bash /tmp/tokenhub-install.sh install
```

从仓库检出目录使用 Docker Compose：

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
- 原生安装密码：由安装脚本输出一次
- Docker 密码：`TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD` 的配置值

原生安装脚本会校验 Release 文件、安装 systemd 服务，并在版本面板提供直接更新、回退和重启。默认 Docker 部署使用一个托管容器同时运行后端与管理后台，无需挂载 Docker Socket，也提供相同的直接操作。Release 完整包保存在 `tokenhub-releases` 卷中，普通重启或重建容器不会丢失页面更新结果。多实例 Docker 部署仍由运维人员通过 Compose 统一更新，避免不同副本版本不一致。完整说明见[部署指南](docs/zh-CN/deployment.md)。

## 文档

- [文档首页](docs/zh-CN/README.md)
- [整体架构](docs/zh-CN/architecture.md)
- [普通用户指南](docs/zh-CN/user-guide.md)
- [团队负责人指南](docs/zh-CN/team-leader-guide.md)
- [管理员指南](docs/zh-CN/administrator-guide.md)
- [贡献指南](CONTRIBUTING.zh-CN.md)

## Contributors

TokenHub 的演进离不开真实企业场景里的使用反馈、网关集成、文档完善、测试补充和持续维护。感谢每一位让项目变得更可靠的人。

<!-- readme: contributors -start -->

<table>
  <tr>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/astaxie">
        <img src="https://avatars.githubusercontent.com/u/233907?v=4" width="80px" alt="astaxie" />
        <br /><sub><b>astaxie</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/deepjerry-ai">
        <img src="https://avatars.githubusercontent.com/u/262369278?v=4" width="80px" alt="deepjerry-ai" />
        <br /><sub><b>deepjerry-ai</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/legendtkl">
        <img src="https://avatars.githubusercontent.com/u/2370761?v=4" width="80px" alt="legendtkl" />
        <br /><sub><b>legendtkl</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/Mr0bean">
        <img src="https://avatars.githubusercontent.com/u/19573968?v=4" width="80px" alt="Mr0bean" />
        <br /><sub><b>Mr0bean</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/cngump">
        <img src="https://avatars.githubusercontent.com/u/108251?v=4" width="80px" alt="cngump" />
        <br /><sub><b>cngump</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/bailu-ZZ">
        <img src="https://avatars.githubusercontent.com/u/311096537?v=4" width="80px" alt="bailu-ZZ" />
        <br /><sub><b>bailu-ZZ</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/coldbrewtea">
        <img src="https://avatars.githubusercontent.com/u/6879314?v=4" width="80px" alt="coldbrewtea" />
        <br /><sub><b>coldbrewtea</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/samz406">
        <img src="https://avatars.githubusercontent.com/u/3055810?v=4" width="80px" alt="samz406" />
        <br /><sub><b>samz406</b></sub>
      </a>
    </td>
  </tr>
  <tr>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/wangle201210">
        <img src="https://avatars.githubusercontent.com/u/19949348?v=4" width="80px" alt="wangle201210" />
        <br /><sub><b>wangle201210</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/imaben">
        <img src="https://avatars.githubusercontent.com/u/3390195?v=4" width="80px" alt="imaben" />
        <br /><sub><b>imaben</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/CLukeLi">
        <img src="https://avatars.githubusercontent.com/u/252523101?v=4" width="80px" alt="CLukeLi" />
        <br /><sub><b>CLukeLi</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/myssl">
        <img src="https://avatars.githubusercontent.com/u/27838738?v=4" width="80px" alt="myssl" />
        <br /><sub><b>myssl</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/exgliuzhi">
        <img src="https://avatars.githubusercontent.com/u/6261701?v=4" width="80px" alt="exgliuzhi" />
        <br /><sub><b>exgliuzhi</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/hoorayman">
        <img src="https://avatars.githubusercontent.com/u/73151874?v=4" width="80px" alt="hoorayman" />
        <br /><sub><b>hoorayman</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/debin-ge">
        <img src="https://avatars.githubusercontent.com/u/21329997?v=4" width="80px" alt="debin-ge" />
        <br /><sub><b>debin-ge</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/ocass-chen">
        <img src="https://avatars.githubusercontent.com/u/172055494?v=4" width="80px" alt="ocass-chen" />
        <br /><sub><b>ocass-chen</b></sub>
      </a>
    </td>
  </tr>
  <tr>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/AnxForever">
        <img src="https://avatars.githubusercontent.com/u/130662349?v=4" width="80px" alt="AnxForever" />
        <br /><sub><b>AnxForever</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/yujiewanwan">
        <img src="https://avatars.githubusercontent.com/u/268286250?v=4" width="80px" alt="yujiewanwan" />
        <br /><sub><b>yujiewanwan</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/lxm">
        <img src="https://avatars.githubusercontent.com/u/1918195?v=4" width="80px" alt="lxm" />
        <br /><sub><b>lxm</b></sub>
      </a>
    </td>
    <td align="center" valign="top" width="12.5%">
      <a href="https://github.com/susunola">
        <img src="https://avatars.githubusercontent.com/u/38539169?v=4" width="80px" alt="susunola" />
        <br /><sub><b>susunola</b></sub>
      </a>
    </td>
  </tr>
</table>

<!-- readme: contributors -end -->

<p align="center">
  <a href="https://github.com/astaxie/TokenHub/graphs/contributors">查看全部贡献者</a>
  ·
  <a href="CONTRIBUTING.zh-CN.md">参与贡献</a>
</p>

## Star History

<a href="https://www.star-history.com/?repos=astaxie%2Ftokenhub&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=astaxie/tokenhub&type=date&theme=dark&legend=top-left&sealed_token=hWH3kDnssTf49CCLxzq3NVqEp0WTL-HFhsdpQJJz1DUuZt0D-nu1jgXLnhCxrUrMYujv6IJJk12B1wCp5qiU2bU_J03ECSYvb3Y9Pv-gqX7RuwS4SehRrQ" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=astaxie/tokenhub&type=date&legend=top-left&sealed_token=hWH3kDnssTf49CCLxzq3NVqEp0WTL-HFhsdpQJJz1DUuZt0D-nu1jgXLnhCxrUrMYujv6IJJk12B1wCp5qiU2bU_J03ECSYvb3Y9Pv-gqX7RuwS4SehRrQ" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=astaxie/tokenhub&type=date&legend=top-left&sealed_token=hWH3kDnssTf49CCLxzq3NVqEp0WTL-HFhsdpQJJz1DUuZt0D-nu1jgXLnhCxrUrMYujv6IJJk12B1wCp5qiU2bU_J03ECSYvb3Y9Pv-gqX7RuwS4SehRrQ" />
 </picture>
</a>

## License

TokenHub 采用 [Apache License 2.0](LICENSE) 协议。
