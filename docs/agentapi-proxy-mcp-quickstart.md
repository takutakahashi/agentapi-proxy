# AgentAPI Proxy MCP Server クイックスタート

このガイドでは、AgentAPI Proxy を MCP (Model Context Protocol) Server として使用し、Claude Desktop や他の MCP クライアントから AgentAPI Proxy の機能を利用する方法を説明します。

## 概要

AgentAPI Proxy MCP Server は、AgentAPI Proxy の機能を MCP プロトコル経由で利用できるようにします。これにより、Claude Desktop などの MCP クライアントから、エージェントセッションの作成、管理、メッセージ送信などが可能になります。

## 前提条件

- Go 1.21 以上
- [AgentAPI Proxy](https://github.com/takutakahashi/agentapi-proxy) が構築済み
- [coder/agentapi](https://github.com/coder/agentapi) バイナリが利用可能
- Claude Desktop または他の MCP クライアント

## セットアップ

### 1. AgentAPI Proxy の構築

```bash
# リポジトリのクローン
git clone https://github.com/takutakahashi/agentapi-proxy.git
cd agentapi-proxy

# 依存関係のインストール
make install-deps

# ビルド
make build
```

### 2. AgentAPI Proxy サーバーの起動

```bash
# AgentAPI Proxy サーバーを起動
./bin/agentapi-proxy server --port 8080 --verbose
```

### 3. MCP Server の起動

別のターミナルで MCP Server を起動します：

```bash
# MCP Server を起動（デフォルトポート 3000）
./bin/agentapi-proxy mcp

# または、カスタムポートとプロキシ URL を指定
./bin/agentapi-proxy mcp --port 3001 --proxy-url http://localhost:8080 --verbose
```

## Claude Desktop での設定

Claude Desktop で AgentAPI Proxy MCP Server を使用するには、`claude_desktop_config.json` に設定を追加します：

### macOS の場合

```bash
# 設定ファイルを編集
nano ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

### Windows の場合

```bash
# 設定ファイルを編集 (PowerShell)
notepad $env:APPDATA\Claude\claude_desktop_config.json
```

### 設定内容

```json
{
  "mcpServers": {
    "agentapi-proxy": {
      "command": "/path/to/agentapi-proxy",
      "args": ["mcp", "--port", "3000", "--proxy-url", "http://localhost:8080"],
      "env": {}
    }
  }
}
```

## 利用可能なツール

AgentAPI Proxy MCP Server は以下のツールを提供します：

### 1. start_session
新しいエージェントセッションを開始します。

**パラメータ:**
- `user_id` (必須): セッションのユーザー ID
- `environment` (オプション): 環境変数のオブジェクト

**使用例:**
```
新しいセッションを開始してください。ユーザー ID は "alice" で、GITHUB_TOKEN を設定してください。
```

### 2. search_sessions
アクティブなセッションを検索します。

**パラメータ:**
- `user_id` (オプション): ユーザー ID でフィルタ
- `status` (オプション): ステータスでフィルタ

**使用例:**
```
ユーザー "alice" のアクティブなセッションを検索してください。
```

### 3. send_message
セッションにメッセージを送信します。

**パラメータ:**
- `session_id` (必須): メッセージを送信するセッション ID
- `message` (必須): 送信するメッセージ内容
- `type` (オプション): メッセージタイプ ("user" または "raw")

**使用例:**
```
セッション "550e8400-e29b-41d4-a716-446655440000" に "Hello, world!" というメッセージを送信してください。
```

### 4. get_messages
セッションの会話履歴を取得します。

**パラメータ:**
- `session_id` (必須): 会話履歴を取得するセッション ID

**使用例:**
```
セッション "550e8400-e29b-41d4-a716-446655440000" の会話履歴を表示してください。
```

### 5. get_status
セッションのステータスを取得します。

**パラメータ:**
- `session_id` (必須): ステータスを取得するセッション ID

**使用例:**
```
セッション "550e8400-e29b-41d4-a716-446655440000" のステータスを確認してください。
```

## 使用例

### 基本的なワークフロー

1. **セッションの開始**
   ```
   ユーザー ID "alice" で新しいセッションを開始してください。GITHUB_TOKEN を "ghp_xxx" に設定してください。
   ```

2. **セッション一覧の確認**
   ```
   現在のアクティブなセッションを表示してください。
   ```

3. **エージェントとの対話**
   ```
   セッション [session_id] に "Pythonでファイルを読み書きするコードを書いてください" というメッセージを送信してください。
   ```

4. **会話履歴の確認**
   ```
   セッション [session_id] の会話履歴を表示してください。
   ```

### 高度な使用例

#### 複数セッションの管理

```
1. ユーザー "developer1" で新しいセッションを開始してください。
2. ユーザー "developer2" で別のセッションを開始してください。
3. 両方のセッションのステータスを確認してください。
```

#### 環境変数付きセッション

```
以下の環境変数でセッションを開始してください：
- ユーザー ID: "data-scientist"
- GITHUB_TOKEN: "ghp_your_token"
- WORKSPACE_NAME: "ml-project"
- DEBUG: "true"
```

## トラブルシューティング

### MCP Server が起動しない

1. **AgentAPI Proxy サーバーが実行中か確認**
   ```bash
   curl http://localhost:8080/health
   ```

2. **ポートの競合確認**
   ```bash
   # 別のポートで起動
   ./bin/agentapi-proxy mcp --port 3001
   ```

3. **詳細ログの確認**
   ```bash
   ./bin/agentapi-proxy mcp --verbose
   ```

### Claude Desktop で認識されない

1. **設定ファイルの構文確認**
   ```bash
   # JSON 構文が正しいか確認
   cat ~/Library/Application\ Support/Claude/claude_desktop_config.json | python -m json.tool
   ```

2. **パスの確認**
   ```bash
   # バイナリパスが正しいか確認
   which agentapi-proxy
   ```

3. **Claude Desktop の再起動**

### セッション作成に失敗する

1. **AgentAPI バイナリの確認**
   ```bash
   # agentapi バイナリが PATH にあるか確認
   which agentapi
   ```

2. **ポート範囲の確認**
   - デフォルトでは 9000 番ポートから開始
   - ファイアウォールやポート制限を確認

3. **権限の確認**
   - プロセス作成権限があるか確認

## 設定オプション

### MCP Server の設定

```bash
# すべてのオプション
./bin/agentapi-proxy mcp --help

# よく使用するオプション
./bin/agentapi-proxy mcp \
  --port 3000 \
  --proxy-url http://localhost:8080 \
  --verbose
```

### AgentAPI Proxy の設定

```json
{
  "start_port": 9000,
  "max_sessions": 100,
  "session_timeout": "1h"
}
```

## 次のステップ

- [AgentAPI Proxy API ドキュメント](./api.md) で詳細な API 仕様を確認
- [セッション永続化](./session-persistence.md) について学ぶ
- [MCP プロトコル](https://modelcontextprotocol.io/) の詳細を確認

## 参考リンク

- [AgentAPI Proxy GitHub リポジトリ](https://github.com/takutakahashi/agentapi-proxy)
- [Coder AgentAPI](https://github.com/coder/agentapi)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [Claude Desktop](https://claude.ai/desktop)

## サポート

問題が発生した場合は、以下の情報を含めて Issue を作成してください：

- AgentAPI Proxy のバージョン
- 使用している OS
- エラーメッセージ
- 設定ファイルの内容
- 実行時のログ