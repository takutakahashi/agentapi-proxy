# GitHub OAuth認証セットアップガイド

このガイドでは、agentapi-proxyでGitHub OAuth認証を設定する方法を説明します。OAuth認証により、ユーザーはGitHubアカウントでログインし、APIキーを管理する必要がなくなります。

## 目次

1. [前提条件](#前提条件)
2. [GitHub OAuth Appの作成](#github-oauth-appの作成)
3. [設定ファイルの作成](#設定ファイルの作成)
4. [環境変数の設定](#環境変数の設定)
5. [OAuthフローの使用](#oauthフローの使用)
6. [APIエンドポイント](#apiエンドポイント)
7. [セキュリティのベストプラクティス](#セキュリティのベストプラクティス)

## 前提条件

- GitHub アカウント
- agentapi-proxy v2.x 以降
- HTTPS対応のコールバックURL（本番環境の場合）

## GitHub OAuth Appの作成

### 1. GitHub.comでOAuth Appを作成

1. GitHubにログインし、**Settings** → **Developer settings** → **OAuth Apps** に移動
2. **New OAuth App** をクリック
3. 以下の情報を入力：
   - **Application name**: `AgentAPI Proxy` （任意の名前）
   - **Homepage URL**: アプリケーションのURL
   - **Authorization callback URL**: `https://your-domain.com/oauth/callback`
   - **Description**: （オプション）アプリケーションの説明
4. **Register application** をクリック
5. 生成された **Client ID** と **Client Secret** を安全に保存

### 2. GitHub Enterprise Serverの場合

1. GHESインスタンスの **Settings** → **Developer settings** → **OAuth Apps** に移動
2. 上記と同様の手順でOAuth Appを作成

## 設定ファイルの作成

### OAuth設定を含む設定ファイル

`config.json`を以下のように設定します：

```json
{
  "start_port": 9000,
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "base_url": "https://api.github.com",
      "token_header": "Authorization",
      "oauth": {
        "client_id": "${GITHUB_CLIENT_ID}",
        "client_secret": "${GITHUB_CLIENT_SECRET}",
        "scope": "repo workflow read:org admin:repo_hook notifications user:email",
        "base_url": "https://github.com"
      },
      "user_mapping": {
        "default_role": "user",
        "default_permissions": ["read", "session:create", "session:list"],
        "team_role_mapping": {
          "myorg/admins": {
            "role": "admin",
            "permissions": ["*"]
          },
          "myorg/developers": {
            "role": "developer",
            "permissions": ["read", "write", "execute", "session:create", "session:list", "session:delete", "session:access"]
          }
        }
      }
    }
  }
}
```

### GitHub Enterprise Server向け設定

```json
{
  "auth": {
    "github": {
      "base_url": "https://github.enterprise.com/api/v3",
      "oauth": {
        "base_url": "https://github.enterprise.com",
        "client_id": "${GHES_CLIENT_ID}",
        "client_secret": "${GHES_CLIENT_SECRET}"
      }
    }
  }
}
```

## 環境変数の設定

OAuth認証に必要な環境変数を設定します：

```bash
# GitHub OAuth App credentials
export GITHUB_CLIENT_ID="your-client-id"
export GITHUB_CLIENT_SECRET="your-client-secret"

# オプション: GitHub Enterprise Server
export GHES_CLIENT_ID="your-ghes-client-id"
export GHES_CLIENT_SECRET="your-ghes-client-secret"
```

## OAuthフローの使用

### 1. 認証URLの生成

```bash
# OAuth認証を開始
curl -X POST https://your-proxy.com/oauth/authorize \
  -H "Content-Type: application/json" \
  -d '{
    "redirect_uri": "https://your-app.com/callback"
  }'
```

レスポンス例：
```json
{
  "auth_url": "https://github.com/login/oauth/authorize?client_id=xxx&redirect_uri=xxx&scope=xxx&state=xxx",
  "state": "secure-random-state"
}
```

### 2. ユーザーのリダイレクト

ユーザーを`auth_url`にリダイレクトします。ユーザーはGitHubでアプリケーションを承認します。

### 3. コールバックの処理

GitHubがユーザーをコールバックURLにリダイレクトします：

```
https://your-app.com/callback?code=xxx&state=xxx
```

### 4. アクセストークンの取得

```bash
# コールバックパラメータでトークンを取得
curl -X GET "https://your-proxy.com/oauth/callback?code=xxx&state=xxx"
```

レスポンス例：
```json
{
  "session_id": "uuid-session-id",
  "access_token": "gho_xxxxxxxxxxxx",
  "token_type": "Bearer",
  "expires_at": "2024-01-01T00:00:00Z",
  "user": {
    "user_id": "github-username",
    "role": "developer",
    "permissions": ["read", "write", "session:create"],
    "github_user": {
      "login": "github-username",
      "id": 123456,
      "email": "user@example.com"
    }
  }
}
```

### 5. セッションを使用したAPIアクセス

```bash
# セッションIDを使用してAPIにアクセス
curl -H "X-Session-ID: uuid-session-id" \
     https://your-proxy.com/search

# または Bearer tokenとして使用
curl -H "Authorization: Bearer uuid-session-id" \
     https://your-proxy.com/search
```

## APIエンドポイント

### OAuth認証エンドポイント

#### POST /oauth/authorize
OAuth認証フローを開始します。

**リクエスト:**
```json
{
  "redirect_uri": "https://your-app.com/callback"
}
```

**レスポンス:**
```json
{
  "auth_url": "https://github.com/login/oauth/authorize?...",
  "state": "secure-state-parameter"
}
```

#### GET /oauth/callback
GitHubからのコールバックを処理し、セッションを作成します。

**パラメータ:**
- `code`: GitHubから提供される認証コード
- `state`: 認証リクエストで生成されたstate

**レスポンス:**
```json
{
  "session_id": "uuid",
  "access_token": "gho_xxx",
  "token_type": "Bearer",
  "expires_at": "2024-01-01T00:00:00Z",
  "user": {...}
}
```

#### POST /oauth/logout
セッションをログアウトし、GitHubトークンを無効化します。

**ヘッダー:**
- `X-Session-ID: uuid` または
- `Authorization: Bearer uuid`

**レスポンス:**
```json
{
  "message": "Successfully logged out"
}
```

#### POST /oauth/refresh
セッションの有効期限を延長します。

**ヘッダー:**
- `X-Session-ID: uuid`

**レスポンス:**
```json
{
  "access_token": "gho_xxx",
  "token_type": "Bearer",
  "expires_at": "2024-01-02T00:00:00Z",
  "user": {...}
}
```

## JavaScriptでの実装例

```javascript
// OAuth認証フローの実装
class GitHubOAuthClient {
  constructor(proxyUrl) {
    this.proxyUrl = proxyUrl;
    this.sessionId = localStorage.getItem('oauth_session_id');
  }

  async startAuth(redirectUri) {
    const response = await fetch(`${this.proxyUrl}/oauth/authorize`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ redirect_uri: redirectUri })
    });
    
    const data = await response.json();
    // stateを保存（CSRF対策）
    sessionStorage.setItem('oauth_state', data.state);
    // GitHubにリダイレクト
    window.location.href = data.auth_url;
  }

  async handleCallback() {
    const params = new URLSearchParams(window.location.search);
    const code = params.get('code');
    const state = params.get('state');
    
    // state検証
    const savedState = sessionStorage.getItem('oauth_state');
    if (state !== savedState) {
      throw new Error('Invalid state parameter');
    }
    
    const response = await fetch(
      `${this.proxyUrl}/oauth/callback?code=${code}&state=${state}`
    );
    
    const data = await response.json();
    // セッションIDを保存
    localStorage.setItem('oauth_session_id', data.session_id);
    this.sessionId = data.session_id;
    
    return data;
  }

  async makeAuthenticatedRequest(endpoint, options = {}) {
    if (!this.sessionId) {
      throw new Error('Not authenticated');
    }
    
    return fetch(`${this.proxyUrl}${endpoint}`, {
      ...options,
      headers: {
        ...options.headers,
        'X-Session-ID': this.sessionId
      }
    });
  }

  async logout() {
    if (!this.sessionId) return;
    
    await fetch(`${this.proxyUrl}/oauth/logout`, {
      method: 'POST',
      headers: { 'X-Session-ID': this.sessionId }
    });
    
    localStorage.removeItem('oauth_session_id');
    this.sessionId = null;
  }
}

// 使用例
const client = new GitHubOAuthClient('https://your-proxy.com');

// 認証開始
await client.startAuth('https://your-app.com/callback');

// コールバックページで
await client.handleCallback();

// 認証済みリクエスト
const sessions = await client.makeAuthenticatedRequest('/search');
```

## GitHub OAuthスコープの選択

### 開発用途別のスコープ設定

#### 1. 基本的な開発（推奨）
```json
"scope": "repo workflow read:org admin:repo_hook notifications user:email"
```
- **repo**: プライベート・パブリックリポジトリの読み書き、プルリクエスト、イシュー管理
- **workflow**: GitHub Actionsワークフローファイルの作成・編集
- **read:org**: Organization メンバーシップの確認
- **admin:repo_hook**: リポジトリWebhookの管理
- **notifications**: 通知の管理
- **user:email**: ユーザーのメールアドレス取得

#### 2. パブリックリポジトリのみ
```json
"scope": "public_repo workflow read:org notifications user:email"
```
- **public_repo**: パブリックリポジトリのみの読み書き
- プライベートリポジトリへのアクセスが不要な場合

#### 3. エンタープライズ環境（フル権限）
```json
"scope": "repo workflow admin:org admin:repo_hook admin:org_hook notifications user:email delete_repo"
```
- **admin:org**: Organization設定の完全管理
- **admin:org_hook**: Organization レベルのWebhook管理
- **delete_repo**: リポジトリ削除権限

#### 4. 読み取り専用（最小権限）
```json
"scope": "read:user read:org"
```
- ユーザー情報とOrganization情報の読み取りのみ

### スコープ詳細説明

| スコープ | 説明 | 用途 |
|---------|------|------|
| `repo` | プライベート・パブリックリポジトリのフル権限 | 基本的な開発作業 |
| `public_repo` | パブリックリポジトリのみの読み書き | オープンソース開発 |
| `workflow` | GitHub Actionsワークフローの管理 | CI/CD設定 |
| `admin:org` | Organization の完全管理 | 管理者権限 |
| `admin:repo_hook` | リポジトリWebhookの管理 | 統合・自動化 |
| `admin:org_hook` | OrganizationWebhookの管理 | エンタープライズ統合 |
| `notifications` | 通知の読み書き | 通知管理 |
| `user:email` | ユーザーメールアドレス | 識別・連絡用 |
| `delete_repo` | リポジトリ削除 | 管理作業 |

### 設定例ファイル

複数のサンプル設定ファイルが利用可能です：

- **config.oauth.example.json**: 基本的な開発用設定
- **config.oauth.development.example.json**: 開発チーム向け詳細設定
- **config.oauth.public-only.example.json**: パブリックリポジトリ専用
- **config.oauth.enterprise.example.json**: エンタープライズ環境向け

## セキュリティのベストプラクティス

### 1. HTTPS の使用
- 本番環境では必ずHTTPSを使用
- コールバックURLもHTTPSを使用

### 2. State パラメータの検証
- CSRF攻撃を防ぐため、必ずstateパラメータを検証
- stateは予測不可能なランダム値を使用

### 3. クライアントシークレットの保護
- クライアントシークレットは環境変数で管理
- 設定ファイルに直接記述しない
- バージョン管理システムにコミットしない

### 4. スコープの最小化
- 必要最小限のGitHubスコープのみを要求
- 開発要件に応じて適切なスコープセットを選択
- 定期的にスコープの見直しを実施

### 5. セッション管理
- セッションには適切な有効期限を設定（デフォルト24時間）
- 定期的に期限切れセッションをクリーンアップ
- ログアウト時はGitHubトークンも無効化

### 6. エラーハンドリング
```javascript
try {
  await client.handleCallback();
} catch (error) {
  if (error.message.includes('Invalid state')) {
    // CSRF攻撃の可能性
    console.error('Security error: Invalid state');
  } else if (error.message.includes('expired')) {
    // セッション期限切れ
    await client.startAuth(redirectUri);
  }
}
```

### 7. 監査ログ
- すべての認証イベントをログに記録
- 異常なアクセスパターンを監視
- 定期的にログを確認

## トラブルシューティング

### 認証エラー（401）
- Client IDとClient Secretが正しいか確認
- コールバックURLがOAuth Appの設定と一致しているか確認

### CORS エラー
- プロキシのCORS設定を確認
- フロントエンドのオリジンが許可されているか確認

### セッション期限切れ
- `/oauth/refresh`エンドポイントを使用してセッションを更新
- または再度認証フローを実行

このガイドに従うことで、安全で使いやすいGitHub OAuth認証を実装できます。