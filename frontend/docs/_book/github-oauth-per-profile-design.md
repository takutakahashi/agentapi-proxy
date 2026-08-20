# プロファイルごとのGitHub OAuth認証認可設計

## 1. 概要

agentapi-uiにおいて、プロファイルごとに独立したGitHub OAuth認証と認可制御を実現する設計案です。

## 2. 設計目標

- **プロファイル独立性**: 各プロファイルが独自のGitHub認証情報を持つ
- **権限分離**: プロファイルごとに異なるGitHub権限スコープを設定可能
- **セキュリティ**: トークンの安全な管理と適切な権限制御
- **UX**: シームレスなプロファイル切り替えと認証フロー

## 3. データ構造の拡張

### 3.1 Profile型の拡張

```typescript
// src/types/profile.ts
export interface Profile {
  // 既存のフィールド...
  
  // GitHub OAuth設定を追加
  githubAuth?: GitHubAuthSettings;
}

// 新規追加
export interface GitHubAuthSettings {
  enabled: boolean;
  clientId?: string;           // GitHub OAuth App Client ID (proxy側で管理)
  accessToken?: string;        // 暗号化されたアクセストークン
  refreshToken?: string;       // 暗号化されたリフレッシュトークン
  tokenExpiresAt?: string;     // トークン有効期限
  user?: GitHubUser;           // 認証済みユーザー情報
  scopes: string[];            // 許可されたGitHubスコープ
  organizations?: string[];     // アクセス可能な組織（制限する場合）
  repositories?: string[];      // アクセス可能なリポジトリ（制限する場合）
}

export interface GitHubUser {
  id: number;
  login: string;
  name?: string;
  email?: string;
  avatarUrl?: string;
}
```

### 3.2 AgentApiProxySettings型の拡張

```typescript
// src/types/settings.ts
export interface AgentApiProxySettings {
  // 既存のフィールド...
  
  // GitHub OAuth統合設定
  githubOAuthEnabled?: boolean;
  githubClientId?: string;      // プロファイル専用のClient ID（オプション）
}
```

## 4. 認証フロー

### 4.1 認証開始

1. ユーザーがプロファイル設定でGitHub連携を有効化
2. UI側がproxy側の `/auth/github/login` エンドポイントを呼び出し
3. proxy側がGitHub OAuth URLを生成して返却
4. UIが新規ウィンドウ/タブでGitHub認証ページを開く

### 4.2 コールバック処理

1. GitHub認証完了後、proxy側の `/auth/github/callback` に戻る
2. proxy側がアクセストークンを取得
3. UIに認証成功を通知（WebSocketまたはポーリング）
4. UIがトークンと認証情報をプロファイルに保存

### 4.3 トークン管理

```typescript
// src/services/githubAuthService.ts
export class GitHubAuthService {
  // プロファイルごとのトークン管理
  async saveToken(profileId: string, token: string, expiresAt: Date): Promise<void> {
    // トークンを暗号化して保存
    const encryptedToken = await this.encrypt(token);
    const profile = await profileManager.getProfile(profileId);
    
    profile.githubAuth = {
      ...profile.githubAuth,
      accessToken: encryptedToken,
      tokenExpiresAt: expiresAt.toISOString()
    };
    
    await profileManager.updateProfile(profile);
  }
  
  // トークンの取得と復号
  async getToken(profileId: string): Promise<string | null> {
    const profile = await profileManager.getProfile(profileId);
    if (!profile.githubAuth?.accessToken) return null;
    
    // トークンの有効期限チェック
    if (this.isTokenExpired(profile.githubAuth.tokenExpiresAt)) {
      await this.refreshToken(profileId);
    }
    
    return await this.decrypt(profile.githubAuth.accessToken);
  }
  
  // トークンリフレッシュ
  async refreshToken(profileId: string): Promise<void> {
    // proxy側のリフレッシュエンドポイントを呼び出し
    const response = await fetch('/api/auth/github/refresh', {
      headers: {
        'X-Profile-Id': profileId
      }
    });
    
    const { token, expiresAt } = await response.json();
    await this.saveToken(profileId, token, new Date(expiresAt));
  }
}
```

## 5. 認可制御

### 5.1 スコープ管理

```typescript
// src/constants/githubScopes.ts
export const GITHUB_SCOPES = {
  // 基本スコープ
  USER_READ: 'read:user',
  USER_EMAIL: 'user:email',
  
  // リポジトリスコープ
  REPO_READ: 'repo',
  REPO_STATUS: 'repo:status',
  REPO_DEPLOYMENT: 'repo_deployment',
  PUBLIC_REPO: 'public_repo',
  
  // 組織スコープ
  ORG_READ: 'read:org',
  
  // その他
  GIST: 'gist',
  NOTIFICATIONS: 'notifications'
} as const;

// プロファイルごとのデフォルトスコープ
export const DEFAULT_SCOPES_BY_PROFILE_TYPE = {
  readonly: [GITHUB_SCOPES.USER_READ, GITHUB_SCOPES.PUBLIC_REPO],
  developer: [GITHUB_SCOPES.USER_READ, GITHUB_SCOPES.REPO_READ, GITHUB_SCOPES.ORG_READ],
  admin: Object.values(GITHUB_SCOPES)
};
```

### 5.2 アクセス制御

```typescript
// src/utils/githubAccessControl.ts
export class GitHubAccessControl {
  // リポジトリへのアクセス可否をチェック
  async canAccessRepository(
    profileId: string, 
    owner: string, 
    repo: string
  ): Promise<boolean> {
    const profile = await profileManager.getProfile(profileId);
    const githubAuth = profile.githubAuth;
    
    if (!githubAuth?.enabled || !githubAuth.accessToken) {
      return false;
    }
    
    // 組織制限チェック
    if (githubAuth.organizations?.length > 0) {
      if (!githubAuth.organizations.includes(owner)) {
        return false;
      }
    }
    
    // リポジトリ制限チェック
    if (githubAuth.repositories?.length > 0) {
      const fullName = `${owner}/${repo}`;
      if (!githubAuth.repositories.includes(fullName)) {
        return false;
      }
    }
    
    return true;
  }
  
  // スコープの検証
  hasRequiredScopes(profile: Profile, requiredScopes: string[]): boolean {
    const grantedScopes = profile.githubAuth?.scopes || [];
    return requiredScopes.every(scope => grantedScopes.includes(scope));
  }
}
```

## 6. UI実装

### 6.1 プロファイル設定画面の拡張

```typescript
// src/components/profiles/GitHubAuthSettings.tsx
export const GitHubAuthSettings: React.FC<{ profile: Profile }> = ({ profile }) => {
  const [isConnecting, setIsConnecting] = useState(false);
  const githubAuth = profile.githubAuth;
  
  const handleConnect = async () => {
    setIsConnecting(true);
    try {
      // OAuth認証フローを開始
      const authUrl = await githubAuthService.getAuthUrl(profile.id);
      window.open(authUrl, '_blank');
      
      // 認証完了を待機
      await githubAuthService.waitForAuth(profile.id);
      toast.success('GitHub連携が完了しました');
    } catch (error) {
      toast.error('GitHub連携に失敗しました');
    } finally {
      setIsConnecting(false);
    }
  };
  
  return (
    <div className="space-y-4">
      <h3>GitHub連携設定</h3>
      
      {githubAuth?.user ? (
        <div className="flex items-center space-x-3">
          <img 
            src={githubAuth.user.avatarUrl} 
            alt={githubAuth.user.login}
            className="w-10 h-10 rounded-full"
          />
          <div>
            <p className="font-medium">{githubAuth.user.name || githubAuth.user.login}</p>
            <p className="text-sm text-gray-500">{githubAuth.user.email}</p>
          </div>
          <Button 
            variant="outline" 
            size="sm"
            onClick={() => githubAuthService.disconnect(profile.id)}
          >
            連携解除
          </Button>
        </div>
      ) : (
        <Button onClick={handleConnect} disabled={isConnecting}>
          {isConnecting ? '接続中...' : 'GitHubと連携'}
        </Button>
      )}
      
      {/* スコープ設定 */}
      <ScopeSelector 
        selectedScopes={githubAuth?.scopes || []}
        onChange={(scopes) => updateProfileScopes(profile.id, scopes)}
      />
      
      {/* アクセス制限設定 */}
      <AccessRestrictions
        organizations={githubAuth?.organizations || []}
        repositories={githubAuth?.repositories || []}
        onChange={(restrictions) => updateProfileRestrictions(profile.id, restrictions)}
      />
    </div>
  );
};
```

### 6.2 認証状態の表示

```typescript
// src/components/GitHubAuthStatus.tsx
export const GitHubAuthStatus: React.FC = () => {
  const currentProfile = useCurrentProfile();
  const githubAuth = currentProfile?.githubAuth;
  
  if (!githubAuth?.enabled || !githubAuth.user) {
    return null;
  }
  
  return (
    <div className="flex items-center space-x-2 text-sm">
      <GitHubIcon className="w-4 h-4" />
      <span>{githubAuth.user.login}</span>
      {isTokenExpired(githubAuth.tokenExpiresAt) && (
        <Badge variant="warning" size="sm">要再認証</Badge>
      )}
    </div>
  );
};
```

## 7. セキュリティ考慮事項

### 7.1 トークンの暗号化

```typescript
// src/utils/encryption.ts
export class TokenEncryption {
  private readonly algorithm = 'aes-256-gcm';
  
  // プロファイルごとの暗号化キー生成
  private async getEncryptionKey(profileId: string): Promise<CryptoKey> {
    const keyMaterial = await crypto.subtle.importKey(
      'raw',
      new TextEncoder().encode(profileId + process.env.ENCRYPTION_SALT),
      'PBKDF2',
      false,
      ['deriveKey']
    );
    
    return crypto.subtle.deriveKey(
      {
        name: 'PBKDF2',
        salt: new TextEncoder().encode('github-oauth-salt'),
        iterations: 100000,
        hash: 'SHA-256'
      },
      keyMaterial,
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt', 'decrypt']
    );
  }
  
  async encrypt(profileId: string, token: string): Promise<string> {
    const key = await this.getEncryptionKey(profileId);
    const iv = crypto.getRandomValues(new Uint8Array(12));
    
    const encrypted = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv },
      key,
      new TextEncoder().encode(token)
    );
    
    return btoa(String.fromCharCode(...iv, ...new Uint8Array(encrypted)));
  }
  
  async decrypt(profileId: string, encryptedToken: string): Promise<string> {
    const key = await this.getEncryptionKey(profileId);
    const data = Uint8Array.from(atob(encryptedToken), c => c.charCodeAt(0));
    const iv = data.slice(0, 12);
    const ciphertext = data.slice(12);
    
    const decrypted = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv },
      key,
      ciphertext
    );
    
    return new TextDecoder().decode(decrypted);
  }
}
```

### 7.2 セキュリティベストプラクティス

1. **トークンの保存**
   - ブラウザのlocalStorageに暗号化して保存
   - セッション終了時の自動削除オプション
   - トークンの有効期限管理

2. **通信のセキュリティ**
   - proxy通信時は必ずHTTPS使用
   - CSRFトークンの実装
   - プロファイルIDの検証

3. **権限の最小化**
   - 必要最小限のGitHubスコープのみ要求
   - プロファイルごとに権限を分離
   - 定期的な権限レビュー機能

## 8. 実装優先順位

1. **Phase 1: 基本機能**
   - Profile型へのGitHub認証フィールド追加
   - 基本的な認証フロー実装
   - トークンの暗号化保存

2. **Phase 2: UI統合**
   - プロファイル設定画面でのGitHub連携
   - 認証状態の表示
   - エラーハンドリング

3. **Phase 3: 高度な機能**
   - スコープ管理
   - アクセス制限設定
   - トークンの自動リフレッシュ

4. **Phase 4: セキュリティ強化**
   - 監査ログ
   - 異常検知
   - セキュリティ設定の一元管理