## リポジトリの参照先

特に言及のない場合、リポジトリはカレントディレクトリのものを参照しています。

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
また、ローカルリポジトリ配下でない場合からリポジトリをクローンする場合、`--repo-fullname org/repo` の引数を付けてこのコマンドを実行してください。
`xxx/xxx` という形式の文字列を与えられた場合はリポジトリ情報である可能性が高いです。

### ユーザーへの通知

**作業完了後は必ずユーザーに通知を送信してください。**

作業の終了を通知するために `agentapi-proxy client send-notification` コマンドが使用できます。
以下は実行例です。

```bash
agentapi-proxy client send-notification \
  --title "作業が完了しました" \
  --body "作業内容を確認してください" \
  --notify-session-id "$AGENTAPI_SESSION_ID" \
  --url "$NOTIFICATION_BASE_URL/agentapi?session=$AGENTAPI_SESSION_ID"
```

**重要**: 全ての作業が完了した時点で、**必ず**上記コマンドを実行してユーザーに通知を送信してください。

### セッション情報の更新

作業中は、セッションに紐づく情報を `agentapi-proxy client annotate-session` で更新してください。
更新できる情報は `pr_url`, `issue_url`, `description`, `running_task` です。

特に以下のタイミングでは、該当する情報を更新してください：

- PR を作成・更新したとき: `--pr-url`
- 対応する issue があるとき: `--issue-url`
- セッションの目的や要約が明確になったとき: `--description`
- 現在取り組んでいる作業が変わったとき: `--running-task`

例：

```bash
agentapi-proxy client annotate-session \
  --pr-url "https://github.com/owner/repo/pull/123" \
  --issue-url "https://github.com/owner/repo/issues/456" \
  --description "セッションアノテーション機能の実装" \
  --running-task "レビュー指摘の反映"
```

値を空文字で指定すると、その情報をクリアできます。

```bash
agentapi-proxy client annotate-session --running-task ""
```
