# API仕様書

## 概要
このAPIは、セッションごとに `agentapi` を起動し、各セッションに対して個別にリクエストを処理します。

## エンドポイント一覧

### POST /start
- 新しいセッションを作成します。
- レスポンスとして、作成されたセッションIDを返します。
- 各セッションごとに `agentapi` が新規に起動されます。
- リクエストボディで任意のタグ（key-value）を指定できます。

#### リクエストボディ例
```json
{
  "user_id": "user123",
  "environment": {
    "CUSTOM_VAR": "value"
  },
  "tags": {
    "repository": "agentapi-proxy",
    "branch": "main",
    "env": "production"
  }
}
```

### /session_id/*
- すべての `/session_id/*` へのリクエストは、該当セッションの `agentapi` へ転送されます。
- セッションIDごとに独立した `agentapi` が動作しています。

### GET /search
- 既存のセッション一覧を検索・取得します。
- クエリパラメータでフィルタリングが可能です。
- 全体のセッション情報を取得するためのエンドポイントです。
- レスポンスはJSON形式で、セッションのリストを返します。
- タグによるフィルタリングが可能です（`tag.キー名=値` の形式）。

#### サポートするクエリパラメータ
- `user_id`: ユーザーIDでフィルタ
- `status`: ステータスでフィルタ
- `tag.{key}`: 指定したタグキーの値でフィルタ

#### リクエスト例
```
GET /search?user_id=123&status=active
GET /search?tag.repository=agentapi-proxy&tag.env=production
GET /search?user_id=123&tag.branch=main
```

#### レスポンス例
```json
{
  "sessions": [
    {
      "session_id": "abc123",
      "user_id": "123",
      "status": "active",
      "started_at": "2023-06-08T12:00:00Z",
      "port": 9000,
      "tags": {
        "repository": "agentapi-proxy",
        "branch": "main",
        "env": "production"
      }
    },
    {
      "session_id": "def456",
      "user_id": "123",
      "status": "active",
      "started_at": "2023-06-08T12:05:00Z",
      "port": 9001,
      "tags": {
        "repository": "another-repo",
        "branch": "develop",
        "env": "production"
      }
    }
  ]
}
```

## フロー
1. クライアントは `POST /start` で新しいセッションを作成します。
2. サーバーは新しいセッションIDを発行し、対応する `agentapi` を起動します。
3. クライアントは `/session_id/*` 形式のエンドポイントにリクエストを送信し、個別の `agentapi` とやりとりします。
