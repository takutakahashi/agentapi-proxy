# MCP (Model Context Protocol) クイックスタート

このガイドでは、AgentAPI の MCP 機能を使用して、AI エージェントに外部ツールやサービスへのアクセスを提供する方法を説明します。

## MCP とは

MCP (Model Context Protocol) は、AI エージェントが外部ツールやサービスと対話するための標準化されたプロトコルです。AgentAPI では、MCP サーバーを統合することで、エージェントの機能を拡張できます。

## 前提条件

- Go 1.21 以上
- Docker および Docker Compose
- 有効な API キー（OpenAI、Anthropic など）

## セットアップ

### 1. リポジトリのクローン

```bash
git clone https://github.com/takutakahashi/agentapi.git
cd agentapi
```

### 2. 設定ファイルの準備

```bash
# API キーの設定
cp api_keys.example.json api_keys.json
# api_keys.json を編集して、実際の API キーを設定

# MCP 設定
cp config.json.example config.json
# config.json を編集して、MCP サーバーの設定を追加
```

### 3. MCP サーバーの設定

`config.json` に MCP サーバーを追加します：

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": {}
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "your-github-token"
      }
    }
  }
}
```

## 基本的な使い方

### 1. AgentAPI サーバーの起動

```bash
# Docker Compose を使用
docker-compose up -d

# または、ローカルで実行
go run main.go
```

### 2. セッションの作成（MCP 付き）

```bash
curl -X POST http://localhost:8080/v1/sessions \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "model": "gpt-4",
    "provider": "openai",
    "mcpServers": ["filesystem", "github"]
  }'
```

### 3. MCP ツールの使用

セッションが作成されると、エージェントは MCP サーバーが提供するツールを自動的に利用できます：

```bash
# ファイルシステムの操作
curl -X POST http://localhost:8080/v1/sessions/{session_id}/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "content": "List all files in /tmp directory"
  }'

# GitHub の操作
curl -X POST http://localhost:8080/v1/sessions/{session_id}/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "content": "Create a new issue in the repository owner/repo with title 'Bug report'"
  }'
```

## よく使用される MCP サーバー

### 1. ファイルシステムサーバー
- ローカルファイルシステムへの読み書きアクセスを提供
- ディレクトリの一覧表示、ファイルの作成・編集・削除が可能

### 2. GitHub サーバー
- GitHub API へのアクセスを提供
- Issue の作成、PR の管理、リポジトリ情報の取得が可能

### 3. Slack サーバー
- Slack ワークスペースとの統合
- メッセージの送信、チャンネル情報の取得が可能

## 高度な設定

### カスタム MCP サーバーの作成

独自の MCP サーバーを作成することも可能です：

```javascript
// custom-mcp-server.js
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';

const server = new Server({
  name: 'custom-server',
  version: '1.0.0',
}, {
  capabilities: {
    tools: {}
  }
});

// ツールの定義
server.setRequestHandler('tools/list', async () => {
  return {
    tools: [{
      name: 'custom_tool',
      description: 'A custom tool',
      inputSchema: {
        type: 'object',
        properties: {
          input: { type: 'string' }
        }
      }
    }]
  };
});

// ツールの実行
server.setRequestHandler('tools/call', async (request) => {
  if (request.params.name === 'custom_tool') {
    return {
      content: [{
        type: 'text',
        text: `Processed: ${request.params.arguments.input}`
      }]
    };
  }
});

// サーバーの起動
const transport = new StdioServerTransport();
await server.connect(transport);
```

設定に追加：

```json
{
  "mcpServers": {
    "custom": {
      "command": "node",
      "args": ["./custom-mcp-server.js"],
      "env": {}
    }
  }
}
```

## トラブルシューティング

### MCP サーバーが起動しない

1. ログを確認：
```bash
docker-compose logs -f
```

2. Node.js と npm が正しくインストールされているか確認
3. MCP サーバーパッケージがアクセス可能か確認

### ツールが利用できない

1. セッション作成時に `mcpServers` パラメータが正しく指定されているか確認
2. MCP サーバーの設定が `config.json` に正しく記載されているか確認
3. 必要な環境変数（API トークンなど）が設定されているか確認

## 次のステップ

- [API ドキュメント](./api.md) で詳細な API 仕様を確認
- [セッション永続化](./session-persistence.md) について学ぶ
- [MCP SDK ドキュメント](https://modelcontextprotocol.io/docs) で MCP の詳細を確認

## 参考リンク

- [AgentAPI GitHub リポジトリ](https://github.com/takutakahashi/agentapi)
- [Model Context Protocol 公式サイト](https://modelcontextprotocol.io/)
- [MCP サーバーのリスト](https://github.com/modelcontextprotocol/servers)