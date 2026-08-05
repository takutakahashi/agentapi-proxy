# GitHub OAuth認証 クイックスタートガイド

このガイドでは、agentapi-proxyでGitHub OAuth認証を素早くセットアップして使用する方法を説明します。

## 目次

1. [5分でセットアップ](#5分でセットアップ)
2. [動作確認](#動作確認)
3. [フロントエンド実装例](#フロントエンド実装例)
4. [トラブルシューティング](#トラブルシューティング)

## 5分でセットアップ

### ステップ1: GitHub OAuth Appを作成

1. GitHubにログインして [Settings > Developer settings > OAuth Apps](https://github.com/settings/developers) にアクセス
2. **New OAuth App** をクリック
3. 以下の情報を入力：
   ```
   Application name: AgentAPI Proxy Dev
   Homepage URL: http://localhost:8080
   Authorization callback URL: http://localhost:3000/callback
   ```
4. **Register application** をクリック
5. Client IDをコピー
6. **Generate a new client secret** をクリックしてClient Secretを生成・コピー

### ステップ2: 環境変数を設定

```bash
# .envファイルを作成
cat > .env << EOF
GITHUB_CLIENT_ID=your_client_id_here
GITHUB_CLIENT_SECRET=your_client_secret_here
EOF

# 環境変数をエクスポート
export $(cat .env | xargs)
```

### ステップ3: 設定ファイルを作成

```bash
# config.oauth.example.jsonをコピー
cp config.oauth.example.json config.json

# 環境変数を使用するように設定されているか確認
cat config.json | grep -E "(GITHUB_CLIENT_ID|GITHUB_CLIENT_SECRET)"
```

### ステップ4: プロキシサーバーを起動

```bash
# Dockerを使用する場合
docker run -p 8080:8080 \
  -e GITHUB_CLIENT_ID=$GITHUB_CLIENT_ID \
  -e GITHUB_CLIENT_SECRET=$GITHUB_CLIENT_SECRET \
  -v $(pwd)/config.json:/app/config.json \
  ghcr.io/takutakahashi/agentapi-proxy:latest server

# または、ビルド済みバイナリを使用
./bin/agentapi-proxy server --config config.json --port 8080
```

## 動作確認

### 1. 認証フローのテスト（curlを使用）

```bash
# 1. 認証URLを取得
curl -X POST http://localhost:8080/oauth/authorize \
  -H "Content-Type: application/json" \
  -d '{"redirect_uri": "http://localhost:3000/callback"}' \
  | jq

# レスポンス例:
# {
#   "auth_url": "https://github.com/login/oauth/authorize?client_id=xxx&redirect_uri=xxx&scope=xxx&state=xxx",
#   "state": "secure-random-state"
# }
```

### 2. ブラウザで認証

1. 上記の `auth_url` をブラウザで開く
2. GitHubにログインしてアプリケーションを承認
3. `http://localhost:3000/callback?code=xxx&state=xxx` にリダイレクトされる

### 3. アクセストークンを取得

```bash
# コールバックのcode と state を使用
curl "http://localhost:8080/oauth/callback?code=YOUR_CODE&state=YOUR_STATE" | jq

# レスポンス例:
# {
#   "session_id": "550e8400-e29b-41d4-a716-446655440000",
#   "access_token": "gho_xxxxxxxxxxxx",
#   "token_type": "Bearer",
#   "expires_at": "2024-01-02T00:00:00Z",
#   "user": {
#     "user_id": "your-github-username",
#     "role": "developer",
#     "permissions": ["read", "write", "session:create"]
#   }
# }
```

### 4. 認証されたAPIリクエスト

```bash
# セッションIDを使用してAPIにアクセス
SESSION_ID="550e8400-e29b-41d4-a716-446655440000"

# 新しいagentapiセッションを作成
curl -X POST http://localhost:8080/start \
  -H "X-Session-ID: $SESSION_ID" \
  -H "Content-Type: application/json" \
  -d '{"tags": {"project": "my-app"}}'

# セッション一覧を取得
curl http://localhost:8080/search \
  -H "X-Session-ID: $SESSION_ID"
```

## フロントエンド実装例

### React/Next.jsでの実装

```jsx
// components/GitHubLogin.jsx
import { useState, useEffect } from 'react';

const PROXY_URL = process.env.NEXT_PUBLIC_PROXY_URL || 'http://localhost:8080';
const REDIRECT_URI = `${window.location.origin}/auth/callback`;

export function GitHubLogin() {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [user, setUser] = useState(null);

  useEffect(() => {
    // 既存のセッションをチェック
    const sessionId = localStorage.getItem('agentapi_session_id');
    if (sessionId) {
      checkSession(sessionId);
    }
  }, []);

  async function startOAuth() {
    try {
      const response = await fetch(`${PROXY_URL}/oauth/authorize`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ redirect_uri: REDIRECT_URI })
      });
      
      const data = await response.json();
      
      // stateを保存（CSRF対策）
      sessionStorage.setItem('oauth_state', data.state);
      
      // GitHubにリダイレクト
      window.location.href = data.auth_url;
    } catch (error) {
      console.error('OAuth start failed:', error);
    }
  }

  async function checkSession(sessionId) {
    try {
      // セッションの有効性を確認（セッション情報を取得）
      const response = await fetch(`${PROXY_URL}/search`, {
        headers: { 'X-Session-ID': sessionId }
      });
      
      if (response.ok) {
        setIsAuthenticated(true);
        // ユーザー情報を取得する追加のAPIコールが必要な場合
      } else {
        // セッションが無効な場合
        localStorage.removeItem('agentapi_session_id');
        setIsAuthenticated(false);
      }
    } catch (error) {
      console.error('Session check failed:', error);
    }
  }

  async function logout() {
    const sessionId = localStorage.getItem('agentapi_session_id');
    if (!sessionId) return;

    try {
      await fetch(`${PROXY_URL}/oauth/logout`, {
        method: 'POST',
        headers: { 'X-Session-ID': sessionId }
      });
    } finally {
      localStorage.removeItem('agentapi_session_id');
      setIsAuthenticated(false);
      setUser(null);
    }
  }

  if (isAuthenticated) {
    return (
      <div>
        <p>ログイン済み: {user?.user_id}</p>
        <button onClick={logout}>ログアウト</button>
      </div>
    );
  }

  return (
    <button onClick={startOAuth}>
      GitHubでログイン
    </button>
  );
}
```

```jsx
// pages/auth/callback.jsx (Next.js) または 
// src/routes/auth/callback.jsx (React Router)
import { useEffect } from 'react';
import { useRouter } from 'next/router'; // または useNavigate (React Router)

const PROXY_URL = process.env.NEXT_PUBLIC_PROXY_URL || 'http://localhost:8080';

export default function OAuthCallback() {
  const router = useRouter();

  useEffect(() => {
    handleCallback();
  }, []);

  async function handleCallback() {
    const params = new URLSearchParams(window.location.search);
    const code = params.get('code');
    const state = params.get('state');
    
    // エラーチェック
    if (!code || !state) {
      console.error('Missing code or state');
      router.push('/login?error=missing_params');
      return;
    }

    // state検証
    const savedState = sessionStorage.getItem('oauth_state');
    if (state !== savedState) {
      console.error('Invalid state parameter');
      router.push('/login?error=invalid_state');
      return;
    }

    try {
      const response = await fetch(
        `${PROXY_URL}/oauth/callback?code=${code}&state=${state}`
      );
      
      if (!response.ok) {
        throw new Error('OAuth callback failed');
      }

      const data = await response.json();
      
      // セッション情報を保存
      localStorage.setItem('agentapi_session_id', data.session_id);
      localStorage.setItem('agentapi_user', JSON.stringify(data.user));
      
      // クリーンアップ
      sessionStorage.removeItem('oauth_state');
      
      // ダッシュボードにリダイレクト
      router.push('/dashboard');
    } catch (error) {
      console.error('OAuth callback error:', error);
      router.push('/login?error=callback_failed');
    }
  }

  return (
    <div>
      <p>認証処理中...</p>
    </div>
  );
}
```

### APIクライアントクラス

```javascript
// lib/agentapi-client.js
export class AgentAPIClient {
  constructor(proxyUrl = 'http://localhost:8080') {
    this.proxyUrl = proxyUrl;
    this.sessionId = null;
  }

  // 保存されたセッションIDを読み込み
  loadSession() {
    this.sessionId = localStorage.getItem('agentapi_session_id');
    return this.sessionId;
  }

  // 認証済みリクエストを送信
  async request(path, options = {}) {
    if (!this.sessionId) {
      throw new Error('Not authenticated');
    }

    const response = await fetch(`${this.proxyUrl}${path}`, {
      ...options,
      headers: {
        ...options.headers,
        'X-Session-ID': this.sessionId,
        'Content-Type': 'application/json'
      }
    });

    if (response.status === 401) {
      // セッション期限切れ
      this.clearSession();
      throw new Error('Session expired');
    }

    if (!response.ok) {
      throw new Error(`API error: ${response.status}`);
    }

    return response.json();
  }

  // 新しいagentapiセッションを開始
  async createSession(data = {}) {
    return this.request('/start', {
      method: 'POST',
      body: JSON.stringify(data)
    });
  }

  // セッション一覧を取得
  async listSessions(filters = {}) {
    const params = new URLSearchParams(filters);
    return this.request(`/search?${params}`);
  }

  // agentapiサーバーにリクエストを転送
  async proxyRequest(sessionId, path, options = {}) {
    return this.request(`/${sessionId}${path}`, options);
  }

  // セッションをクリア
  clearSession() {
    this.sessionId = null;
    localStorage.removeItem('agentapi_session_id');
    localStorage.removeItem('agentapi_user');
  }
}

// 使用例
const client = new AgentAPIClient();
client.loadSession();

try {
  // 新しいagentapiセッションを作成
  const session = await client.createSession({
    tags: { project: 'my-app' },
    environment: { DEBUG: 'true' }
  });
  
  console.log('Created session:', session.session_id);
  
  // agentapiサーバーにリクエスト
  const workspaces = await client.proxyRequest(
    session.session_id,
    '/api/workspaces'
  );
} catch (error) {
  if (error.message === 'Session expired') {
    // 再ログインが必要
    window.location.href = '/login';
  }
}
```

## Docker Composeでの開発環境

```yaml
# docker-compose.yml
version: '3.8'

services:
  agentapi-proxy:
    image: ghcr.io/takutakahashi/agentapi-proxy:latest
    ports:
      - "8080:8080"
    environment:
      - GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
      - GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
    volumes:
      - ./config.json:/app/config.json
      - ./sessions:/app/sessions
    command: server --config /app/config.json

  # 開発用のフロントエンド
  frontend:
    image: node:20
    working_dir: /app
    ports:
      - "3000:3000"
    volumes:
      - ./frontend:/app
    environment:
      - NEXT_PUBLIC_PROXY_URL=http://localhost:8080
    command: npm run dev
```

```bash
# 起動
docker-compose up -d

# ログ確認
docker-compose logs -f agentapi-proxy
```

## トラブルシューティング

### よくある問題と解決方法

#### 1. "Invalid client" エラー

**原因**: Client IDまたはClient Secretが正しくない

**解決方法**:
```bash
# 環境変数を確認
echo $GITHUB_CLIENT_ID
echo $GITHUB_CLIENT_SECRET

# 設定ファイルを確認
cat config.json | jq '.auth.github.oauth'
```

#### 2. "Redirect URI mismatch" エラー

**原因**: GitHub OAuth Appに登録したコールバックURLと一致しない

**解決方法**:
- GitHub OAuth Appの設定でAuthorization callback URLを確認
- リクエストで送信している`redirect_uri`が完全に一致することを確認

#### 3. CORS エラー

**原因**: フロントエンドのオリジンが許可されていない

**解決方法**:
```bash
# プロキシサーバーのCORS設定を確認
# デフォルトでは全オリジンが許可されているはず
```

#### 4. セッションが見つからない

**原因**: セッションが期限切れまたは無効

**解決方法**:
```javascript
// セッションをリフレッシュ
async function refreshSession() {
  const sessionId = localStorage.getItem('agentapi_session_id');
  
  try {
    const response = await fetch(`${PROXY_URL}/oauth/refresh`, {
      method: 'POST',
      headers: { 'X-Session-ID': sessionId }
    });
    
    if (!response.ok) {
      throw new Error('Refresh failed');
    }
    
    const data = await response.json();
    console.log('Session refreshed, expires at:', data.expires_at);
  } catch (error) {
    // 再ログインが必要
    window.location.href = '/login';
  }
}
```

### デバッグモード

```bash
# 詳細なログを有効化
./bin/agentapi-proxy server --config config.json --verbose

# または環境変数で
DEBUG=true ./bin/agentapi-proxy server
```

### ログの確認

```bash
# OAuth関連のログを確認
tail -f logs/*.log | grep -E "(OAuth|GitHub|authentication)"

# セッション関連のログ
tail -f logs/*.log | grep -E "(session|Session)"
```

## 次のステップ

1. [セキュリティのベストプラクティス](github-oauth.md#セキュリティのベストプラクティス)を確認
2. [チーム権限の設定](github-authentication.md#チーム組織ベースの権限設定)
3. [本番環境へのデプロイ](../README.md#deployment)

質問や問題がある場合は、[GitHubのIssue](https://github.com/takutakahashi/agentapi-proxy/issues)で報告してください。