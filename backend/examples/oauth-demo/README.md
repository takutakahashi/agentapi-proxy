# GitHub OAuth デモアプリケーション

このディレクトリには、agentapi-proxyのGitHub OAuth認証を試すための簡単なデモアプリケーションが含まれています。

## セットアップ

### 1. GitHub OAuth Appを作成

1. [GitHub Developer Settings](https://github.com/settings/developers)にアクセス
2. "New OAuth App"をクリック
3. 以下の情報を入力：
   - **Application name**: AgentAPI OAuth Demo
   - **Homepage URL**: http://localhost:3000
   - **Authorization callback URL**: http://localhost:3000/examples/oauth-demo/index.html
4. Client IDとClient Secretを保存

### 2. 環境変数を設定

```bash
export GITHUB_CLIENT_ID=your_client_id_here
export GITHUB_CLIENT_SECRET=your_client_secret_here
```

### 3. agentapi-proxyを起動

```bash
# プロジェクトルートから
./bin/agentapi-proxy server --config config.oauth.example.json --port 8080
```

### 4. デモアプリケーションを開く

#### 方法1: ローカルサーバーを使用

```bash
# Python 3を使用
cd examples/oauth-demo
python3 -m http.server 3000

# またはNode.jsを使用
npx http-server -p 3000
```

ブラウザで http://localhost:3000/index.html を開く

#### 方法2: ファイルを直接開く

ブラウザで `file:///path/to/agentapi-proxy/examples/oauth-demo/index.html` を開く

**注意**: この場合、Authorization callback URLを`file://`プロトコルに合わせて変更する必要があります。

## 使い方

1. **プロキシサーバーURL**を確認（デフォルト: http://localhost:8080）
2. **GitHubでログイン**ボタンをクリック
3. GitHubにリダイレクトされるので、アプリケーションを承認
4. 認証成功後、自動的にデモページに戻ります
5. 認証済みの状態で以下が可能：
   - 新しいAgentAPIセッションの作成
   - アクティブなセッション一覧の表示
   - セッショントークンの更新
   - ログアウト

## デモの機能

### 認証フロー
- OAuth認証の開始
- GitHubコールバックの処理
- セキュアなstate検証
- セッション情報の保存

### セッション管理
- セッションの作成
- セッション一覧の取得
- セッションの更新
- ログアウト処理

### デバッグ機能
- リアルタイムログ表示
- エラーハンドリング
- セッション情報の表示

## トラブルシューティング

### "Invalid client"エラー
- Client IDとClient Secretが正しく設定されているか確認
- 環境変数が正しくエクスポートされているか確認

### "Redirect URI mismatch"エラー
- GitHub OAuth Appの設定でAuthorization callback URLを確認
- デモアプリケーションのURLと完全に一致することを確認

### CORSエラー
- agentapi-proxyが起動していることを確認
- プロキシサーバーのURLが正しいことを確認

## カスタマイズ

### プロダクション環境での使用

このデモコードをベースに、以下の改善を加えることを推奨：

1. **セキュリティ強化**
   - HTTPSの使用
   - CSRFトークンの追加検証
   - セッション情報の安全な保存

2. **エラーハンドリング**
   - ネットワークエラーの再試行
   - より詳細なエラーメッセージ

3. **UI/UX改善**
   - ローディング状態の表示
   - より洗練されたデザイン
   - 多言語対応

4. **機能拡張**
   - セッションの詳細情報表示
   - AgentAPIへのプロキシリクエスト
   - リアルタイムステータス更新

## ソースコード

デモのソースコードは単一のHTMLファイル（`index.html`）に含まれています。以下の技術を使用：

- バニラJavaScript（フレームワーク不使用）
- Fetch API（HTTPリクエスト）
- LocalStorage/SessionStorage（データ保存）
- シンプルなCSS（スタイリング）

プロダクション環境では、React、Vue、Angular等のフレームワークを使用することを推奨します。