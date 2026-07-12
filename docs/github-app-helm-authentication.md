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
   ※ Installation IDは省略可能で、指定しない場合は自動的に検出されます

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
    # GitHub App Installation ID (省略可能：指定しない場合は自動検出)
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
    installationId: "12345678"  # 省略可能：指定しない場合は自動検出
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
    value: "12345678"  # 省略可能：指定しない場合は自動検出
  - name: GITHUB_APP_PEM
    valueFrom:
      secretKeyRef:
        name: github-app-private-key
        key: private-key
  - name: GITHUB_APP_PEM_PATH
    value: "/etc/github-app/private-key"
```

### ファイルマウント

GitHub App の秘密鍵は環境変数だけでなく、ファイルとしても自動的にマウントされます：

```yaml
# Volume Mount
volumeMounts:
  - name: github-app-private-key
    mountPath: /etc/github-app
    readOnly: true

# Volume
volumes:
  - name: github-app-private-key
    secret:
      secretName: github-app-private-key
      items:
      - key: private-key
        path: private-key
      defaultMode: 0400  # セキュリティのため読み取り専用
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
- **セッションPodへの資格情報の非公開**: GitHub App の設定（`GITHUB_APP_ID` /
  `GITHUB_INSTALLATION_ID` / `REPOSITORY_RESTRICTION`）および秘密鍵 PEM は
  **agentapi-proxy 本体の Pod にのみ**マウント・注入されます。個々のセッション Pod
  には一切渡されません。

### セッション Pod への認証情報の受け渡し（トークンブローカモデル）

セッション Pod（各エージェント実行環境）へは、GitHub App の設定や秘密鍵を一切渡さず、
**agentapi-proxy をトークンブローカとして利用**して短命な installation token をオンデマンドで
取得させます。これにより長時間実行されるセッションでも token 失効後に git/gh が継続利用できます。

#### proxy 側の仕組み

1. proxy は自身が保持する GitHub App 資格情報（App ID・秘密鍵）をプロセス内に閉じ込め、
   セッションごとに session-scoped なブローカクレデンシャル（HMAC で session ID に紐付け）
   を発行します。このクレデンシャルはそのセッションのブローカ endpoint 専用で、
   他の API や他セッションの token 取得には使えません。
2. ブローカ endpoint: `POST /internal/sessions/:sessionId/github-token` が token を返します。
   repository スコープを検証し（対象リポジトリがセッションのリポジトリと一致すること）、
   `REPOSITORY_RESTRICTION` と GitHub Enterprise API base を踏襲します。
3. 発行した installation token は期限手前でサーバ側キャッシュし、期限が近づくと再発行します。
   token/PEM はログ・エラー・返却メッセージのいずれにも出力しません。

#### セッション Pod に渡るもの

セッション設定 env に以下のみが注入されます（`GITHUB_TOKEN` は注入しません）。

| 環境変数 | 説明 |
|----------|------|
| `AGENTAPI_GITHUB_BROKER_URL` | ブローカ endpoint URL（proxy Service 経由）。chart の `kubernetesSession.provisioner.proxyUrl` 未指定時は fullname ベースの in-cluster Service DNS が自動設定されます |
| `AGENTAPI_GITHUB_BROKER_TOKEN` | session-scoped ブーカクレデンシャル（HMAC） |

#### Pod 内の仕組み

- **git credential helper** (`/home/agentapi/.session/git-credential-broker`): `get` 要求時に
  ブローカから有効な token を取得し git credential protocol で渡します。`store`/`erase` は
  no-op で token を永続保存しません。対象 host の既存 helper chain を空エントリで reset して
  broker helper のみを設定するため、他 helper や対話 prompt への暗黙 fallback はありません。
  broker 取得失敗時は非 zero で終了し認証失敗として報告されます。
- **gh wrapper** (`/home/agentapi/.session/bin/gh`): 各 gh 実行時にブローカから token を取得し
  `GH_TOKEN` として real gh に渡します。token はコマンド引数・標準出力に出ず、再帰呼出は
  `AGENTAPI_GH_WRAPPER_ACTIVE` で回避します。real gh は導入前に `exec.LookPath` で解決した
  **絶対パス**を埋め込み、shell-safe な文字種のみ許可するため、PATH 再解決による無限再帰や
  コマンド注入を防ぎます。real gh が解決できない場合は wrapper を有効化せずエラーで中止します。
- **Cache-Control**: broker endpoint の token レスポンス（JSON / raw 双方）は `Cache-Control: no-store`
  を返し、仲介キャッシュによる token の永続化・再利用を防ぎます。

#### broker setup 失敗時の挙動（legacy fallback なし）

broker env が存在するセッションには in-Pod に PEM/token がないため、wrapper/helper の作成や
real gh の解決に失敗した場合は **legacy auth へ暗黙に fallback せず**、clone/setup を明確な
エラーで中止します。legacy path は broker env が完全に不在（個人 token / 認証なし）の場合のみ
使用されます。

#### 各経路の挙動

- **通常 Kubernetes セッション / stock セッション**: ブローカモデルを使用。Pod は token を持たず
  必要時に更新取得するため、長時間セッションでも token 失効後も git/gh が使えます。
- **External Session Manager (ESM)**: リモート Pod がプロキシのブローカに折り返せないため、
  proxy がサーバサイドで token を一度解決して `GITHUB_TOKEN` として埋め込みます（更新不可だが
  App 資格情報はプロキシから一切外れない）。
- **GitHub Enterprise Server**: proxy の `GITHUB_API` を用いて token を発行します。
- **`params.github_token`（個人アクセストークン）**: 従来どおりその token が `GITHUB_TOKEN` として
  使用され、ブローカは関与しません。

これにより、セッション Pod が侵害されても流出するのは短命な token のみ（またはブローカ
  クレデンシャルはそのセッション専用で他用途に使えない）で、GitHub App の秘密鍵や App 設定は
  プロキシ内に保護されます。

> 注: トークン発行失敗時は秘密を含まない明確なエラーとなり、期限切れ token への無言 fallback は
  行いません。

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

#### 4. Private Key Permission Denied エラー

**症状**: `permission denied` エラーまたは `failed to read PEM file` エラー

**原因と対処法**:
```bash
# Pod内でファイルの権限確認
kubectl exec -n agentapi deployment/agentapi-proxy -- ls -la /etc/github-app/private-key

# Pod のセキュリティコンテキスト確認
kubectl get statefulset -n agentapi agentapi-proxy -o yaml | grep -A 10 securityContext

# 秘密鍵の内容確認 (base64でエンコードされている)
kubectl get secret -n agentapi github-app-private-key -o yaml | grep private-key
```

**修正方法**:

この問題は agentapi-proxy v1.x.x 以降で自動的に解決されます。EmptyDir を使用した init container による安全な権限設定が組み込まれています。

### 自動解決の仕組み

1. **EmptyDir Volume の使用**: 
   - 一時的な EmptyDir volume を作成
   - init container で Secret から EmptyDir にファイルをコピー
   - 適切な権限（600）とオーナー（1000:1000）を設定

2. **Init Container の処理**:
```yaml
initContainers:
  - name: setup-github-app-key
    image: busybox:1.35
    command:
      - sh
      - -c
      - |
        echo "Setting up GitHub App private key..."
        cp /tmp/github-app-secret/private-key /etc/github-app/private-key
        chown 1000:1000 /etc/github-app/private-key
        chmod 600 /etc/github-app/private-key
    volumeMounts:
      - name: github-app-private-key-secret
        mountPath: /tmp/github-app-secret
        readOnly: true
      - name: github-app-private-key
        mountPath: /etc/github-app
    securityContext:
      runAsUser: 0  # root で実行
```

3. **メインコンテナでの使用**:
   - メインコンテナは EmptyDir をマウント
   - 非 root ユーザー（1000）で実行
   - 適切な権限でファイルを読み取り可能

### 手動対処（緊急時）

環境変数による秘密鍵指定のフォールバック:
```yaml
env:
  - name: GITHUB_APP_PEM
    valueFrom:
      secretKeyRef:
        name: github-app-private-key
        key: private-key
```

#### 5. ネットワーク接続エラー

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