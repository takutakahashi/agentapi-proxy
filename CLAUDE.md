## 前提条件

作業を開始する前に、以下の前提条件を必ず確認してください：

### API 仕様の参照

- **作業開始前に [agentapi の OpenAPI 仕様](https://github.com/coder/agentapi/blob/main/openapi.json) を必ず参照する**
  - API エンドポイント、リクエスト/レスポンス形式、認証方法などを確認
  - UI 実装時に正確な API 仕様に基づいて開発を行う

### 参考資料

- **必要に応じて [agentapi のコード](https://github.com/coder/agentapi) を参考にする**
  - バックエンドの実装詳細を理解する際に参照
  - API の動作を理解するためのリファレンスとして活用

### 開発ワークフロー

- **絶対に main ブランチに直接プッシュしてはいけない**
  - 必ず feature ブランチを作成して作業を行う
  - 変更は feature ブランチにコミット・プッシュする
  - プルリクエストを作成してレビューを受ける
  - main ブランチへの直接プッシュは禁止

- **変更を行った際は忘れずにブランチにプッシュする**
  - コードの変更、新機能の追加、バグ修正などを行った場合は必ずコミットしてブランチにプッシュする
  - プルリクエストを作成して変更内容をレビューしてもらう
  - チーム内での作業状況を共有し、進捗を可視化する

- **API の変更時は必ず OpenAPI 仕様を更新する**
  - 新しいエンドポイントを追加した場合は `spec/openapi.json` に追加する
  - 既存エンドポイントのリクエスト/レスポンス形式を変更した場合も更新する
  - スキーマ（components/schemas）の追加・変更も忘れずに行う
  - タグ（tags）の追加が必要な場合は追加する

### ✅ 作業完了時の必須チェックリスト

作業完了時は以下を**順番に必ず実行**してください。1つでも省略してはいけません：

1. **`make lint` を実行する** - コードの品質チェック
2. **`make test` を実行する** - テストの実行
3. **変更をブランチにプッシュし PR を作成する**
4. **`agentapi-proxy client task create` でユーザータスクを1件作成する**
   - `--task-type user`、`--scope user` を指定する
   - ネクストアクションは最も重要なもの**1つだけ**に絞る（複数作らない）
   - PR を作成した場合は `--link "url|title"` 形式で PR の URL をリンクとして含める
   - 例：PR レビュー依頼タスクの作成
     ```bash
     agentapi-proxy client task create \
       --endpoint http://$AGENTAPI_PROXY_SERVICE_HOST:$AGENTAPI_PROXY_SERVICE_PORT \
       --session-id $AGENTAPI_SESSION_ID \
       --title "PR をレビューしてください" \
       --task-type user \
       --scope user \
       --link "https://github.com/owner/repo/pull/123|PR #123"
     ```
5. **`agentapi-proxy helpers send-notification` で通知を送る**
   - PR を作成した場合は `--url` に PR の URL を指定する
