# Session Scope-based Access Control Design

## Overview

現在のセッションアクセス制御は `admin` か `user` の2パターンのみですが、より柔軟な組織レベルのアクセス制御を実現するため、`scope` 概念を導入します。

## 問題

### 現在の制限
- `admin` ロール：すべてのセッションにアクセス可能
- `admin` 以外：自分のセッションのみアクセス可能
- 組織内での部分的な権限共有ができない

### 具体的な課題
```go
// 現在のコード (pkg/auth/auth.go:UserOwnsSession)
if user.Role == "admin" {
    return true  // すべてのセッションにアクセス可能
}
if user.UserID != sessionUserID {
    return false  // 自分のセッション以外アクセス不可
}
```

## 提案：Scope-based Access Control

### 3つのスコープレベル

1. **`admin`** - グローバル管理者
   - すべてのセッションにアクセス可能（現在と同じ）

2. **`org`** - 組織レベル
   - 同じ組織のメンバーのセッションにアクセス可能
   - 例：同じGitHub organizationのメンバー

3. **`user`** - ユーザーレベル  
   - 自分のセッションのみアクセス可能（現在のdefaultと同じ）

### 設定例

```json
{
  "auth": {
    "github": {
      "user_mapping": {
        "team_role_mapping": {
          "myorg/platform-team": {
            "role": "platform-engineer",
            "scope": "org",
            "permissions": ["session:access", "session:delete"],
            "env_file": "/etc/agentapi/envs/platform.env"
          },
          "myorg/developers": {
            "role": "developer", 
            "scope": "user",
            "permissions": ["session:create", "session:access"],
            "env_file": "/etc/agentapi/envs/developer.env"
          },
          "myorg/admins": {
            "role": "admin",
            "scope": "admin", 
            "permissions": ["*"]
          }
        }
      }
    }
  }
}
```

## 実装設計

### 1. 設定構造の拡張

#### `TeamRoleRule` の拡張
```go
type TeamRoleRule struct {
    Role        string   `json:"role" mapstructure:"role" yaml:"role"`
    Scope       string   `json:"scope,omitempty" mapstructure:"scope" yaml:"scope"` // 新規追加
    Permissions []string `json:"permissions" mapstructure:"permissions" yaml:"permissions"`
    EnvFile     string   `json:"env_file,omitempty" mapstructure:"env_file" yaml:"env_file"`
}
```

#### `UserContext` の拡張
```go
type UserContext struct {
    UserID       string
    Role         string
    Scope        string            // 新規追加
    Organization string            // 新規追加 - org scopeで使用
    Permissions  []string
    // ... 既存フィールド
}
```

### 2. アクセス制御ロジックの変更

#### 新しい `UserCanAccessSession` 関数
```go
func UserCanAccessSession(c echo.Context, session *AgentSession) bool {
    user := GetUserFromContext(c)
    if user == nil {
        return false
    }

    switch user.Scope {
    case "admin":
        return true // すべてのセッションにアクセス可能
        
    case "org":
        // 同じ組織のメンバーのセッションにアクセス可能
        sessionOwner := GetUserInfoFromSession(session)
        return sessionOwner != nil && user.Organization == sessionOwner.Organization
        
    case "user":
        fallthrough
    default:
        // 自分のセッションのみアクセス可能
        return user.UserID == session.UserID
    }
}
```

### 3. セッション情報の拡張

#### `AgentSession` の拡張
```go
type AgentSession struct {
    ID           string
    Port         int
    UserID       string
    Organization string  // 新規追加 - org scopeで使用
    // ... 既存フィールド
}
```

### 4. GitHub認証での組織情報取得

#### 認証時の組織情報設定
```go
func (p *GitHubAuthProvider) Authenticate(ctx context.Context, token string) (*UserContext, error) {
    // ... 既存の認証処理
    
    // 組織情報を取得
    organization := p.extractPrimaryOrganization(user.Teams)
    
    // scope を決定
    scope := p.determineScopeFromTeams(teams)
    
    return &UserContext{
        UserID:       user.Login,
        Role:        role,
        Scope:       scope,        // 新規
        Organization: organization, // 新規
        // ...
    }
}
```

### 5. 後方互換性

#### デフォルト値の設定
- `scope` が未指定の場合：
  - `role == "admin"` → `scope = "admin"`
  - それ以外 → `scope = "user"`

#### 既存設定との互換性
```go
func (rule *TeamRoleRule) GetScope() string {
    if rule.Scope != "" {
        return rule.Scope
    }
    
    // 後方互換性：roleから推測
    if rule.Role == "admin" {
        return "admin"
    }
    return "user"
}
```

## 実装ステップ

### Phase 1: 基本構造の拡張
1. [ ] `TeamRoleRule` に `Scope` フィールド追加
2. [ ] `UserContext` に `Scope`, `Organization` フィールド追加  
3. [ ] `AgentSession` に `Organization` フィールド追加
4. [ ] 設定例の更新

### Phase 2: アクセス制御ロジック
1. [ ] `UserCanAccessSession` 関数の実装
2. [ ] セッション操作（削除、一覧、プロキシ）での適用
3. [ ] 後方互換性の確保

### Phase 3: GitHub認証での組織情報
1. [ ] チーム情報から組織を抽出するロジック
2. [ ] セッション作成時の組織情報設定
3. [ ] scope決定ロジック

### Phase 4: テストとドキュメント
1. [ ] 各scopeレベルのテスト作成
2. [ ] 後方互換性テスト
3. [ ] ドキュメント更新

## セキュリティ考慮事項

### 1. 組織情報の信頼性
- GitHub APIから取得した組織情報を使用
- セッション作成時に組織情報を固定

### 2. scope昇格の防止  
- 認証時にのみscopeを決定
- ランタイムでのscope変更を禁止

### 3. デフォルトセキュア
- 不明なscopeは `user` として扱う
- 組織情報が不明な場合は自分のセッションのみアクセス

## 影響範囲

### 変更が必要なファイル
- `pkg/config/config.go` - 設定構造
- `pkg/auth/auth.go` - アクセス制御ロジック  
- `pkg/auth/github.go` - GitHub認証
- `pkg/proxy/proxy.go` - セッション操作
- 設定例ファイル群
- テストファイル群

### 破壊的変更
なし（後方互換性を維持）

### パフォーマンス影響
- 組織情報の追加でメモリ使用量微増
- アクセス制御チェックの複雑化による軽微な処理時間増加

## 代替案検討

### 1. Permission-based approach
scopeではなく、より細かい権限設定で制御
- メリット：より柔軟
- デメリット：設定が複雑、理解しにくい

### 2. Tag-based filtering
セッションにタグを付けて、タグベースでアクセス制御
- メリット：非常に柔軟
- デメリット：複雑、組織の概念と合わない

### 3. 現状維持 + 追加権限
現在の仕組みを維持し、特定の権限を持つユーザーのみ他人のセッションにアクセス可能
- メリット：最小限の変更
- デメリット：組織レベルでの制御ができない

## 結論

scope-based approach が最も適切：
- 理解しやすい（admin/org/user）
- 組織の一般的なアクセス制御パターンに合致
- 後方互換性を維持可能
- 段階的な実装が可能