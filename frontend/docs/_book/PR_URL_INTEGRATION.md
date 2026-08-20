# PR URL Integration with agentapi-proxy

このドキュメントは、agentapi-ui が PR URL を表示するために agentapi-proxy から期待するリクエスト形式について説明します。

## 概要

agentapi-ui のセッション一覧画面で PR へのリンクを表示するため、セッションのメタデータに `pr_url` フィールドを含める必要があります。

## 期待されるデータ形式

### セッション作成時

セッションを作成する際、または既存のセッションを更新する際に、`metadata` フィールドに `pr_url` を含めてください：

```json
{
  "session_id": "session-001",
  "user_id": "user-alice",
  "status": "active",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z",
  "metadata": {
    "description": "Feature implementation for user authentication",
    "repository": "owner/repo-name",
    "pr_url": "https://github.com/owner/repo-name/pull/123"
  },
  "tags": {
    "project_type": "backend",
    "technology": "Node.js"
  }
}
```

### セッションリスト取得時

`GET /sessions` または検索エンドポイントでセッション一覧を返す際も、同様に `metadata.pr_url` を含めてください：

```json
{
  "sessions": [
    {
      "session_id": "session-001",
      "user_id": "user-alice",
      "status": "active",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z",
      "metadata": {
        "description": "Feature implementation",
        "pr_url": "https://github.com/owner/repo/pull/123"
      }
    }
  ],
  "total": 1,
  "page": 1,
  "limit": 20
}
```

## UI での表示

`metadata.pr_url` が存在する場合：
- 「PR作成」ボタンの代わりに「PR表示」リンクが表示されます
- リンクは紫色のボーダーで表示され、外部リンクアイコンが付きます
- クリックすると新しいタブで PR が開きます

`metadata.pr_url` が存在しない場合：
- 通常通り「PR作成」ボタンが表示されます
- クリックするとチャット画面に遷移し、PR 作成メッセージが送信されます

## 実装の推奨事項

### PR URL の設定タイミング

1. **エージェントが PR を作成した時点で設定**
   - エージェントが `gh pr create` コマンドを実行し、PR URL を取得した際に、セッションのメタデータを更新

2. **PR URL の形式**
   - GitHub の PR URL 形式に従う: `https://github.com/{owner}/{repo}/pull/{number}`
   - 完全な URL を保存（相対パスではなく）

### API エンドポイントの推奨実装

セッションメタデータを更新するためのエンドポイント：

```
PATCH /sessions/{session_id}
Content-Type: application/json

{
  "metadata": {
    "pr_url": "https://github.com/owner/repo/pull/123"
  }
}
```

または、既存のセッション更新エンドポイントがある場合は、それを使用してメタデータを更新してください。

## 注意事項

- `pr_url` は `metadata` オブジェクト内に配置する必要があります
- URL は文字列型で保存してください
- セッションの他のメタデータは保持されるべきです（部分更新をサポート）

## 今後の拡張可能性

将来的に以下の情報も追加することを検討できます：

- `pr_status`: PR のステータス（open, merged, closed）
- `pr_created_at`: PR 作成日時
- `pr_merged_at`: PR マージ日時
- `pr_branch`: PR のブランチ名