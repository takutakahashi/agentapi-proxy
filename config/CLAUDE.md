## 前提条件

作業を開始する前に、以下の前提条件を必ず確認してください：

### default ブランチ

作業をプッシュする際は**必ず**ブランチを切って作業してください。default branch にはプッシュしないでください。

### ツール選定

mise コマンドを利用することができます。言語のインタプリタが存在しない場合や実行したいコマンドが存在しない場合、積極的に mise を活用して作業を実施してください。

### test, lint

リモートにプッシュする前に、ローカルでのテストと lint 実行を徹底してください。

### CI の確認

可能な限り、CI の結果を確認してその後のアクションにつなげてください。

### Git 認証エラーの対処

git の認証に失敗する場合は、以下のコマンドを実行して GitHub 認証をセットアップしてください：

```bash
agentapi-proxy helpers setup-gh
```

このコマンドは自動的に現在のリポジトリの情報を `git config --get remote.origin.url` から取得し、適切な GitHub 認証を設定します。

#### 必要な環境変数

setup-gh コマンドを実行するには、以下のいずれかの認証方法を設定する必要があります：

**パターン1: GitHub Personal Access Token を使用**
```bash
export GITHUB_TOKEN=your_personal_access_token
# または
export GITHUB_PERSONAL_ACCESS_TOKEN=your_personal_access_token
```

**パターン2: GitHub App を使用**
```bash
export GITHUB_APP_ID=your_app_id
export GITHUB_APP_PEM_PATH=/path/to/private-key.pem
# または GITHUB_APP_PEM でキーの内容を直接指定
export GITHUB_APP_PEM="-----BEGIN RSA PRIVATE KEY-----\n..."

# Installation ID は自動検出されますが、手動指定も可能
export GITHUB_INSTALLATION_ID=your_installation_id

# GitHub Enterprise を使用する場合
export GITHUB_API=https://your-github-enterprise.com/api/v3
```

**認証情報の確認**
認証情報が正しく設定されているかは、以下のコマンドで確認できます：
```bash
# トークンが設定されているかチェック
echo $GITHUB_TOKEN
echo $GITHUB_PERSONAL_ACCESS_TOKEN

# GitHub App の設定をチェック
echo $GITHUB_APP_ID
echo $GITHUB_APP_PEM_PATH
```

### ユーザーへの通知

**作業完了後は必ずユーザーに通知を送信してください。**

作業の終了を通知するために `agentapi-proxy helpers send-notification` というヘルパーが使用できます。  
以下は実行例です。  

```
agentapi-proxy helpers send-notification \
  --title "作業が完了しました" \
  --body "作業内容を確認してください" \
  --url "$NOTIFICATION_BASE_URL/agentapi?session={{ session ID }}"
```

**重要**: 全ての作業が完了した時点で、**必ず**上記コマンドを実行してユーザーに通知を送信してください。
