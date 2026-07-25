# E2E テスト

移行フレームワークは Docker Compose ベースの E2E テストを提供し、extract → plan → apply → verify → rollback のフロー全体を検証します。

## 実行方法

```bash
# サービス起動
docker compose -f deploy/docker-compose.migration-e2e.yml up -d --wait

# テスト実行
cd sdk/migration-e2e && npm ci && npm run test:litellm

# クリーンアップ
docker compose -f deploy/docker-compose.migration-e2e.yml down -v
```

## CI トリガー

CI では `migration:e2e` ラベルを付与することで E2E テストが実行されます。

詳細は `docs/migration/e2e.md` を参照してください。
