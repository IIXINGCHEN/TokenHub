# CLI リファレンス

## インストール

```bash
cd backend && go build -o tokenhub-migrate ./cmd/tokenhub-migrate/
```

## コマンド一覧

| コマンド | 説明 |
|--------|------|
| `extract litellm --from <yaml> --out <json>` | LiteLLM 設定から bundle を抽出 |
| `plan --bundle <json>` | Dry-run、作成/更新/スキップ数を表示 |
| `apply --bundle <json>` | 移行を実行 |
| `verify --bundle <json>` | 移行後の整合性を検証 |
| `rollback --checkpoint <json>` | 作成されたリソースをロールバック |

## 環境変数

| 変数 | 説明 |
|------|------|
| `TOKENHUB_API` | TokenHub Admin API URL（未設定時はローカル store） |
| `TOKENHUB_ADMIN_TOKEN` | Admin API 認証トークン |

完全なコマンド仕様は `docs/migration/cli.md` を参照してください。
