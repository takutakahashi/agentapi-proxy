# agentapi-ui

このリポジトリは、[agentapi](https://github.com/coder/agentapi) の UI リポジトリです。

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

- **main ブランチに直接プッシュしない**
  - main ブランチへの直接的な変更は禁止
  - 必ずブランチを切って作業を行う

- **ブランチの命名規則**
  - ブランチ名は `claude/[session_id]` 形式とする
  - session_id は作業セッションごとに一意の識別子を使用

- **変更を行った際は忘れずにブランチにプッシュする**
  - コードの変更、新機能の追加、バグ修正などを行った場合は必ずコミットしてブランチにプッシュする
  - プルリクエストを作成して変更内容をレビューしてもらう
  - チーム内での作業状況を共有し、進捗を可視化する

- **PR (プルリクエスト) の作成**
  - 変更があった場合は、必ず PR を作成するところまでケアする
  - PR タイトルと説明には変更内容を明確に記載
  - レビュアーへの適切な情報提供を心がける

## プロジェクトの技術仕様と開発環境

### パッケージマネージャー

- **Bun を使用**
  - `bun.lockb` ファイルが存在
  - `npm install` ではなく `bun install` を使用
  - ビルドコマンド: `bun run build`
  - lint コマンド: `bun run lint`
  - TypeScript チェック: `npx tsc --noEmit`

### 開発環境セットアップ

- **mise を活用**
  - `.rtx.toml` ファイルで Node.js バージョン管理
  - `mise trust` で設定ファイルを信頼
  - `mise install` で依存関係をインストール
  - `mise exec -- <command>` でコマンド実行

### デバッグとトラブルシューティング

#### ビルドエラーの対処

- **Node.js/Next.js が見つからない場合**
  ```bash
  mise trust
  mise install
  mise exec -- bun install
  mise exec -- bun run build
  ```

- **型エラーのチェック**
  ```bash
  mise exec -- npx tsc --noEmit
  ```

- **変数名の衝突に注意**
  - 同一スコープ内での `const`/`let` 変数の重複宣言を避ける
  - TypeScript のブロックスコープを理解して適切な変数名を使用

#### 一般的なデバッグ手法

- **ブラウザの開発者ツール**
  - Network タブでAPI リクエスト/レスポンスを確認
  - Console タブでログとエラーメッセージを確認

- **コンソールログの活用**
  - デバッグ時は適切にコンソールログを仕込む
  - プロダクション環境では不要なログを削除

### コード品質維持のベストプラクティス

#### 型安全性の確保

- **Optional chaining の活用**
  - `object?.property` でundefined チェック
  - `array?.length` で配列の存在確認

- **undefined/null チェック**
  - `value && value.trim()` で文字列の存在と空文字チェック
  - `Array.isArray(arr) && arr.length > 0` で配列の有効性チェック

#### コンポーネント設計

- **useEffect の依存関係**
  - ESLint の warnings に注意
  - 必要な依存関係を適切に設定

- **状態管理**
  - useState の初期値を適切に設定
  - 不必要な再レンダリングを避ける

#### API クライアント設計

- **エラーハンドリング**
  - try-catch でAPIエラーを適切にキャッチ
  - ユーザーフレンドリーなエラーメッセージを表示

- **設定の優先順位**
  - 明示的な設定 > プロファイル設定 > デフォルト値の順で適用
  - 設定のマージ時は適切な順序を維持