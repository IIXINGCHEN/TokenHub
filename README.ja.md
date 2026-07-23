<p align="center">
  <img src="frontend/public/brand/tokenhub-logo.png" alt="TokenHub" width="96" />
</p>

<h1 align="center">TokenHub</h1>

<p align="center">
  TokenHub は、ユーザー、チームリーダー、管理者のためのロール別ワークスペースを備えたエンタープライズ向けプライベート AI ゲートウェイです。
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
  <a href="README.md">English</a> | <a href="README.zh-CN.md">简体中文</a> | 日本語
</p>

## スクリーンショット

| ログインコンソール | ゲートウェイ概要 |
| --- | --- |
| ![Login Console](docs/assets/screenshots/login-en.png) | ![Gateway Overview](docs/assets/screenshots/overview-en.png) |
| API ドキュメント | Provider チャネル |
| ![API Documentation](docs/assets/screenshots/gateway-en.png) | ![Provider Channels](docs/assets/screenshots/providers-en.png) |
| モデルカタログ | ルーティングポリシー |
| ![Model Catalog](docs/assets/screenshots/models-en.png) | ![Routing Policies](docs/assets/screenshots/routes-en.png) |
| 利用分析 | システム設定 |
| ![Usage Analytics](docs/assets/screenshots/usage-en.png) | ![System Settings](docs/assets/screenshots/settings-en.png) |

## 3つのロールを中心に設計

TokenHub は、日常的なモデル利用、チームガバナンス、プラットフォーム運用を明確に分け、企業ユーザーが自分の責任に合ったワークフローへすぐ入れるようにします。

| ロール | ワークスペースの重点 | ガイド |
| --- | --- | --- |
| ユーザー | 利用可能なモデルの確認、プロジェクト Key の作成、モデル API の呼び出し、個人利用状況の確認 | [ユーザーガイド](docs/ja/user-guide.md) |
| チームリーダー | プロジェクトスペース、プロジェクトメンバー、プロジェクト Key、チームレポート、プロジェクト別コスト配賦の管理 | [チームリーダーガイド](docs/ja/team-leader-guide.md) |
| 管理者 | Provider、モデルカタログ、ルーティングポリシー、ID ソース、RBAC、監査、コスト制御の設定 | [管理者ガイド](docs/ja/administrator-guide.md) |

## プラットフォーム機能

- OpenAI-Compatible モデル API: `/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`。
- Provider チャネル: OpenAI-Compatible、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、ローカル vLLM/Ollama、カスタム上流。
- モデルカタログとルーティングポリシー: 優先度、重み、フェイルオーバー順序、ルートヘルス診断に対応。
- プロジェクト単位の Key 管理: チーム所有、メンバー権限、クォータ、並行数制限に対応。
- ユーザー、プロジェクト、チーム、モデル、コストセンターに紐づく利用分析とリクエストログ。
- OAuth/OIDC によるエンタープライズサインイン、RBAC、監査証跡に対応する ID ソース設定。
- クリーンなコンソール: ロール別ナビゲーション、グローバル検索、ライト/ダーク切り替え、左ナビ + 右詳細の API ドキュメント。
- SQLite-first のプライベートデプロイと Docker Compose サポート。
- コネクションプーリング対応の PostgreSQL による本番環境デプロイメントサポート。
- 管理コンソールは英語、中国語、日本語の切り替えに対応。

## マルチインスタンス構成

デフォルトのインストールでは、SQLite を使用するフロントエンド 1 台とバックエンド 1 台を起動します。水平スケールする場合は `deploy/docker-compose.remote-postgres.yml` を使用します。Nginx が複数のフロントエンドおよびバックエンドレプリカの統一エントリーポイントとなり、共有状態はリモート PostgreSQL に保存されます。複数のバックエンドレプリカで同じ SQLite ファイルを共有してはいけません。

<p align="center">
  <img src="docs/assets/architecture/tokenhub-multi-instance.png" alt="TokenHub マルチインスタンス構成" width="1200" />
</p>

マルチインスタンスモードでは：

- Nginx が管理コンソール、API、ヘルスチェックのトラフィックを正常なレプリカへ分散します。
- バックエンドレプリカは、永続設定、OAuth セッション、クォータカウンター、監査データ、クラスターロック、実行中リクエストの並行数リースを PostgreSQL で共有します。
- リースの期限と所有権は PostgreSQL のクロックで判定し、ホスト間の時刻ずれによる早期引き継ぎを防ぎます。所有権を失った処理はハートビートによってキャンセルされます。
- マウントされたモデルカタログはバックエンドの起動ごとに同期され、冪等な同期処理はクラスターロックによって直列化されます。
- データベースの調整障害では Provider の容量だけを解放し、正常なモデル Provider を誤って失敗扱いにしません。

すべてのバックエンドレプリカで同じ `TOKENHUB_SECRET_KEY` を使用してください。`TOKENHUB_DB_MAX_OPEN_CONNS` はレプリカ単位で設定し、接続プールの合計が PostgreSQL の上限を下回るようにします。

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.remote-postgres.yml up -d \
  --scale tokenhub-backend=3 \
  --scale tokenhub-frontend=2
```

設定要件、ヘルスプローブ、実際の PostgreSQL E2E テストについては、[デプロイガイド](docs/ja/deployment.md#リモート-postgresql-を使用するマルチインスタンス構成)を参照してください。

## クイックスタート

```bash
cp deploy/.env.example deploy/.env
# deploy/.env のすべての change-me 値を強いシークレットに置き換えます。
./deploy/install.sh
```

アクセス先:

- 管理コンソール: `http://localhost:3000`
- バックエンド API: `http://localhost:8080`
- ヘルスチェック: `http://localhost:8080/healthz`

初期管理者ログイン:

- ユーザー名: `admin`
- パスワード: `TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD` の設定値

デプロイスクリプトはビルド前に本番用認証情報を検証し、秘密値を表示せずに安全でない変数を個別に報告します。その試行で作成または再起動したバックエンドコンテナの異常により Compose が失敗した場合に限り、その試行で生成された直近のバックエンドログを自動表示します。

## ローカル開発

バックエンド:

```bash
cd backend
go run ./cmd/tokenhub
```

フロントエンド:

```bash
cd frontend
npm install
npm run dev
```

SDK サンプルでモデル API の疎通を確認できます。

```bash
cd sdk
npm install
npm run test:deepseek
```

## ドキュメント

- [ドキュメントホーム](docs/ja/README.md)
- [ユーザーガイド](docs/ja/user-guide.md)
- [チームリーダーガイド](docs/ja/team-leader-guide.md)
- [管理者ガイド](docs/ja/administrator-guide.md)
- [English documentation](docs/README.md)
- [简体中文文档](docs/zh-CN/README.md)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=astaxie%2FTokenHub&type=Date&legend=top-left)](https://www.star-history.com/?repos=astaxie%2FTokenHub&type=date&legend=top-left)

## License

TokenHub は [Apache License 2.0](LICENSE) の下で提供されています。
