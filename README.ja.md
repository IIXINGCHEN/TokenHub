<p align="center">
  <img src="frontend/public/brand/tokenhub-logo.png" alt="TokenHub" width="96" />
</p>

<h1 align="center">TokenHub</h1>

<p align="center">
  TokenHub は、企業の AI モデル接続とガバナンスを一元化し、すべてのリクエストを制御・追跡し、利用主体を特定できるプライベートゲートウェイです。
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker Compose" />
  <img src="https://img.shields.io/badge/OpenAI-Compatible-10A37F" alt="OpenAI Compatible" />
</p>

<p align="center">
  <a href="README.md">English</a> | <a href="README.zh-CN.md">简体中文</a> | 日本語
</p>

## エンタープライズ Token ガバナンス

企業が AI モデルを導入するときの課題は、単に特定のモデル API を呼び出せるかどうかだけではありません。Provider の認証情報を露出せずに各チームへ Token を配布すること、上流障害や価格変動に応じて適切なモデルへルーティングすること、Provider の請求を社内のプロジェクト、チーム、コストセンターと照合することが難しくなります。

TokenHub は、これらの制御をすべてのモデル呼び出しの手前に置きます。

- プロジェクト単位の Key により、Provider のシークレットを渡さずにチームへ利用権限を付与できます。
- ルーティングポリシーでモデルチャネルの優先度、重み、フェイルオーバー、ヘルス診断を一元管理できます。
- 利用分析とリクエストログを、ユーザー、プロジェクト、チーム、モデル、コストセンターへ紐づけられます。
- RBAC、OAuth/OIDC ID ソース、監査証跡、クォータ、並行数制限により、プライベート環境の AI アクセスを管理可能にします。

## TokenHub が選ばれる理由

多くのオープンソース AI ゲートウェイは Provider の集約に重点を置いています。つまり、1 つのエンドポイントから複数の上流を呼び出す仕組みです。これは開発者のモデル接続には役立ちますが、企業運用の課題をそれだけで解決できるわけではありません。TokenHub は、その不足しているガバナンス層を中心に設計されています。

- Token 配布をプロジェクトとチームで管理し、生の Provider Key を各アプリケーションへ散在させません。
- モデルアクセス、ルーティング、フェイルオーバーを管理者がポリシーとして変更でき、クライアントコードの変更を抑えられます。
- 請求とリクエスト履歴を社内の所有関係へ紐づけ、財務、プラットフォーム、事業チームが AI コストを説明できます。
- ユーザー、チームリーダー、管理者のワークスペースを分け、日常利用、承認、コスト配賦、プラットフォーム運用を責任ごとに整理します。

## スクリーンショット

<p align="center">
  <img src="docs/assets/screenshots/tokenhub-tour.webp" alt="TokenHub 製品ツアー：ログイン、概要、API ドキュメント、Provider チャネル、モデルカタログ、ルーティングポリシー、利用分析、システム設定" width="100%">
</p>

## 3つのロールを中心に設計

TokenHub は、日常的なモデル利用、チームガバナンス、プラットフォーム運用を明確に分け、企業ユーザーが自分の責任に合ったワークフローへすぐ入れるようにします。

| ロール | ワークスペースの重点 | ガイド |
| --- | --- | --- |
| ユーザー | 利用可能なモデルの確認、プロジェクト Key の作成、モデル API の呼び出し、個人利用状況の確認 | [ユーザーガイド](docs/ja/user-guide.md) |
| チームリーダー | プロジェクトスペース、プロジェクトメンバー、プロジェクト Key、チームレポート、プロジェクト別コスト配賦の管理 | [チームリーダーガイド](docs/ja/team-leader-guide.md) |
| 管理者 | Provider、モデルカタログ、ルーティングポリシー、ID ソース、RBAC、監査、コスト制御の設定 | [管理者ガイド](docs/ja/administrator-guide.md) |

## プラットフォーム機能

- プロジェクト単位の Key 管理: チーム所有、メンバー権限、クォータ、並行数制限に対応。
- モデルカタログとルーティングポリシー: 優先度、重み、フェイルオーバー順序、ルートヘルス診断に対応。
- ユーザー、プロジェクト、チーム、モデル、コストセンターに紐づく利用分析とリクエストログ。
- OAuth/OIDC によるエンタープライズサインイン、RBAC、監査証跡に対応する ID ソース設定。
- OpenAI-Compatible モデル API: `/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`。Anthropic Messages API: `/v1/messages`、`/v1/messages/count_tokens`。
- OpenAI-Compatible の画像生成および参照画像編集 API: `/v1/images/generations`、`/v1/images/edits`。非同期ジョブとサーバー側の画像保持に対応します。
- クリーンなコンソール: ロール別ナビゲーション、グローバル検索、ライト/ダーク切り替え、左ナビ + 右詳細の API ドキュメント。
- SQLite-first のプライベートデプロイ向けに、ネイティブ systemd と Docker Compose の両方をサポート。
- PostgreSQL はマルチインスタンス構成に対応します。リモート PostgreSQL で状態を共有し、フロントエンドとバックエンドのレプリカを水平スケールできるほか、コネクションプールも設定できます。[デプロイガイド](docs/ja/deployment.md)を参照してください。
- 管理コンソールは英語、中国語、日本語の切り替えに対応。

## Provider エコシステム

複数 Provider への対応は TokenHub の一機能であり、中心的な約束ではありません。エンタープライズ Token ガバナンス、ルーティング、利用主体の特定、監査制御を先に整えたうえで、TokenHub はその管理されたワークフローを OpenAI、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、Codex サブスクリプション、ローカルモデル、カスタム OpenAI-Compatible 上流へ接続します。

TokenHub は、OpenAI、Azure OpenAI、Anthropic、Gemini、DeepSeek、Qwen、Codex サブスクリプション、ローカルモデル向けのネイティブアダプターに加え、150 以上の Provider テンプレートを備えています。主な接続先：

<p align="center">
  <img src="docs/assets/provider-showcase.svg" alt="商用モデル、サブスクリプションモデル、ローカルモデル、カスタム上流を含む TokenHub の主な Provider 接続先。" width="100%">
</p>

Provider テンプレートは、利用可能な場合は対応するネイティブアダプターを使用し、それ以外は OpenAI-Compatible エンドポイントへ接続します。利用可能なモデルと機能は、上流サービスおよびアカウントによって異なります。

## クイックスタート

Linux systemd ホストでネイティブ Release を使用する場合:

```bash
curl -fsSL https://raw.githubusercontent.com/astaxie/TokenHub/main/deploy/native/install.sh \
  -o /tmp/tokenhub-install.sh
sudo bash /tmp/tokenhub-install.sh install
```

リポジトリのチェックアウトから Docker Compose を使用する場合:

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
- ネイティブインストールのパスワード: インストーラーが一度だけ表示
- Docker のパスワード: `TOKENHUB_BOOTSTRAP_ADMIN_PASSWORD` の設定値

ネイティブインストーラーは Release のチェックサムを検証し、systemd サービスをインストールして、バージョンパネルから直接更新、ロールバック、再起動できるようにします。デフォルトの Docker デプロイは、バックエンドと管理コンソールを 1 つの管理対象コンテナで実行し、Docker Socket をマウントせずに同じ直接操作を提供します。Release バンドルは `tokenhub-releases` ボリュームへ保存されるため、通常のコンテナ再起動や再作成でも画面から適用した更新は保持されます。マルチインスタンス Docker では、全レプリカを同時に切り替えるため Compose による運用更新を維持します。詳細は[デプロイガイド](docs/ja/deployment.md)を参照してください。

## ドキュメント

- [ドキュメントホーム](docs/ja/README.md)
- [全体アーキテクチャ](docs/ja/architecture.md)
- [ユーザーガイド](docs/ja/user-guide.md)
- [チームリーダーガイド](docs/ja/team-leader-guide.md)
- [管理者ガイド](docs/ja/administrator-guide.md)
- [コントリビューションガイド](CONTRIBUTING.ja.md)

## Contributors

TokenHub は、実際のエンタープライズ利用からのフィードバック、ゲートウェイ連携、ドキュメント、テスト、継続的なメンテナンスによって育っています。プロジェクトをより信頼できるものにしてくれるすべての方に感謝します。

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
  <a href="https://github.com/astaxie/TokenHub/graphs/contributors">すべてのコントリビューターを見る</a>
  ·
  <a href="CONTRIBUTING.ja.md">コントリビュートを始める</a>
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

TokenHub は [Apache License 2.0](LICENSE) の下で提供されています。
