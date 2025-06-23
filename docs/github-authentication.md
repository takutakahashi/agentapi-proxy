# GitHub認証セットアップガイド

このガイドでは、agentapi-proxyでGitHub認証を設定する方法を説明します。GitHub.comとGitHub Enterprise Server (GHES) の両方に対応しています。

## 目次

1. [前提条件](#前提条件)
2. [GitHub Personal Access Token の作成](#github-personal-access-token-の作成)
3. [設定ファイルの作成](#設定ファイルの作成)
4. [認証方式の選択](#認証方式の選択)
5. [チーム・組織ベースの権限設定](#チーム組織ベースの権限設定)
6. [使用方法](#使用方法)
7. [トラブルシューティング](#トラブルシューティング)

## 前提条件

- GitHub アカウント（GitHub.com または GHES）
- 組織のメンバーシップ（チームベース権限を使用する場合）
- agentapi-proxy v1.x 以降

## GitHub Personal Access Token の作成

### GitHub.com の場合

1. GitHub.com にログインします
2. **Settings** → **Developer settings** → **Personal access tokens** → **Tokens (classic)** に移動
3. **Generate new token** → **Generate new token (classic)** をクリック
4. 以下の権限を選択します：
   - `read:user` - ユーザー情報の読み取り
   - `read:org` - 組織情報の読み取り
   - `read:team` - チーム情報の読み取り (private organization の場合)
5. **Generate token** をクリック
6. 生成されたトークンをコピーして安全に保存します

### GitHub Enterprise Server の場合

1. GHES インスタンスにログインします
2. **Settings** → **Developer settings** → **Personal access tokens** に移動
3. 上記と同様の権限でトークンを作成します

## 設定ファイルの作成

### 基本的なGitHub認証設定

```json
{
  "start_port": 9000,
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "base_url": "https://api.github.com",
      "token_header": "Authorization",
      "user_mapping": {
        "default_role": "guest",
        "default_permissions": ["read"],
        "team_role_mapping": {
          "myorg/admins": {
            "role": "admin",
            "permissions": ["*"]
          },
          "myorg/developers": {
            "role": "developer",
            "permissions": ["read", "write", "execute"]
          }
        }
      }
    }
  }
}
```

### GitHub Enterprise Server設定

```json
{
  "start_port": 9000,
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "base_url": "https://github.enterprise.com/api/v3",
      "token_header": "Authorization",
      "user_mapping": {
        "default_role": "employee",
        "default_permissions": ["read"],
        "team_role_mapping": {
          "enterprise-org/platform-team": {
            "role": "admin",
            "permissions": ["*"]
          },
          "enterprise-org/developers": {
            "role": "developer",
            "permissions": ["read", "write", "execute"]
          }
        }
      }
    }
  }
}
```

### ハイブリッド認証（静的APIキー + GitHub）

```json
{
  "start_port": 9000,
  "auth": {
    "enabled": true,
    "static": {
      "enabled": true,
      "header_name": "X-API-Key",
      "api_keys": [
        {
          "key": "admin-emergency-key",
          "user_id": "emergency-admin",
          "role": "admin",
          "permissions": ["*"],
          "created_at": "2024-01-01T00:00:00Z"
        }
      ]
    },
    "github": {
      "enabled": true,
      "base_url": "https://api.github.com",
      "token_header": "Authorization",
      "user_mapping": {
        "default_role": "user",
        "default_permissions": ["read"],
        "team_role_mapping": {
          "myorg/team-leads": {
            "role": "admin",
            "permissions": ["*"]
          }
        }
      }
    }
  }
}
```

## 認証方式の選択

### 1. GitHub認証のみ
- **使用場面**: GitHubを主要な認証基盤として使用
- **メリット**: 一元的な権限管理、チーム変更の自動反映
- **設定**: `auth.github` のみを有効化

### 2. ハイブリッド認証
- **使用場面**: 緊急時アクセス用の静的キーも必要
- **メリット**: GitHub障害時のフォールバック、サービスアカウント対応
- **設定**: `auth.static` と `auth.github` の両方を有効化

## チーム・組織ベースの権限設定

### チームマッピングの設定

```json
"team_role_mapping": {
  "組織名/チーム名": {
    "role": "ロール名",
    "permissions": ["権限1", "権限2"]
  }
}
```

### 推奨権限設計

```json
"team_role_mapping": {
  "mycompany/platform-engineering": {
    "role": "admin",
    "permissions": ["*"]
  },
  "mycompany/senior-developers": {
    "role": "senior_dev",
    "permissions": ["read", "write", "execute", "debug", "deploy"]
  },
  "mycompany/developers": {
    "role": "developer", 
    "permissions": ["read", "write", "execute"]
  },
  "mycompany/qa-engineers": {
    "role": "tester",
    "permissions": ["read", "execute"]
  },
  "mycompany/interns": {
    "role": "trainee",
    "permissions": ["read"]
  },
  "partner-org/consultants": {
    "role": "contractor",
    "permissions": ["read", "limited_write"]
  }
}
```

### デフォルト設定

- **default_role**: チームにマッチしないユーザーのデフォルトロール
- **default_permissions**: デフォルトロールの基本権限

## 使用方法

### APIリクエストでの認証

#### Bearer Token形式
```bash
curl -H "Authorization: Bearer ghp_xxxxxxxxxxxx" \
     https://your-proxy.com/api/sessions
```

#### Token形式
```bash
curl -H "Authorization: token ghp_xxxxxxxxxxxx" \
     https://your-proxy.com/api/sessions
```

### プログラムでの使用

#### JavaScript/Node.js
```javascript
const headers = {
  'Authorization': `Bearer ${process.env.GITHUB_TOKEN}`,
  'Content-Type': 'application/json'
};

fetch('https://your-proxy.com/api/sessions', { headers })
  .then(response => response.json())
  .then(data => console.log(data));
```

#### Python
```python
import os
import requests

headers = {
    'Authorization': f'Bearer {os.environ["GITHUB_TOKEN"]}',
    'Content-Type': 'application/json'
}

response = requests.get('https://your-proxy.com/api/sessions', headers=headers)
print(response.json())
```

#### Go
```go
package main

import (
    "fmt"
    "net/http"
    "os"
)

func main() {
    client := &http.Client{}
    req, _ := http.NewRequest("GET", "https://your-proxy.com/api/sessions", nil)
    req.Header.Set("Authorization", "Bearer "+os.Getenv("GITHUB_TOKEN"))
    
    resp, err := client.Do(req)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    defer resp.Body.Close()
    
    fmt.Printf("Status: %s\n", resp.Status)
}
```

## トラブルシューティング

### よくある問題と解決方法

#### 1. 認証失敗 (401 Unauthorized)

**原因と対処法**:
- **無効なトークン**: トークンが正しいか、期限切れでないか確認
- **権限不足**: トークンに必要な権限（`read:user`, `read:org`, `read:team`）があるか確認
- **ヘッダー形式**: `Authorization: Bearer <token>` または `Authorization: token <token>` の形式で送信しているか確認

#### 2. 権限不足 (403 Forbidden)

**原因と対処法**:
- **チーム未所属**: 必要なGitHubチームに所属しているか確認
- **組織プライバシー**: private組織の場合、適切な権限でトークンを作成しているか確認
- **設定ミス**: `team_role_mapping` の組織名/チーム名が正しいか確認

#### 3. GHES接続エラー

**原因と対処法**:
- **base_url設定**: 正しいGHES APIエンドポイントを設定しているか確認
- **証明書エラー**: 自己署名証明書の場合、クライアント側で証明書検証を調整
- **ネットワーク**: プロキシが必要な環境では適切なネットワーク設定を確認

#### 4. チーム情報が取得できない

**原因と対処法**:
- **組織の可視性**: 組織のメンバーシップが public に設定されているか確認
- **チームの可視性**: チームのプライバシー設定を確認
- **トークン権限**: `read:org` と `read:team` 権限が付与されているか確認

### ログの確認

agentapi-proxyのログで認証状況を確認できます：

```bash
# 認証成功時
GitHub authentication successful: user username (role: developer) from 192.168.1.100

# 認証失敗時  
GitHub authentication failed: failed to get user info: GitHub API returned status 401 from 192.168.1.100
```

### デバッグ設定

詳細なログを出力するには、verbose フラグを使用します：

```bash
agentapi-proxy server -v --config config.json
```

### 設定の検証

設定ファイルの構文チェック：

```bash
# JSON構文チェック
cat config.json | jq .

# 設定ロードテスト
agentapi-proxy server --config config.json --dry-run
```

## セキュリティのベストプラクティス

1. **トークンの管理**:
   - トークンは環境変数で管理し、設定ファイルに直接記述しない
   - 定期的にトークンをローテーションする
   - 最小権限の原則に従い、必要最小限の権限のみ付与

2. **チーム設計**:
   - 職務に応じた適切なチーム分けを行う
   - 外部パートナーは別組織で管理し、制限された権限のみ付与
   - 定期的にチームメンバーシップを見直す

3. **監査ログ**:
   - agentapi-proxyのアクセスログを保存・監視する
   - GitHubの組織監査ログを定期的に確認する
   - 異常なアクセスパターンを検知するアラートを設定

4. **フォールバック**:
   - GitHub障害に備えて緊急時用の静的APIキーを準備
   - ハイブリッド認証で冗長性を確保
   - 緊急時のアクセス手順を文書化

このガイドに従って設定することで、セキュアで柔軟なGitHub認証システムを構築できます。