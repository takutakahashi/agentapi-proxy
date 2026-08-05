# RBAC (Role-Based Access Control)

agentapi-proxy には、APIキーベースの包括的なRBAC（Role-Based Access Control）システムが実装されており、ロールベースの権限管理とセッション所有権制御を提供します。

## 概要

RBACシステムの主要な特徴：

- **APIキーベース認証**: セキュアなAPIキー認証システム
- **ロールベース権限**: 管理者、一般ユーザー、読み取り専用ユーザーなどの役割定義
- **きめ細かい権限制御**: エンドポイントレベルでの権限チェック
- **セッション所有権制御**: ユーザーが自分のセッションのみアクセス可能
- **APIキーの有効期限**: 期限付きAPIキーのサポート
- **外部キー管理**: 外部ファイルからのAPIキー読み込み

## 設定方法

### 1. 認証の有効化

認証を有効にするには、設定ファイルで `auth.enabled` を `true` に設定します：

```json
{
  "auth": {
    "enabled": true,
    "header_name": "X-API-Key",
    "keys_file": "./api_keys.json"
  }
}
```

### 2. APIキーの設定

APIキーは2つの方法で設定できます：

#### 方法A: メイン設定ファイルに直接記述

```json
{
  "auth": {
    "enabled": true,
    "header_name": "X-API-Key",
    "api_keys": [
      {
        "key": "ap_admin_key_123456789abcdef",
        "user_id": "admin",
        "role": "admin",
        "permissions": ["*"],
        "created_at": "2024-06-14T00:00:00Z",
        "expires_at": "2025-06-14T00:00:00Z"
      }
    ]
  }
}
```

#### 方法B: 外部ファイルからの読み込み

`api_keys.json` ファイルを作成：

```json
{
  "api_keys": [
    {
      "key": "ap_admin_key_123456789abcdef",
      "user_id": "admin",
      "role": "admin",
      "permissions": ["*"],
      "created_at": "2024-06-14T00:00:00Z",
      "expires_at": "2025-06-14T00:00:00Z"
    },
    {
      "key": "ap_user_alice_987654321fedcba",
      "user_id": "alice",
      "role": "user",
      "permissions": ["session:create", "session:list", "session:delete", "session:access"],
      "created_at": "2024-06-14T00:00:00Z",
      "expires_at": "2025-06-14T00:00:00Z"
    },
    {
      "key": "ap_readonly_charlie_aabbccddeeff",
      "user_id": "charlie",
      "role": "readonly",
      "permissions": ["session:list"],
      "created_at": "2024-06-14T00:00:00Z",
      "expires_at": "2025-06-14T00:00:00Z"
    }
  ]
}
```

## ロールと権限

### 定義済みロール

#### 1. 管理者（admin）
- **権限**: `["*"]` (全ての操作が可能)
- **特権**: 全てのユーザーのセッションにアクセス可能
- **用途**: システム管理、監視、メンテナンス

#### 2. 一般ユーザー（user）
- **権限**: `["session:create", "session:list", "session:delete", "session:access"]`
- **制限**: 自分のセッションのみアクセス可能
- **用途**: 通常の開発作業

#### 3. 読み取り専用（readonly）
- **権限**: `["session:list"]`
- **制限**: セッションの一覧表示のみ可能
- **用途**: 監視、レポート作成

### 権限の詳細

| 権限 | 説明 | 対応エンドポイント |
|------|------|-------------------|
| `session:create` | セッション作成 | `POST /start` |
| `session:list` | セッション一覧表示・検索 | `GET /search` |
| `session:delete` | セッション削除 | `DELETE /sessions/:sessionId` |
| `session:access` | セッションへのプロキシアクセス | `ANY /:sessionId/*` |
| `*` | 全ての操作（ワイルドカード） | 全エンドポイント |

## 使用方法

### APIキーを使用したリクエスト

APIリクエストには、HTTPヘッダーでAPIキーを指定します。以下の2つの方法がサポートされています：

#### 方法A: カスタムヘッダー（X-API-Key）

```bash
# セッション作成（user権限必要）
curl -X POST http://localhost:8080/start \
  -H "X-API-Key: ap_user_alice_987654321fedcba" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice",
    "environment": {
      "GITHUB_TOKEN": "your-token"
    }
  }'
```

#### 方法B: Bearer トークン（Authorization ヘッダー）

```bash
# セッション作成（user権限必要）
curl -X POST http://localhost:8080/start \
  -H "Authorization: Bearer ap_user_alice_987654321fedcba" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice",
    "environment": {
      "GITHUB_TOKEN": "your-token"
    }
  }'

# セッション一覧表示（list権限必要）
curl -X GET http://localhost:8080/search \
  -H "Authorization: Bearer ap_user_alice_987654321fedcba"

# セッションアクセス（access権限必要）
curl -X GET http://localhost:8080/550e8400-e29b-41d4-a716-446655440000/api/workspaces \
  -H "Authorization: Bearer ap_user_alice_987654321fedcba"

# セッション削除（delete権限必要）
curl -X DELETE http://localhost:8080/sessions/550e8400-e29b-41d4-a716-446655440000 \
  -H "Authorization: Bearer ap_user_alice_987654321fedcba"
```

**注意**: 認証は優先順位として、まずカスタムヘッダー（X-API-Key）をチェックし、見つからない場合に Authorization ヘッダーの Bearer トークンをチェックします。

### セッション所有権制御

- **管理者**: 全てのセッションにアクセス可能
- **一般ユーザー**: 自分のセッションのみアクセス可能
- **読み取り専用**: セッション一覧表示時、自分のセッションのみ表示

```bash
# 管理者は全ユーザーのセッションを表示
curl -X GET http://localhost:8080/search \
  -H "X-API-Key: ap_admin_key_123456789abcdef"

# 一般ユーザーは自分のセッションのみ表示
curl -X GET http://localhost:8080/search \
  -H "X-API-Key: ap_user_alice_987654321fedcba"
```

## APIキーの管理

### APIキーの形式

```json
{
  "key": "ap_[role]_[username]_[random_string]",
  "user_id": "unique_user_identifier",
  "role": "admin|user|readonly",
  "permissions": ["permission1", "permission2"],
  "created_at": "2024-06-14T00:00:00Z",
  "expires_at": "2025-06-14T00:00:00Z"
}
```

### セキュリティベストプラクティス

1. **強力なAPIキー**: ランダムで十分な長さのAPIキーを使用
2. **最小権限の原則**: 必要最小限の権限のみを付与
3. **有効期限の設定**: APIキーには適切な有効期限を設定
4. **定期的なローテーション**: APIキーを定期的に更新
5. **ログ監視**: 認証失敗や権限違反のログを監視

### APIキーの無効化・更新

APIキーを無効化または更新するには：

1. `api_keys.json` ファイルから該当キーを削除または修正
2. サーバーを再起動（ホットリロードは現在未対応）

## エラーハンドリング

### 認証エラー

```bash
# 無効なAPIキー
HTTP/1.1 401 Unauthorized
{
  "error": "Invalid API key"
}

# 期限切れAPIキー
HTTP/1.1 401 Unauthorized
{
  "error": "API key expired"
}
```

### 認可エラー

```bash
# 権限不足
HTTP/1.1 403 Forbidden
{
  "error": "Insufficient permissions"
}

# セッション所有権違反
HTTP/1.1 403 Forbidden
{
  "error": "Access denied"
}
```

## トラブルシューティング

### よくある問題

1. **認証が無効になっている**
   - `auth.enabled` が `true` に設定されているか確認
   - 設定ファイルのJSONフォーマットが正しいか確認

2. **APIキーファイルが読み込まれない**
   - `keys_file` のパスが正しいか確認
   - ファイルの読み取り権限があるか確認

3. **権限エラーが発生する**
   - ユーザーのロールと権限設定を確認
   - エンドポイントに必要な権限を確認

4. **セッションにアクセスできない**
   - セッションの所有者が正しいか確認
   - 管理者権限が必要な場合は適切なAPIキーを使用

### ログ確認

認証・認可関連のログを確認：

```bash
# 詳細ログを有効にしてサーバー起動
./bin/agentapi-proxy server --verbose

# 認証成功/失敗のログを確認
# [INFO] Authentication successful for user: alice
# [WARN] Authentication failed for IP: 192.168.1.100
# [WARN] Authorization failed for user alice: insufficient permissions
```

## 開発・テスト時の設定

### 認証を無効にする

開発・テスト時には認証を無効にできます：

```json
{
  "auth": {
    "enabled": false
  }
}
```

認証が無効の場合、全てのリクエストが認証・認可をバイパスします。

### テスト用APIキー

テスト環境では以下のようなAPIキーを使用できます：

```json
{
  "api_keys": [
    {
      "key": "test_admin_key",
      "user_id": "test_admin",
      "role": "admin",
      "permissions": ["*"],
      "created_at": "2024-01-01T00:00:00Z",
      "expires_at": "2099-12-31T23:59:59Z"
    }
  ]
}
```

## まとめ

agentapi-proxy のRBACシステムは、本格的なマルチユーザー環境での使用に対応した包括的なアクセス制御機能を提供します。適切な設定により、セキュアで柔軟な権限管理が可能です。