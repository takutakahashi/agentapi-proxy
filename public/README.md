# AgentAPI Proxy - Mock API Server (Netlify)

このディレクトリには、Netlify上で動作するAgentAPI ProxyのモックAPIサーバーが含まれています。

## 概要

このモックAPIサーバーは、Netlify Functionsを使用して実際のAgentAPI Proxyと同じエンドポイントを提供し、フロントエンドのテストや開発に使用できます。実際のHTTPリクエストに対してJSONレスポンスを返します。

## 利用可能なエンドポイント

### セッション管理
- `POST /api/start.json` - セッション作成
- `POST /api/start-with-profile.json` - プロファイル使用セッション作成
- `GET /api/search.json` - セッション検索

### プロファイル管理
- `POST /api/profiles/create.json` - プロファイル作成
- `GET /api/profiles/list.json` - プロファイル一覧
- `GET /api/profiles/detail.json` - プロファイル詳細
- `PUT /api/profiles/update.json` - プロファイル更新
- `DELETE /api/profiles/delete.json` - プロファイル削除
- `POST /api/profiles/add-repository.json` - リポジトリ追加
- `POST /api/profiles/add-template.json` - テンプレート追加

### その他
- `GET /api/sessions/example-session.json` - セッション例
- `GET /api/error-examples.json` - エラーレスポンス例

## デプロイ方法

### Netlify

1. このリポジトリをNetlifyにデプロイ
2. Build settingsで以下を設定:
   - Build command: (空欄のまま)
   - Publish directory: `public`
   - Functions directory: `netlify/functions`

デプロイ後、以下のURLでアクセスできます：

- メインページ: `https://your-app.netlify.app/`
- APIエンドポイント: `https://your-app.netlify.app/[endpoint]`

## 使用方法

```bash
# セッション作成
curl -X POST https://your-app.netlify.app/start \
  -H "Content-Type: application/json" \
  -d '{"user_id": "test-user", "environment": {"DEBUG": "true"}}'

# プロファイル一覧取得
curl https://your-app.netlify.app/profiles

# 特定のプロファイル取得
curl https://your-app.netlify.app/profiles/profile-123

# セッション検索
curl https://your-app.netlify.app/search
```

## 注意事項

- これはモックサーバーであり、実際のAPIサーバーではありません
- 実際のデータ操作は行われません（常に同じレスポンスを返します）
- 認証や認可は実装されていません（全てのリクエストが成功します）
- Netlify Functionsを使用して動的にレスポンスを返しますが、データは固定です
- CORSが有効になっているため、どのドメインからでもアクセス可能です

## 実際のAPI

本物のAgentAPI Proxyの使用方法については、[メインのREADME](../README.md)と[API仕様書](../docs/api.md)をご参照ください。