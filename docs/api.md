# API仕様書

## 概要
このAPIは、セッションごとに `agentapi` を起動し、各セッションに対して個別にリクエストを処理します。

## エンドポイント一覧

### セッション管理

#### POST /start
- 新しいセッションを作成します。
- レスポンスとして、作成されたセッションIDを返します。
- 各セッションごとに `agentapi` が新規に起動されます。
- リクエストボディで任意のタグ（key-value）を指定できます。

#### リクエストボディ例
```json
{
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

**注意**: `user_id` は認証されたユーザーのトークンから自動的に割り当てられます。

#### POST /start-with-profile
- プロファイルを使用して新しいセッションを作成します。
- プロファイルで定義された環境変数とリクエストで指定された環境変数をマージします。
- リクエストで指定された環境変数がプロファイルの設定より優先されます。

##### リクエストボディ例
```json
{
  "profile_id": "profile-uuid-here",
  "environment": {
    "OVERRIDE_VAR": "override_value"
  },
  "tags": {
    "session_type": "profile-based",
    "environment": "production"
  },
  "message": "Optional initial message"
}
```

##### レスポンス例
```json
{
  "session_id": "session-uuid-here"
}
```

#### /session_id/*
- すべての `/session_id/*` へのリクエストは、該当セッションの `agentapi` へ転送されます。
- セッションIDごとに独立した `agentapi` が動作しています。

#### GET /search
- 既存のセッション一覧を検索・取得します。
- クエリパラメータでフィルタリングが可能です。
- 全体のセッション情報を取得するためのエンドポイントです。
- レスポンスはJSON形式で、セッションのリストを返します。
- タグによるフィルタリングが可能です（`tag.キー名=値` の形式）。

##### サポートするクエリパラメータ
- `status`: ステータスでフィルタ
- `tag.{key}`: 指定したタグキーの値でフィルタ

##### リクエスト例
```
GET /search?status=active
GET /search?tag.repository=agentapi-proxy&tag.env=production
GET /search?tag.branch=main
```

**注意**: セッションのフィルタリングは認証されたユーザーのコンテキストに基づいて自動的に行われます。管理者以外のユーザーは自分のセッションのみを表示できます。

##### レスポンス例
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

### プロファイル管理

プロファイル機能により、ユーザーは環境変数、リポジトリ履歴、システムプロンプト、メッセージテンプレートを管理できます。

#### POST /profiles
- 新しいプロファイルを作成します。
- ユーザーごとに独立したプロファイルが作成されます。

##### リクエストボディ例
```json
{
  "name": "開発環境プロファイル",
  "description": "開発作業用の設定",
  "environment": {
    "NODE_ENV": "development",
    "DEBUG": "true",
    "API_BASE_URL": "https://dev-api.example.com"
  },
  "system_prompt": "あなたは開発者のアシスタントです。コードレビューやデバッグを支援してください。",
  "message_templates": [
    {
      "name": "コードレビュー依頼",
      "content": "以下のコードをレビューしてください:\n\n{{code}}\n\n特に{{focus_area}}について確認をお願いします。",
      "variables": ["code", "focus_area"],
      "category": "review"
    }
  ]
}
```

##### レスポンス例
```json
{
  "profile": {
    "id": "profile-uuid-here",
    "user_id": "user-123",
    "name": "開発環境プロファイル",
    "description": "開発作業用の設定",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true",
      "API_BASE_URL": "https://dev-api.example.com"
    },
    "repository_history": [],
    "system_prompt": "あなたは開発者のアシスタントです。コードレビューやデバッグを支援してください。",
    "message_templates": [
      {
        "id": "template-uuid-here",
        "name": "コードレビュー依頼",
        "content": "以下のコードをレビューしてください:\n\n{{code}}\n\n特に{{focus_area}}について確認をお願いします。",
        "variables": ["code", "focus_area"],
        "category": "review",
        "created_at": "2024-01-01T12:00:00Z"
      }
    ],
    "created_at": "2024-01-01T12:00:00Z",
    "updated_at": "2024-01-01T12:00:00Z"
  }
}
```

#### GET /profiles
- ユーザーのプロファイル一覧を取得します。
- 認証されたユーザーのプロファイルのみが返されます。

##### レスポンス例
```json
{
  "profiles": [
    {
      "id": "profile-uuid-1",
      "user_id": "user-123",
      "name": "開発環境プロファイル",
      "description": "開発作業用の設定",
      "environment": {
        "NODE_ENV": "development"
      },
      "repository_history": [],
      "system_prompt": "開発者アシスタント",
      "message_templates": [],
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ],
  "total": 1
}
```

#### GET /profiles/:profileId
- 特定のプロファイルの詳細を取得します。
- 自分のプロファイルのみアクセス可能です。

##### レスポンス例
```json
{
  "profile": {
    "id": "profile-uuid-here",
    "user_id": "user-123",
    "name": "開発環境プロファイル",
    "description": "開発作業用の設定",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true"
    },
    "repository_history": [
      {
        "id": "repo-uuid-here",
        "url": "https://github.com/example/project",
        "name": "example-project",
        "branch": "main",
        "last_commit": "abc123def456",
        "accessed_at": "2024-01-01T12:00:00Z"
      }
    ],
    "system_prompt": "開発者アシスタント",
    "message_templates": [],
    "created_at": "2024-01-01T12:00:00Z",
    "updated_at": "2024-01-01T12:00:00Z",
    "last_used_at": "2024-01-01T13:00:00Z"
  }
}
```

#### PUT /profiles/:profileId
- 既存のプロファイルを更新します。
- 部分更新が可能です（指定されたフィールドのみが更新されます）。

##### リクエストボディ例
```json
{
  "name": "更新された開発環境プロファイル",
  "environment": {
    "NODE_ENV": "development",
    "DEBUG": "true",
    "NEW_VAR": "new_value"
  },
  "system_prompt": "更新されたシステムプロンプト"
}
```

##### レスポンス例
```json
{
  "profile": {
    "id": "profile-uuid-here",
    "user_id": "user-123",
    "name": "更新された開発環境プロファイル",
    "description": "開発作業用の設定",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true",
      "NEW_VAR": "new_value"
    },
    "repository_history": [],
    "system_prompt": "更新されたシステムプロンプト",
    "message_templates": [],
    "created_at": "2024-01-01T12:00:00Z",
    "updated_at": "2024-01-01T12:30:00Z"
  }
}
```

#### DELETE /profiles/:profileId
- プロファイルを削除します。
- 自分のプロファイルのみ削除可能です。

##### レスポンス例
```json
{
  "message": "Profile deleted successfully",
  "profile_id": "profile-uuid-here"
}
```

#### POST /profiles/:profileId/repositories
- プロファイルにリポジトリエントリを追加します。
- 同じURLのリポジトリが既に存在する場合は更新されます。

##### リクエストボディ例
```json
{
  "url": "https://github.com/example/new-project",
  "name": "new-project",
  "branch": "develop",
  "last_commit": "def456abc789",
  "metadata": {
    "framework": "React",
    "language": "TypeScript"
  }
}
```

##### レスポンス例
```json
{
  "message": "Repository added to profile successfully"
}
```

#### POST /profiles/:profileId/templates
- プロファイルにメッセージテンプレートを追加します。

##### リクエストボディ例
```json
{
  "name": "バグ報告テンプレート",
  "content": "## バグ報告\n\n**発生環境**: {{environment}}\n**再現手順**:\n1. {{step1}}\n2. {{step2}}\n\n**期待される動作**: {{expected}}\n**実際の動作**: {{actual}}",
  "variables": ["environment", "step1", "step2", "expected", "actual"],
  "category": "bug-report",
  "metadata": {
    "priority": "high",
    "template_version": "1.0"
  }
}
```

##### レスポンス例
```json
{
  "message": "Template added to profile successfully"
}
```

## フロー

### 基本的なセッション作成フロー
1. クライアントは `POST /start` で新しいセッションを作成します。
2. サーバーは新しいセッションIDを発行し、対応する `agentapi` を起動します。
3. クライアントは `/session_id/*` 形式のエンドポイントにリクエストを送信し、個別の `agentapi` とやりとりします。

### プロファイルベースのセッション作成フロー
1. クライアントは `POST /profiles` でプロファイルを作成します（初回のみ）。
2. クライアントは `POST /start-with-profile` でプロファイルIDを指定してセッションを作成します。
3. サーバーはプロファイルの設定（環境変数、システムプロンプト等）を適用した `agentapi` を起動します。
4. プロファイルの使用履歴が自動的に更新されます。
5. クライアントは通常どおり `/session_id/*` 形式のエンドポイントで `agentapi` とやりとりします。

### プロファイル管理フロー
1. **作成**: `POST /profiles` で新しいプロファイルを作成
2. **確認**: `GET /profiles` で作成済みプロファイル一覧を取得
3. **詳細取得**: `GET /profiles/:profileId` で特定プロファイルの詳細を取得
4. **更新**: `PUT /profiles/:profileId` でプロファイル設定を更新
5. **リポジトリ追加**: `POST /profiles/:profileId/repositories` でリポジトリ履歴を追加
6. **テンプレート追加**: `POST /profiles/:profileId/templates` でメッセージテンプレートを追加
7. **削除**: `DELETE /profiles/:profileId` で不要なプロファイルを削除

## 認証・認可

すべてのAPIエンドポイントは認証が必要です。以下の権限体系が適用されます：

### セッション管理権限
- `session:create` - セッション作成権限
- `session:list` - セッション一覧取得権限
- `session:access` - セッションアクセス権限
- `session:delete` - セッション削除権限

### プロファイル管理権限
- `profile:create` - プロファイル作成権限
- `profile:read` - プロファイル読み取り権限
- `profile:list` - プロファイル一覧取得権限
- `profile:update` - プロファイル更新権限
- `profile:delete` - プロファイル削除権限

**注意**: ユーザーは自分が作成したプロファイルとセッションのみにアクセスできます。管理者権限を持つユーザーは全てのリソースにアクセス可能です。
