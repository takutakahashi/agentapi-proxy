# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## コマンド

```bash
make build          # バイナリのビルド (bin/agentapi-proxy)
make test           # テスト実行 (CGO_ENABLED=1, race detector 有効)
make test-verbose PKG=./internal/app  # 特定パッケージを詳細出力でテスト
make lint           # golangci-lint 実行 (gofmt も含む)
make gofmt          # コードフォーマット
make install-deps   # Go モジュールと golangci-lint のインストール
make ci             # lint + test + build をまとめて実行
make devbuild       # GitHub Actions でdev用 Docker image + Helm chart をビルド
```

テスト実行には `CGO_ENABLED=1` が必要（race detector のため）。

## アーキテクチャ概要

### 全体像

agentapi-proxy は [coder/agentapi](https://github.com/coder/agentapi) のプロセスマネージャ兼リバースプロキシ。各セッションで独立した agentapi サーバプロセスを起動し、`/:sessionId/*` へのリクエストをそのセッションのバックエンドに転送する。

### レイヤー構成 (Clean Architecture)

```
internal/
  domain/         # エンティティ・インターフェース定義 (外部依存なし)
    entities/     # Session, Task, Memory, Schedule など
    services/     # EncryptionService インターフェース
  usecases/       # ビジネスロジック
    ports/        # repositories/ (SessionManager, SettingsRepository など Port インターフェース)
    session/      # セッション検証 UseCase
    auth/         # 認証 UseCase
    notification/ # 通知 UseCase
  infrastructure/ # 実装詳細
    repositories/ # Kubernetes CRD/Secret/ConfigMap を使った実装、S3/メモリ実装
    services/     # KubernetesSessionManager, SimpleAuthService など
  interfaces/
    controllers/  # Echo ハンドラ (各エンドポイントのビジネスロジック呼び出し)
    presenters/   # レスポンス整形
  app/            # Server と Router の組み立て (DI)
  di/             # DI コンテナ
pkg/              # 外部パッケージから使われる共通ライブラリ
  auth/           # 認証・認可 (API Key, GitHub OAuth, JWT, Team mapping)
  config/         # 設定読み込み (YAML + 環境変数)
  client/         # agentapi-proxy Go クライアント
  notification/   # WebPush 通知サービス
  sessionsettings # セッション設定ユーティリティ
cmd/              # cobra CLI コマンド定義
spec/             # OpenAPI 仕様 (spec/openapi.json) + 静的ファイル埋め込み
```

### セッション管理フロー

1. `POST /start` → `SessionController.StartSession` → `Server.CreateSession` → `KubernetesSessionManager.CreateSession`
2. Kubernetes Deployment + Service + PVC を作成して agentapi プロセスを起動
3. `GET /search` でセッション一覧を取得
4. `ANY /:sessionId/*` → `SessionController.RouteToSession` → バックエンドへプロキシ転送（SSE ストリームも pass-through）
5. `DELETE /sessions/:sessionId` でセッション削除

### 実行モード

| モード | SessionManager 実装 | 説明 |
|--------|-------------------|------|
| Kubernetes | `KubernetesSessionManager` | 本番。K8s Deployment でセッションを管理 |
| Local (acp_server) | `LocalSessionManager` | ローカル開発。プロセス直接起動 |
| External Session Manager | 親プロキシ → External Session Manager | `ManagerID` 指定時に別の agentapi-proxy インスタンスに転送 |

### 認証・認可

- `pkg/auth/` にて API Key / GitHub OAuth / Personal API Key (K8s Secret) / Service Account の4種類をサポート
- `entities.Permission` で `session:read` / `session:create` / `admin` を定義
- 各ルートに `auth.RequirePermission(...)` ミドルウェアを付与

### ルーティング登録

`internal/app/router.go` の `Router.RegisterRoutes()` が起点。リポジトリ/コントローラが nil の場合（K8s 外での起動時など）はそのルートを登録しない条件分岐がある。カスタムハンドラは `CustomHandler` インターフェースを実装して `Router.AddCustomHandler()` で追加。

### OpenAPI 仕様

`spec/openapi.json` が正規の API ドキュメント。API を変更した際はこのファイルも必ず更新する。`spec/static.go` で `//go:embed` により実行バイナリに埋め込まれ `/public/*` として配信される。

## 前提条件・開発ワークフロー

### API 仕様の参照

- **作業開始前に [agentapi の OpenAPI 仕様](https://github.com/coder/agentapi/blob/main/openapi.json) を必ず参照する**
- 必要に応じて [agentapi のコード](https://github.com/coder/agentapi) を参照する

### 開発ワークフロー

- **絶対に main ブランチに直接プッシュしてはいけない** — 必ず feature ブランチを切って作業し、PR を作成する
- **API の変更時は必ず `spec/openapi.json` を更新する**（エンドポイント・スキーマ・タグ）

### 📋 タスクリストの更新ルール

以下のいずれかのアクションを実施した場合は、**必ずタスクリストを更新**してください：

- **PR を作成した** → `agentapi-proxy client task create` または `task update` でタスクを作成・更新し、PR の URL をリンクとして含める
- **開発環境にデプロイした** → タスクを作成・更新してデプロイ完了を記録する
- **変更をブランチにプッシュした** → タスクを作成・更新して進捗を反映する

タスクリストは常に最新の状態を保ち、ユーザーが現在の進捗とネクストアクションを把握できるようにしてください。

### ✅ 作業完了時の必須チェックリスト

作業完了時は以下を**順番に必ず実行**してください。1つでも省略してはいけません：

1. **`make lint` を実行する** - コードの品質チェック
2. **`make test` を実行する** - テストの実行
3. **変更をブランチにプッシュし PR を作成する**
4. **`agentapi-proxy client task create` でユーザータスクを1件作成する**
   - `--task-type user`、`--scope user` を指定する
   - ネクストアクションは最も重要なもの**1つだけ**に絞る（複数作らない）
   - PR を作成した場合は `--link "url|title"` 形式で PR の URL をリンクとして含める
   - 例：PR レビュー依頼タスクの作成
     ```bash
     agentapi-proxy client task create \
       --endpoint http://$AGENTAPI_PROXY_SERVICE_HOST:$AGENTAPI_PROXY_SERVICE_PORT \
       --session-id $AGENTAPI_SESSION_ID \
       --title "PR をレビューしてください" \
       --task-type user \
       --scope user \
       --link "https://github.com/owner/repo/pull/123|PR #123"
     ```
5. **`agentapi-proxy client send-notification` で通知を送る**
   - 例：
     ```bash
     agentapi-proxy client send-notification \
       --title "作業が完了しました" \
       --body "作業内容を確認してください" \
       --notify-session-id "$AGENTAPI_SESSION_ID"
     ```
