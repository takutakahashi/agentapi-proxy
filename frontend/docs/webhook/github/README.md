# GitHub Webhook

GitHub Webhook 機能を使うと、GitHub からのイベント（プルリクエスト、Issue、プッシュなど）を受け取り、AI エージェントと連携できます。

## 初期メッセージテンプレートで使える変数

GitHub Webhook の初期メッセージテンプレートは、Go Template 形式を使用しています。`{{` と `}}` で囲まれた変数名を使用して、webhook ペイロードから値を取得できます。

### 基本構文

{% raw %}
```
{{.フィールド名}}
{{.オブジェクト名.フィールド名}}
```
{% endraw %}

### 共通フィールド（すべてのイベントで利用可能）

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.action}}</code></td>
<td>イベントのアクション</td>
<td><code>opened</code>, <code>closed</code>, <code>synchronize</code></td>
</tr>
<tr>
<td><code>{{.repository.Name}}</code></td>
<td>リポジトリ名</td>
<td><code>agentapi-ui</code></td>
</tr>
<tr>
<td><code>{{.repository.FullName}}</code></td>
<td>オーナー/リポジトリ名</td>
<td><code>coder/agentapi-ui</code></td>
</tr>
<tr>
<td><code>{{.repository.HTMLURL}}</code></td>
<td>リポジトリのURL</td>
<td><code>https://github.com/coder/agentapi-ui</code></td>
</tr>
<tr>
<td><code>{{.sender.Login}}</code></td>
<td>イベント送信者のユーザー名</td>
<td><code>octocat</code></td>
</tr>
<tr>
<td><code>{{.sender.HTMLURL}}</code></td>
<td>送信者のプロフィールURL</td>
<td><code>https://github.com/octocat</code></td>
</tr>
</tbody>
</table>
{% endraw %}


### Pull Request イベント

`pull_request` イベントで使える変数：

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.pull_request.Number}}</code></td>
<td>プルリクエスト番号</td>
<td><code>123</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Title}}</code></td>
<td>プルリクエストのタイトル</td>
<td><code>Fix: Update user validation</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Body}}</code></td>
<td>プルリクエストの説明</td>
<td><code>This PR fixes...</code></td>
</tr>
<tr>
<td><code>{{.pull_request.State}}</code></td>
<td>プルリクエストの状態</td>
<td><code>open</code>, <code>closed</code></td>
</tr>
<tr>
<td><code>{{.pull_request.HTMLURL}}</code></td>
<td>プルリクエストのURL</td>
<td><code>https://github.com/org/repo/pull/123</code></td>
</tr>
<tr>
<td><code>{{.pull_request.User.Login}}</code></td>
<td>プルリクエスト作成者</td>
<td><code>octocat</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Head.Ref}}</code></td>
<td>ソースブランチ名</td>
<td><code>feature/new-feature</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Head.SHA}}</code></td>
<td>ソースブランチのコミットSHA</td>
<td><code>abc123def456...</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Base.Ref}}</code></td>
<td>ターゲットブランチ名</td>
<td><code>main</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Base.SHA}}</code></td>
<td>ターゲットブランチのコミットSHA</td>
<td><code>def456abc123...</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Draft}}</code></td>
<td>ドラフトかどうか</td>
<td><code>true</code>, <code>false</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Merged}}</code></td>
<td>マージ済みかどうか</td>
<td><code>true</code>, <code>false</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Mergeable}}</code></td>
<td>マージ可能かどうか</td>
<td><code>true</code>, <code>false</code></td>
</tr>
</tbody>
</table>
{% endraw %}


### Issues イベント

`issues` イベントで使える変数：

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.issue.Number}}</code></td>
<td>Issue 番号</td>
<td><code>456</code></td>
</tr>
<tr>
<td><code>{{.issue.Title}}</code></td>
<td>Issue のタイトル</td>
<td><code>Bug: Login fails</code></td>
</tr>
<tr>
<td><code>{{.issue.Body}}</code></td>
<td>Issue の本文</td>
<td><code>Steps to reproduce...</code></td>
</tr>
<tr>
<td><code>{{.issue.State}}</code></td>
<td>Issue の状態</td>
<td><code>open</code>, <code>closed</code></td>
</tr>
<tr>
<td><code>{{.issue.HTMLURL}}</code></td>
<td>Issue のURL</td>
<td><code>https://github.com/org/repo/issues/456</code></td>
</tr>
<tr>
<td><code>{{.issue.User.Login}}</code></td>
<td>Issue 作成者</td>
<td><code>octocat</code></td>
</tr>
<tr>
<td><code>{{.issue.Assignee.Login}}</code></td>
<td>アサインされたユーザー</td>
<td><code>reviewer1</code></td>
</tr>
</tbody>
</table>
{% endraw %}


### Issue Comment イベント

`issue_comment` イベントで使える変数：

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.comment.Body}}</code></td>
<td>コメントの本文</td>
<td><code>Looks good to me!</code></td>
</tr>
<tr>
<td><code>{{.comment.HTMLURL}}</code></td>
<td>コメントのURL</td>
<td><code>https://github.com/org/repo/issues/456#comment-123</code></td>
</tr>
<tr>
<td><code>{{.comment.User.Login}}</code></td>
<td>コメント投稿者</td>
<td><code>octocat</code></td>
</tr>
<tr>
<td><code>{{.issue.Number}}</code></td>
<td>Issue/PR 番号</td>
<td><code>456</code></td>
</tr>
<tr>
<td><code>{{.issue.Title}}</code></td>
<td>Issue/PR のタイトル</td>
<td><code>Bug: Login fails</code></td>
</tr>
</tbody>
</table>
{% endraw %}


### Pull Request Review イベント

`pull_request_review` イベントで使える変数：

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.review.Body}}</code></td>
<td>レビューコメント</td>
<td><code>Overall looks good...</code></td>
</tr>
<tr>
<td><code>{{.review.State}}</code></td>
<td>レビューの状態</td>
<td><code>approved</code>, <code>changes_requested</code>, <code>commented</code></td>
</tr>
<tr>
<td><code>{{.review.HTMLURL}}</code></td>
<td>レビューのURL</td>
<td><code>https://github.com/org/repo/pull/123#review-456</code></td>
</tr>
<tr>
<td><code>{{.review.User.Login}}</code></td>
<td>レビュアー</td>
<td><code>reviewer1</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Number}}</code></td>
<td>プルリクエスト番号</td>
<td><code>123</code></td>
</tr>
<tr>
<td><code>{{.pull_request.Title}}</code></td>
<td>プルリクエストのタイトル</td>
<td><code>Fix: Update user validation</code></td>
</tr>
</tbody>
</table>
{% endraw %}


### Push イベント

`push` イベントで使える変数：

{% raw %}
<table>
<thead>
<tr>
<th>変数</th>
<th>説明</th>
<th>例</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>{{.ref}}</code></td>
<td>ブランチ参照</td>
<td><code>refs/heads/main</code></td>
</tr>
<tr>
<td><code>{{.before}}</code></td>
<td>プッシュ前のコミットSHA</td>
<td><code>abc123...</code></td>
</tr>
<tr>
<td><code>{{.after}}</code></td>
<td>プッシュ後のコミットSHA</td>
<td><code>def456...</code></td>
</tr>
<tr>
<td><code>{{.commits}}</code></td>
<td>コミットの配列</td>
<td><code>[...]</code></td>
</tr>
<tr>
<td><code>{{.head_commit.Message}}</code></td>
<td>最新コミットメッセージ</td>
<td><code>Fix bug in user service</code></td>
</tr>
<tr>
<td><code>{{.head_commit.Author.Name}}</code></td>
<td>コミット作成者名</td>
<td><code>John Doe</code></td>
</tr>
<tr>
<td><code>{{.pusher.Name}}</code></td>
<td>プッシュしたユーザー名</td>
<td><code>octocat</code></td>
</tr>
</tbody>
</table>
{% endraw %}


## 実用例

### 例 1: Pull Request のレビュー依頼

{% raw %}
```
Review PR #{{.pull_request.Number}}: "{{.pull_request.Title}}" in {{.repository.FullName}}

Created by: @{{.pull_request.User.Login}}
Branch: {{.pull_request.Head.Ref}} → {{.pull_request.Base.Ref}}
URL: {{.pull_request.HTMLURL}}
```
{% endraw %}

**出力例:**
```
Review PR #123: "Fix: Update user validation" in coder/agentapi-ui

Created by: @octocat
Branch: feature/validation → main
URL: https://github.com/coder/agentapi-ui/pull/123
```

### 例 2: Issue のトリアージ

{% raw %}
```
New issue #{{.issue.Number}}: {{.issue.Title}}

Reporter: @{{.issue.User.Login}}
Repository: {{.repository.FullName}}
URL: {{.issue.HTMLURL}}

Please review and add appropriate labels.
```
{% endraw %}

**出力例:**
```
New issue #456: Bug: Login fails

Reporter: @octocat
Repository: coder/agentapi-ui
URL: https://github.com/coder/agentapi-ui/issues/456

Please review and add appropriate labels.
```

### 例 3: Pull Request Review コメント

{% raw %}
```
@{{.review.User.Login}} reviewed PR #{{.pull_request.Number}}: {{.review.State}}

PR: "{{.pull_request.Title}}"
Review: {{.review.HTMLURL}}

Please address the feedback and update the PR.
```
{% endraw %}

**出力例:**
```
@reviewer1 reviewed PR #123: changes_requested

PR: "Fix: Update user validation"
Review: https://github.com/coder/agentapi-ui/pull/123#review-456

Please address the feedback and update the PR.
```

### 例 4: 条件分岐を含むテンプレート

Go Template では条件分岐も使用できます：

{% raw %}
```
{{if .pull_request.Draft}}
Draft PR #{{.pull_request.Number}} by @{{.pull_request.User.Login}}
This is a work-in-progress PR. Do not merge yet.
{{else}}
PR #{{.pull_request.Number}} is ready for review
Review "{{.pull_request.Title}}" at {{.pull_request.HTMLURL}}
{{end}}
```
{% endraw %}

### 例 5: 配列要素へのアクセス

{% raw %}
```
Latest commit: {{.head_commit.Message}}
By: {{.head_commit.Author.Name}}
SHA: {{.after}}

Pushed to: {{.ref}}
Repository: {{.repository.FullName}}
```
{% endraw %}

## ベストプラクティス

### 1. 簡潔で明確なメッセージ

エージェントが理解しやすい、明確な指示を含めましょう。

✅ **良い例:**
{% raw %}
```
Review PR #{{.pull_request.Number}}: "{{.pull_request.Title}}"
Check code quality, test coverage, and documentation.
URL: {{.pull_request.HTMLURL}}
```
{% endraw %}

❌ **悪い例:**
{% raw %}
```
PR {{.pull_request.Number}}
```
{% endraw %}

### 2. 必要な情報を含める

エージェントが作業を完了するために必要な情報をすべて含めましょう。

{% raw %}
```
PR #{{.pull_request.Number}} by @{{.pull_request.User.Login}}
Title: {{.pull_request.Title}}
Branch: {{.pull_request.Head.Ref}} → {{.pull_request.Base.Ref}}
URL: {{.pull_request.HTMLURL}}

Tasks:
1. Review code changes
2. Check test coverage
3. Verify documentation updates
4. Add review comments if needed
```
{% endraw %}

### 3. URLを含める

エージェントが直接アクセスできるよう、関連するURLを含めましょう。

{% raw %}
```
{{.pull_request.HTMLURL}}
{{.issue.HTMLURL}}
{{.repository.HTMLURL}}
```
{% endraw %}

### 4. コンテキストを提供

リポジトリ名やユーザー名など、コンテキスト情報を含めましょう。

{% raw %}
```
Repository: {{.repository.FullName}}
Author: @{{.sender.Login}}
Action: {{.action}}
```
{% endraw %}

## デバッグのコツ

### 1. シンプルなテンプレートから始める

最初は基本的な変数だけを使い、徐々に複雑にしていきましょう。

**ステップ 1:**
{% raw %}
```
PR #{{.pull_request.Number}}
```
{% endraw %}

**ステップ 2:**
{% raw %}
```
PR #{{.pull_request.Number}}: {{.pull_request.Title}}
```
{% endraw %}

**ステップ 3:**
{% raw %}
```
Review PR #{{.pull_request.Number}}: "{{.pull_request.Title}}" in {{.repository.FullName}}
```
{% endraw %}

### 2. Delivery Record を確認

Webhook の Delivery Record 機能を使って、実際に受信したペイロードと生成されたメッセージを確認できます。

### 3. 存在しないフィールドに注意

すべてのフィールドが常に存在するわけではありません。オプショナルなフィールド（例: `{{.issue.Assignee.Login}}`）を使う場合は、条件分岐を使用することを検討してください。

{% raw %}
```
{{if .issue.Assignee}}
Assigned to: @{{.issue.Assignee.Login}}
{{else}}
Not assigned yet
{{end}}
```
{% endraw %}

## よくあるエラー

### エラー 1: 大文字小文字の間違い

Go の構造体フィールドは**大文字で始まる**必要があります。

❌ **間違い:**
{% raw %}
```
{{.pull_request.number}}  // 小文字
{{.repository.fullName}}  // キャメルケースの最初は小文字
```
{% endraw %}

✅ **正しい:**
{% raw %}
```
{{.pull_request.Number}}  // 大文字で始まる
{{.repository.FullName}}  // 大文字で始まる
```
{% endraw %}

### エラー 2: 存在しないフィールドへのアクセス

指定したイベントに存在しないフィールドにアクセスするとエラーになります。

❌ **間違い:**
{% raw %}
```
// push イベントで pull_request フィールドにアクセス
{{.pull_request.Number}}
```
{% endraw %}

✅ **正しい:**
{% raw %}
```
// pull_request イベントでのみ使用
{{if .pull_request}}
PR #{{.pull_request.Number}}
{{end}}
```
{% endraw %}

## 関連情報

- [Custom Webhook のテンプレート構文](../custom/go-template.md) - Go Template の詳細な説明
- [GitHub Webhook Events - GitHub Docs](https://docs.github.com/en/webhooks/webhook-events-and-payloads) - 公式ドキュメント
- [Webhook のトラブルシューティング](../troubleshooting/README.md) - 問題解決ガイド

## まとめ

GitHub Webhook の初期メッセージテンプレートを使うことで、各イベントに応じたカスタマイズされたメッセージを AI エージェントに送信できます。適切な変数を使用することで、エージェントが必要な情報をすべて受け取り、効率的に作業を実行できるようになります。
