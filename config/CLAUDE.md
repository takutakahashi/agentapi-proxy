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
