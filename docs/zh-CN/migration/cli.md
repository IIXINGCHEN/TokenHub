# CLI 参考

## 安装

```bash
cd backend && go build -o tokenhub-migrate ./cmd/tokenhub-migrate/
```

## 命令概览

| 命令 | 说明 |
|------|------|
| `extract litellm --from <yaml> --out <json>` | 从 LiteLLM 配置提取 bundle |
| `plan --bundle <json>` | Dry-run，显示将会创建/更新/跳过的资源数 |
| `apply --bundle <json>` | 执行迁移 |
| `verify --bundle <json>` | 校验迁移后的一致性 |
| `rollback --checkpoint <json>` | 回滚已创建的资源 |

## 环境变量

| 变量 | 说明 |
|------|------|
| `TOKENHUB_API` | TokenHub Admin API 地址（默认用本地 store） |
| `TOKENHUB_ADMIN_TOKEN` | Admin API 认证 token |

完整命令用法请参阅 `docs/migration/cli.md`。
