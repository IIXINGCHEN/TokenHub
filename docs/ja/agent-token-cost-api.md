# Agent Token コスト API

Language: [English](../agent-token-cost-api.md) | [简体中文](../zh-CN/agent-token-cost-api.md) | 日本語

バージョン化された Agent Token コスト API を使うと、ローカルのレポート/監視 Agent は、管理者セッション、モデル呼び出し用 API Key、Provider 認証情報、手動エクスポートなしで TokenHub の利用量を読み取れます。この API は読み取り専用で、管理者の利用量画面と同じリクエスト数、Token 数、エラー数、推定顧客コストを使用します。

## エンドポイント

| エンドポイント | 認証 | 用途 |
| --- | --- | --- |
| `GET /api/v1/analytics/token-costs` | 分析 Credential | リクエスト単位または集計済み Token コストを JSON/CSV で取得 |
| `GET /api/admin/analytics/credentials` | プラットフォーム管理者 | 分析 Credential のメタデータを一覧表示 |
| `POST /api/admin/analytics/credentials` | プラットフォーム管理者 | 分析 Credential を作成し、Token を一度だけ表示 |
| `DELETE /api/admin/analytics/credentials/{id}` | プラットフォーム管理者 | 分析 Credential を即時失効 |

分析 Credential は `tha_` で始まり、`/v1/models`、モデル推論エンドポイント、管理者エンドポイントの認証には使えません。

## 最小権限 Credential の作成

管理者セッションまたは設定済み管理者 Token で Project スコープの Credential を作成します。

```bash
curl -sS https://tokenhub.example.com/api/admin/analytics/credentials \
  -H "Authorization: Bearer $TOKENHUB_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "payments-cost-agent",
    "scope_type": "project",
    "project_id": "prj_payments",
    "expires_at": "2026-12-31T00:00:00Z"
  }'
```

レスポンスには `credential` メタデータと `token` が含まれます。Token は直ちにコピーしてください。以後の一覧には prefix と suffix だけが表示されます。Agent が TokenHub インスタンス全体を読む必要がある場合に限り、`scope_type` を `organization` にして `project_id` を省略します。有効期限は任意ですが、設定を推奨します。

Token をローカル Agent の Secret ストアに保存します。

```bash
export TOKENHUB_ANALYTICS_TOKEN='tha_REPLACE_ME'
```

Agent を廃止した場合や Token 漏えいの可能性がある場合は失効させます。

```bash
curl -sS -X DELETE \
  https://tokenhub.example.com/api/admin/analytics/credentials/acred_REPLACE_ME \
  -H "Authorization: Bearer $TOKENHUB_ADMIN_TOKEN"
```

## リクエスト単位コストの取得

`from` は含み、`to` は含まない RFC 3339 時刻です。省略した場合は、クエリ開始時点までの直近 24 時間を取得します。

```bash
curl -sS -G https://tokenhub.example.com/api/v1/analytics/token-costs \
  -H "Authorization: Bearer $TOKENHUB_ANALYTICS_TOKEN" \
  --data-urlencode 'from=2026-08-01T00:00:00Z' \
  --data-urlencode 'to=2026-08-02T00:00:00Z' \
  --data-urlencode 'provider_id=prv_openai' \
  --data-urlencode 'model=gpt-4.1-mini' \
  --data-urlencode 'status=success' \
  --data-urlencode 'limit=100'
```

既定の `granularity=request` は、ゲートウェイリクエストごとにサニタイズ済みの 1 行を返します。安定した ID とメトリクスは含まれますが、提示された分析 Token、API Key Secret、Provider 認証情報、Provider コスト、クライアント IP、リクエスト/レスポンス Payload、User-Agent は含まれません。

## フィルターと集計

次の例は、Project、Provider、モデル、成功/エラー状態ごとの日次合計を返します。

```bash
curl -sS -G https://tokenhub.example.com/api/v1/analytics/token-costs \
  -H "Authorization: Bearer $TOKENHUB_ANALYTICS_TOKEN" \
  --data-urlencode 'from=2026-07-01T00:00:00Z' \
  --data-urlencode 'to=2026-08-01T00:00:00Z' \
  --data-urlencode 'granularity=day' \
  --data-urlencode 'group_by=project,provider,model,status'
```

時間バケットには `hour`、`day`、`month` を使います。時間バケットなしの合計には `granularity=none` を使います。`group_by` だけを指定した場合も `none` になります。`request` と `group_by` は併用できません。

| パラメーター | 値と動作 |
| --- | --- |
| `from`、`to` | RFC 3339 の `[from, to)` 区間。既定は直近 24 時間 |
| `project_id` | 完全一致する Project ID。Project Credential は常に自 Project に限定され、別 Project の指定には `403` を返す |
| `user_id` | 完全一致する利用帰属ユーザー ID |
| `api_key_id` | 保存済み API Key ID。API Key Secret ではない |
| `provider_id` | 完全一致する Provider ID |
| `model` | 完全一致する外部モデル名 |
| `status` | HTTP status が 400 未満なら `success`、400 以上なら `error` |
| `granularity` | `request`（既定）、`none`、`hour`、`day`、`month` |
| `group_by` | カンマ区切りまたは繰り返し指定する `project`、`user`、`api_key`、`provider`、`model`、`status` |
| `limit` | 1～1000 行。既定は 100 |
| `cursor` | 直前ページの不透明な `next_cursor` |
| `after` | 新しい差分取得に使う、コミット済みの不透明な `watermark`。`from`、`cursor` と併用不可 |
| `format` | `json`（既定）または `csv`。`Accept: text/csv` でも CSV を選択 |

リクエスト単位の期間上限は 31 日、集計クエリは 366 日です。それより長い履歴は連続する期間に分割してください。

## JSON Schema

すべての JSON レスポンスは `schema_version: "1.0"` を宣言し、次の形になります。

```json
{
  "schema_version": "1.0",
  "object": "token_cost.list",
  "generated_at": "2026-08-02T00:00:01Z",
  "query": {
    "from": "2026-08-01T00:00:00Z",
    "to": "2026-08-02T00:00:00Z",
    "granularity": "day",
    "group_by": ["project", "model"],
    "filters": {"project_id": "prj_payments"},
    "format": "json",
    "limit": 100
  },
  "data": [
    {
      "bucket": "2026-08-01",
      "project_id": "prj_payments",
      "model": "gpt-4.1-mini",
      "metrics": {
        "request_count": 42,
        "error_count": 2,
        "input_tokens": 120000,
        "cached_input_tokens": 35000,
        "cache_write_input_tokens": 4000,
        "output_tokens": 18000,
        "reasoning_output_tokens": 2500,
        "total_tokens": 138000,
        "estimated_cost_usd": 1.73
      }
    }
  ],
  "has_more": false,
  "watermark": "OPAQUE_WATERMARK"
}
```

`request_count` と `error_count` はゲートウェイのリクエストログから得られるため、利用量行を持たない失敗リクエストも含みます。Token とコストは利用量レコードから得ます。キャッシュと推論 Token は入力/出力合計に含まれている明細なので、すべての明細を `total_tokens` に再加算しないでください。`estimated_cost_usd` は管理者の利用量画面と同じ外部顧客向け推定課金額であり、機密の Provider コストではありません。

## ページングと差分取得

`has_more` が true の場合は `cursor=next_cursor` で再度呼び出します。同じフィルター、`granularity`、`group_by` を繰り返してください。Cursor が元のスナップショット期間を保持するため、`from` と `to` は省略できます。クエリ形状を変えると Cursor は拒否されます。スナップショット上限は固定されるため、ページング中に到着したリクエストで後続ページがずれることはありません。

レスポンスの `watermark` は、そのスナップショットで最後に一致したリクエストを指します。`has_more` が false になるまですべてのページを処理し、処理成功後にだけ watermark を Agent の永続状態へコミットしてください。次回は `after=<committed watermark>` を使います。

```bash
curl -sS -G https://tokenhub.example.com/api/v1/analytics/token-costs \
  -H "Authorization: Bearer $TOKENHUB_ANALYTICS_TOKEN" \
  --data-urlencode "after=$TOKENHUB_COST_WATERMARK"
```

`after` の位置は含みません。新規レコードがない場合、TokenHub は空の `data` とコミット済み watermark をそのまま返すため、Agent はチェックポイントを失いません。

## CSV エクスポート

```bash
curl -sS -G https://tokenhub.example.com/api/v1/analytics/token-costs \
  -H "Authorization: Bearer $TOKENHUB_ANALYTICS_TOKEN" \
  -H 'Accept: text/csv' \
  --data-urlencode 'from=2026-08-01T00:00:00Z' \
  --data-urlencode 'to=2026-08-02T00:00:00Z' \
  --data-urlencode 'granularity=hour' \
  --data-urlencode 'group_by=project,provider,model' \
  -o token-costs.csv
```

CSV は JSON と同じフィルター、上限、メトリクス、ページング規則を使います。メタデータは `X-TokenHub-Schema-Version`、`X-TokenHub-Has-More`、`X-TokenHub-Next-Cursor`、`X-TokenHub-Watermark` ヘッダーで返します。

## セキュリティと運用

- 分析 Credential を作成、一覧表示、失効できるのはプラットフォーム管理者だけです。
- 成功したクエリ、スコープ違反、不正なクエリ、無効 Credential の試行は、すべて `token_cost_analytics` 監査イベントとして記録されます。
- 分析 Credential とその Hash はクエリレスポンスおよび監査スナップショットから除外されます。一度だけ表示される Token は Secret として保存してください。
- クエリは `created_at` と `(project_id, created_at)` の Index を使い、期間とページサイズで制限されます。無制限の履歴取得を並列実行せず、ページングしてください。
- 失効は次回リクエストから有効です。ローテーションは、代替 Credential の作成、Agent 更新、旧 Credential の失効の順で行います。
- 安定した HTTP/CSV 契約が、現在サポートされるローカル Agent インターフェースです。将来の CLI/MCP Adapter は、管理者権限やモデル呼び出し権限なしでこの API をラップできます。
