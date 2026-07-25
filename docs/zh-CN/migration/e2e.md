# E2E 测试

迁移框架提供了基于 Docker Compose 的端到端测试，验证完整的 extract → plan → apply → verify → rollback 流程。

## 运行方式

```bash
# 启动服务
docker compose -f deploy/docker-compose.migration-e2e.yml up -d --wait

# 执行测试
cd sdk/migration-e2e && npm ci && npm run test:litellm

# 清理
docker compose -f deploy/docker-compose.migration-e2e.yml down -v
```

## CI 触发

E2E 测试在 CI 中通过添加 `migration:e2e` 标签触发。

详细说明请参阅 `docs/migration/e2e.md`。
