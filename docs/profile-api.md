# プロファイル API 仕様書

## 概要

プロファイル機能は、ユーザーが環境変数、リポジトリ履歴、システムプロンプト、メッセージテンプレートを管理できる機能です。各ユーザーは独自のプロファイルを作成・管理でき、これらの設定を使用してセッションを開始できます。

## データ構造

### Profile

```json
{
  "id": "string (UUID)",
  "user_id": "string",
  "name": "string",
  "description": "string (optional)",
  "environment": {
    "key": "value"
  },
  "repository_history": [
    {
      "id": "string (UUID)",
      "url": "string",
      "name": "string",
      "branch": "string (optional)",
      "last_commit": "string (optional)",
      "metadata": {
        "key": "value"
      },
      "accessed_at": "string (ISO 8601)"
    }
  ],
  "system_prompt": "string",
  "message_templates": [
    {
      "id": "string (UUID)",
      "name": "string",
      "content": "string",
      "variables": ["string"],
      "category": "string (optional)",
      "metadata": {
        "key": "value"
      },
      "created_at": "string (ISO 8601)"
    }
  ],
  "created_at": "string (ISO 8601)",
  "updated_at": "string (ISO 8601)",
  "last_used_at": "string (ISO 8601, optional)"
}
```

### RepositoryEntry

```json
{
  "id": "string (UUID, auto-generated)",
  "url": "string (required)",
  "name": "string (required)",
  "branch": "string (optional)",
  "last_commit": "string (optional)",
  "metadata": {
    "framework": "string",
    "language": "string",
    "custom_key": "custom_value"
  },
  "accessed_at": "string (ISO 8601, auto-generated)"
}
```

### MessageTemplate

```json
{
  "id": "string (UUID, auto-generated)",
  "name": "string (required)",
  "content": "string (required)",
  "variables": ["string"],
  "category": "string (optional)",
  "metadata": {
    "priority": "string",
    "template_version": "string",
    "custom_key": "custom_value"
  },
  "created_at": "string (ISO 8601, auto-generated)"
}
```

## API エンドポイント

### プロファイル CRUD 操作

#### POST /profiles

新しいプロファイルを作成します。

**必要な権限**: `profile:create`

**リクエストボディ**:
```json
{
  "name": "string (required)",
  "description": "string (optional)",
  "environment": {
    "key": "value"
  },
  "repository_history": [
    {
      "url": "string (required)",
      "name": "string (required)",
      "branch": "string (optional)",
      "last_commit": "string (optional)",
      "metadata": {}
    }
  ],
  "system_prompt": "string (required)",
  "message_templates": [
    {
      "name": "string (required)",
      "content": "string (required)",
      "variables": ["string"],
      "category": "string (optional)",
      "metadata": {}
    }
  ]
}
```

**レスポンス**: `201 Created`
```json
{
  "profile": {
    // Full Profile object
  }
}
```

**エラーレスポンス**:
- `400 Bad Request` - 無効なリクエストボディ
- `401 Unauthorized` - 認証が必要
- `403 Forbidden` - 権限不足

#### GET /profiles

ユーザーのプロファイル一覧を取得します。

**必要な権限**: `profile:list`

**レスポンス**: `200 OK`
```json
{
  "profiles": [
    {
      // Profile object
    }
  ],
  "total": 1
}
```

#### GET /profiles/:profileId

特定のプロファイルの詳細を取得します。

**必要な権限**: `profile:read`

**パラメータ**:
- `profileId` (required): プロファイルのUUID

**レスポンス**: `200 OK`
```json
{
  "profile": {
    // Full Profile object
  }
}
```

**エラーレスポンス**:
- `404 Not Found` - プロファイルが見つからない
- `403 Forbidden` - 他のユーザーのプロファイルにアクセス

#### PUT /profiles/:profileId

既存のプロファイルを更新します。部分更新をサポートします。

**必要な権限**: `profile:update`

**パラメータ**:
- `profileId` (required): プロファイルのUUID

**リクエストボディ** (すべてのフィールドがオプション):
```json
{
  "name": "string",
  "description": "string",
  "environment": {
    "key": "value"
  },
  "repository_history": [
    {
      // RepositoryEntry objects
    }
  ],
  "system_prompt": "string",
  "message_templates": [
    {
      // MessageTemplate objects
    }
  ]
}
```

**レスポンス**: `200 OK`
```json
{
  "profile": {
    // Updated Profile object
  }
}
```

#### DELETE /profiles/:profileId

プロファイルを削除します。

**必要な権限**: `profile:delete`

**パラメータ**:
- `profileId` (required): プロファイルのUUID

**レスポンス**: `200 OK`
```json
{
  "message": "Profile deleted successfully",
  "profile_id": "profile-uuid-here"
}
```

### プロファイル拡張操作

#### POST /profiles/:profileId/repositories

プロファイルにリポジトリエントリを追加します。同じURLのリポジトリが既に存在する場合は更新されます。

**必要な権限**: `profile:update`

**パラメータ**:
- `profileId` (required): プロファイルのUUID

**リクエストボディ**:
```json
{
  "url": "string (required)",
  "name": "string (required)",
  "branch": "string (optional)",
  "last_commit": "string (optional)",
  "metadata": {
    "framework": "React",
    "language": "TypeScript",
    "custom_key": "custom_value"
  }
}
```

**レスポンス**: `200 OK`
```json
{
  "message": "Repository added to profile successfully"
}
```

#### POST /profiles/:profileId/templates

プロファイルにメッセージテンプレートを追加します。

**必要な権限**: `profile:update`

**パラメータ**:
- `profileId` (required): プロファイルのUUID

**リクエストボディ**:
```json
{
  "name": "string (required)",
  "content": "string (required)",
  "variables": ["string"],
  "category": "string (optional)",
  "metadata": {
    "priority": "high",
    "template_version": "1.0",
    "custom_key": "custom_value"
  }
}
```

**レスポンス**: `200 OK`
```json
{
  "message": "Template added to profile successfully"
}
```

### プロファイルベースセッション作成

#### POST /start-with-profile

プロファイルの設定を使用して新しいセッションを作成します。

**必要な権限**: `session:create`

**リクエストボディ**:
```json
{
  "profile_id": "string (required)",
  "environment": {
    "key": "value"
  },
  "tags": {
    "key": "value"
  },
  "message": "string (optional)"
}
```

**動作**:
1. 指定されたプロファイルを取得
2. プロファイルの環境変数とリクエストの環境変数をマージ（リクエストが優先）
3. プロファイルの`last_used_at`を現在時刻に更新
4. マージされた設定でセッションを作成

**レスポンス**: `201 Created`
```json
{
  "session_id": "session-uuid-here"
}
```

## 使用例

### 開発環境プロファイルの作成

```bash
curl -X POST http://localhost:8080/profiles \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "開発環境",
    "description": "Node.js開発用の設定",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true",
      "API_BASE_URL": "https://dev-api.example.com"
    },
    "system_prompt": "あなたはNode.js開発者のアシスタントです。コードレビューとデバッグを支援してください。",
    "message_templates": [
      {
        "name": "バグ報告",
        "content": "## バグ報告\n\n**環境**: {{environment}}\n**再現手順**: {{steps}}\n**期待値**: {{expected}}\n**実際**: {{actual}}",
        "variables": ["environment", "steps", "expected", "actual"],
        "category": "bug"
      }
    ]
  }'
```

### プロファイルを使用したセッション作成

```bash
curl -X POST http://localhost:8080/start-with-profile \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "profile_id": "profile-uuid-here",
    "environment": {
      "CUSTOM_VAR": "session_specific_value"
    },
    "tags": {
      "project": "my-project",
      "session_type": "development"
    }
  }'
```

### リポジトリ履歴の追加

```bash
curl -X POST http://localhost:8080/profiles/profile-uuid-here/repositories \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "url": "https://github.com/myorg/myproject",
    "name": "myproject",
    "branch": "feature/new-feature",
    "last_commit": "abc123def456",
    "metadata": {
      "framework": "React",
      "language": "TypeScript",
      "maintainer": "team-frontend"
    }
  }'
```

## セキュリティ

- すべてのAPIエンドポイントは認証が必要
- ユーザーは自分が作成したプロファイルのみにアクセス可能
- 管理者権限を持つユーザーは全てのプロファイルにアクセス可能
- 環境変数などの機密情報は適切に管理される
- プロファイル削除時は関連するすべてのデータが削除される

## 制限事項

- プロファイル名は同一ユーザー内で一意である必要はありません
- リポジトリ履歴は同一URLで重複登録された場合、最新のものに更新されます
- メッセージテンプレートの変数展開は実装されていません（フロントエンド側で処理）
- プロファイル数に制限はありませんが、パフォーマンスを考慮して適切な数に抑えることを推奨します