# HelmでのGitHub App認証セットアップガイド

このガイドでは、Helmを使用してagentapi-proxyでGitHub App認証を設定する方法を説明します。GitHub AppはOAuth Appよりも細かい権限制御が可能で、大規模な組織での使用に適しています。

## 目次

1. [前提条件](#前提条件)
2. [GitHub Appの作成](#github-appの作成)
3. [Helm設定](#helm設定)
4. [GitHub App認証の実装詳細](#github-app認証の実装詳細)
5. [デプロイメント](#デプロイメント)
6. [使用方法](#使用方法)
7. [トラブルシューティング](#トラブルシューティング)

## 前提条件

- Kubernetes クラスター
- Helm 3.x 以降
- GitHub 組織の管理者権限（GitHub App作成のため）
- agentapi-proxy Helm チャート

## GitHub Appの作成

### 1. GitHub Appの新規作成

1. GitHub組織の **Settings** → **Developer settings** → **GitHub Apps** に移動
2. **New GitHub App** をクリック
3. 以下の設定を行います：

#### 基本設定
```
App name: agentapi-proxy
Homepage URL: https://your-agentapi.example.com
Webhook URL: https://your-agentapi.example.com/webhook (使用しない場合は無効化)
```

#### 権限設定
以下の権限を設定します：

**Repository permissions:**
- Contents: Read
- Issues: Read
- Pull requests: Read
- Metadata: Read

**Organization permissions:**
- Members: Read

**Account permissions:**
- Email addresses: Read

#### イベント設定
```
☐ Repository (チェックを外す)
☐ Issues (チェックを外す)
☐ Pull request (チェックを外す)
```

### 2. GitHub Appの設定完了

1. **Create GitHub App** をクリック
2. App IDをメモ（後で使用）
3. **Generate a private key** をクリックしてPEMファイルをダウンロード

### 3. GitHub Appのインストール

1. 作成したGitHub Appの設定画面で **Install App** をクリック
2. 対象の組織を選択
3. **All repositories** または **Selected repositories** を選択してインストール
4. Installation IDをURLから取得（例：`/settings/installations/12345678` → `12345678`）

## Helm設定

### 1. GitHub App秘密鍵のSecret作成

```bash
# PEMファイルからSecretを作成
kubectl create secret generic github-app-private-key \
  --from-file=private-key=/path/to/your-app.private-key.pem
```

### 2. values.yamlの設定

```yaml
# GitHub App認証設定
github:
  app:
    # GitHub App ID
    id: "123456"
    # GitHub App Installation ID  
    installationId: "12345678"
    # GitHub App の秘密鍵を含むSecret
    privateKey:
      secretName: "github-app-private-key"
      key: "private-key"

# 認証を有効化
config:
  auth:
    enabled: true
    github:
      enabled: true
      baseUrl: "https://api.github.com"
      tokenHeader: "Authorization"

# 認証設定（ConfigMapに格納）
authConfig:
  github:
    user_mapping:
      default_role: "user"
      default_permissions:
        - "read"
        - "session:create"
        - "session:list"
      team_role_mapping:
        "myorg/admins":
          role: "admin"
          permissions:
            - "*"
        "myorg/developers":
          role: "developer"
          permissions:
            - "read"
            - "write"
            - "execute"
            - "session:create"
            - "session:list"
            - "session:delete"
            - "session:access"
        "myorg/qa-team":
          role: "tester"
          permissions:
            - "read"
            - "session:create"
            - "session:list"
            - "session:access"
        "myorg/viewers":
          role: "viewer"
          permissions:
            - "read"
            - "session:list"

# Ingress設定
ingress:
  enabled: true
  className: "nginx"
  hosts:
    - host: agentapi.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: agentapi-proxy-tls
      hosts:
        - agentapi.example.com

# 永続化設定
persistence:
  enabled: true
  size: 50Gi
```

### 3. GitHub Enterprise Server用の設定

```yaml
# GitHub Enterprise Server を使用する場合
github:
  enterprise:
    enabled: true
    # GitHub Enterprise Server のベース URL
    baseUrl: "https://github.company.com"
    # GitHub Enterprise Server API の URL  
    apiUrl: "https://github.company.com/api/v3"
  app:
    id: "123456"
    installationId: "12345678"
    privateKey:
      secretName: "github-app-private-key"
      key: "private-key"

config:
  auth:
    enabled: true
    github:
      enabled: true
      baseUrl: "https://github.company.com/api/v3"
      tokenHeader: "Authorization"
```

## GitHub App認証の実装詳細

### 環境変数の設定

Helmテンプレートでは以下の環境変数が自動的に設定されます：

```yaml
# StatefulSetで設定される環境変数
env:
  - name: GITHUB_APP_ID
    value: "123456"
  - name: GITHUB_INSTALLATION_ID  
    value: "12345678"
  - name: GITHUB_APP_PEM
    valueFrom:
      secretKeyRef:
        name: github-app-private-key
        key: private-key
```

### 認証フロー

1. **Installation Token生成**: GitHub App IDと秘密鍵を使用してJWTを生成
2. **Access Token取得**: JWTを使用してInstallation Access Tokenを取得
3. **ユーザー認証**: Access TokenでGitHub APIにアクセスしてユーザー情報を取得
4. **権限マッピング**: チームメンバーシップに基づいて権限を決定

### セキュリティ考慮事項

- **秘密鍵の保護**: PEMファイルはKubernetes Secretに安全に格納
- **最小権限の原則**: GitHub Appには必要最小限の権限のみ付与
- **Access Token TTL**: Installation Access Tokenは1時間で自動的に期限切れ

## デプロイメント

### 1. Helmチャートのインストール

```bash
# Helmリポジトリの追加
helm repo add agentapi-proxy https://takutakahashi.github.io/agentapi-proxy

# values.yamlを使用してインストール
helm install agentapi-proxy agentapi-proxy/agentapi-proxy \
  -f values.yaml \
  --namespace agentapi \
  --create-namespace
```

### 2. デプロイメントの確認

```bash
# Podの状態確認
kubectl get pods -n agentapi

# ログの確認
kubectl logs -n agentapi deployment/agentapi-proxy

# Secret の確認
kubectl get secrets -n agentapi
```

### 3. アップグレード

```bash
# 設定変更後のアップグレード
helm upgrade agentapi-proxy agentapi-proxy/agentapi-proxy \
  -f values.yaml \
  --namespace agentapi
```

## 使用方法

### 1. GitHub Personal Access Tokenでの認証

GitHub Appがインストールされている組織のメンバーは、Personal Access Tokenを使用して認証できます：

```bash
# API リクエスト例
curl -H "Authorization: Bearer ghp_xxxxxxxxxxxx" \
     https://agentapi.example.com/api/sessions
```

### 2. プログラムでの使用例

#### JavaScript/Node.js
```javascript
const headers = {
  'Authorization': `Bearer ${process.env.GITHUB_TOKEN}`,
  'Content-Type': 'application/json'
};

const response = await fetch('https://agentapi.example.com/api/sessions', {
  headers
});

const sessions = await response.json();
console.log(sessions);
```

#### Python
```python
import os
import requests

headers = {
    'Authorization': f'Bearer {os.environ["GITHUB_TOKEN"]}',
    'Content-Type': 'application/json'
}

response = requests.get('https://agentapi.example.com/api/sessions', headers=headers)
sessions = response.json()
print(sessions)
```

### 3. CLI での使用

```bash
# 環境変数設定
export GITHUB_TOKEN="ghp_xxxxxxxxxxxx"

# agentapi CLIでの使用
agentapi-proxy client --server https://agentapi.example.com
```

## トラブルシューティング

### よくある問題と解決方法

#### 1. GitHub App認証失敗

**症状**: `GitHub App authentication failed` エラー

**原因と対処法**:
```bash
# App ID確認
kubectl get configmap -n agentapi agentapi-proxy -o yaml | grep GITHUB_APP_ID

# Installation ID確認  
kubectl get configmap -n agentapi agentapi-proxy -o yaml | grep GITHUB_INSTALLATION_ID

# 秘密鍵確認
kubectl get secret -n agentapi github-app-private-key -o yaml
```

#### 2. 権限不足エラー

**症状**: `Insufficient permissions` エラー

**原因と対処法**:
- GitHub Appに必要な権限が付与されているか確認
- Organization/Team設定が正しいか確認
- ユーザーが適切なチームに所属しているか確認

```bash
# 認証設定確認
kubectl get configmap -n agentapi agentapi-proxy-auth-config -o yaml
```

#### 3. Installation Token取得失敗

**症状**: `Failed to get installation token` エラー

**原因と対処法**:
- GitHub Appが正しくインストールされているか確認
- Installation IDが正しいか確認
- 秘密鍵のフォーマットが正しいか確認

#### 4. ネットワーク接続エラー

**症状**: GitHub APIへの接続失敗

**原因と対処法**:
```bash
# DNS解決確認
kubectl exec -n agentapi deployment/agentapi-proxy -- nslookup api.github.com

# ネットワーク接続確認
kubectl exec -n agentapi deployment/agentapi-proxy -- curl -I https://api.github.com
```

### ログレベルの設定

詳細なデバッグ情報を出力するには：

```yaml
# values.yamlに追加
env:
  - name: LOG_LEVEL
    value: "debug"
  - name: AGENTAPI_VERBOSE
    value: "true"
```

### ヘルスチェック

```bash
# アプリケーションのヘルスチェック
curl https://agentapi.example.com/health

# GitHub APIへの接続確認
curl -H "Authorization: Bearer ghp_xxxxxxxxxxxx" \
     https://agentapi.example.com/api/auth/info
```

## セキュリティのベストプラクティス

### 1. 秘密鍵の管理

```bash
# 秘密鍵のローテーション
kubectl create secret generic github-app-private-key-new \
  --from-file=private-key=/path/to/new-private-key.pem

# values.yamlでSecret名を更新してHelm upgrade
```

### 2. 権限の最小化

```yaml
# 最小権限の例
authConfig:
  github:
    user_mapping:
      default_role: "guest"
      default_permissions:
        - "read"
      team_role_mapping:
        "security-team/admins":
          role: "admin"
          permissions:
            - "*"
        "engineering/senior":
          role: "developer"
          permissions:
            - "read"
            - "write"
            - "execute"
```

### 3. 監査ログ

```yaml
# ログ設定の例
env:
  - name: AUDIT_LOG_ENABLED
    value: "true"
  - name: AUDIT_LOG_LEVEL
    value: "info"
```

### 4. ネットワークセキュリティ

```yaml
# Network Policy の例（別途適用）
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: agentapi-proxy-netpol
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: agentapi-proxy
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # GitHub API
```

このガイドに従って設定することで、Helmを使用したセキュアで柔軟なGitHub App認証システムを構築できます。