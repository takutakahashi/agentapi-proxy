# AgentAPI Proxy - Mock API Server

このディレクトリには、AgentAPI Proxyの動作をシミュレートするモックAPIサーバーが含まれています。

## 概要

このモックAPIサーバーは、実際のAgentAPI Proxyと同じエンドポイントを提供し、テストやデモンストレーション用のレスポンスを返します。

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

## GitHub Pages

このモックAPIサーバーはGitHub Pagesでホストされ、以下のURLでアクセスできます：

- メインページ: `https://takutakahashi.github.io/agentapi-proxy/`
- APIエンドポイント: `https://takutakahashi.github.io/agentapi-proxy/api/[endpoint].json`

## 使用方法

```bash
# セッション作成のレスポンス例を取得
curl https://takutakahashi.github.io/agentapi-proxy/api/start.json

# プロファイル一覧のレスポンス例を取得
curl https://takutakahashi.github.io/agentapi-proxy/api/profiles/list.json
```

## 注意事項

- これはモックサーバーであり、実際のAPIサーバーではありません
- 実際のデータ操作は行われません
- 認証や認可は実装されていません
- 全てのレスポンスは静的なJSONファイルです

## 実際のAPI

本物のAgentAPI Proxyの使用方法については、[メインのREADME](../README.md)と[API仕様書](../docs/api.md)をご参照ください。