# agentapi-proxy における Webhook

agentapi-proxy は、Webhook を AI エージェントと連携させるための強力なプラットフォームです。外部サービスからのイベントを受け取り、自動的に AI エージェントとのセッションを開始できます。

## agentapi-proxy の役割

```
┌──────────────┐
│ 外部サービス  │ (GitHub, Slack, Datadog など)
└──────┬───────┘
       │ Webhook POST
       ↓
┌──────────────────────────────┐
│    agentapi-proxy            │
├──────────────────────────────┤
│ 1. 署名検証                   │
│ 2. トリガー条件の評価          │
│ 3. セッション作成              │
│ 4. 初期メッセージ生成          │
└──────┬───────────────────────┘
       │
       ↓
┌──────────────┐
│ AI エージェント │ (Claude など)
└──────────────┘
```

## 主な機能

### 1. マルチソース対応

agentapi-proxy は、様々な Webhook ソースに対応しています。

#### GitHub Webhook

- プッシュイベント
- プルリクエスト
- イシューの作成・更新
- リリース
- その他多数のイベント

#### Custom Webhook

JSONPath を使った柔軟な条件設定により、任意のサービスと連携可能：

- Slack
- Datadog
- PagerDuty
- カスタムサービス

### 2. 条件ベースのトリガー

特定の条件を満たした場合のみ、セッションを開始できます。

**例: GitHub Webhook**
```json
{
  "conditions": {
    "github": {
      "events": ["pull_request"],
      "actions": ["opened"],
      "branches": ["main", "develop"]
    }
  }
}
```

**例: Custom Webhook**
```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.severity",
        "operator": "eq",
        "value": "critical"
      },
      {
        "path": "$.environment",
        "operator": "eq",
        "value": "production"
      }
    ]
  }
}
```

### 3. 初期メッセージのカスタマイズ

Go テンプレートを使って、ペイロードから情報を抽出し、AI エージェントへの初期メッセージを生成できます。

```go
Critical alert detected!

Severity: {{.severity}}
Environment: {{.environment}}
Service: {{.service.name}}
Message: {{.message}}

Please investigate and provide recommendations.
```

### 4. セキュリティ

- **HMAC 署名検証**: リクエストが正当なソースから送信されたことを確認
- **Secret 管理**: Kubernetes Secret を使った安全な認証情報の管理
- **ユーザー/チームスコープ**: アクセス制御とマルチテナント対応

### 5. 配信記録（Delivery Record）

すべての Webhook リクエストの履歴を記録し、デバッグやトラブルシューティングに活用できます。

- リクエストのペイロード
- レスポンスステータス
- マッチしたトリガー
- 作成されたセッション情報

## ユースケース

### 1. コードレビューの自動化

プルリクエストが作成されたら、AI エージェントが自動的にコードレビューを開始。

```
GitHub PR 作成 → Webhook → agentapi-proxy → AI レビュー開始
```

### 2. インシデント対応

重大なエラーが発生したら、AI エージェントが原因調査と対応策を提案。

```
Datadog アラート → Webhook → agentapi-proxy → AI 分析開始
```

### 3. デプロイメント通知

デプロイが完了したら、変更内容をまとめて関係者に通知。

```
デプロイ完了 → Webhook → agentapi-proxy → AI サマリー作成
```

### 4. チケット自動トリアージ

新しいイシューが作成されたら、AI が自動的に分類とラベル付けを実行。

```
GitHub Issue 作成 → Webhook → agentapi-proxy → AI トリアージ
```

## アーキテクチャの特徴

### クリーンアーキテクチャ

agentapi-proxy は、レイヤー化されたクリーンアーキテクチャを採用しています。

```
┌─────────────────────────────────┐
│  Interface Layer                │
│  - WebhookGitHubController      │
│  - WebhookCustomController      │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│  Domain Layer                   │
│  - Webhook Entity               │
│  - Trigger Conditions           │
│  - Session Config               │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│  Infrastructure Layer           │
│  - WebhookRepository            │
│  - SessionManager               │
│  - SignatureVerifier            │
│  - JSONPathEvaluator            │
└─────────────────────────────────┘
```

### スケーラビリティ

- Kubernetes ネイティブ
- 水平スケーリング対応
- Secret ベースの設定管理

### 拡張性

- 新しい Webhook ソースを簡単に追加
- カスタム条件演算子の実装が容易
- プラグイン可能なアーキテクチャ

## Webhook のライフサイクル

```
1. 作成 (Create)
   ↓
   - Webhook URL が生成される
   - Secret が発行される
   - トリガー条件を設定

2. 設定 (Configure)
   ↓
   - 外部サービスに Webhook URL を登録
   - Secret を設定

3. 受信 (Receive)
   ↓
   - イベント発生
   - agentapi-proxy がリクエストを受信

4. 検証 (Verify)
   ↓
   - 署名を検証
   - ペイロードを解析

5. 評価 (Evaluate)
   ↓
   - トリガー条件をチェック
   - マッチするトリガーを特定

6. 実行 (Execute)
   ↓
   - セッションを作成
   - 初期メッセージを生成
   - AI エージェントに送信

7. 記録 (Record)
   ↓
   - Delivery Record を保存
   - メトリクスを更新
```

## 次のステップ

基本的な概念を理解したら、実際に Webhook を作成してみましょう。

- [Webhook の作成](../setup/README.md) - セットアップの基本
- [GitHub との連携](../github/README.md) - GitHub Webhook の詳細
- [カスタム Webhook](../custom/README.md) - 任意のサービスとの連携

## まとめ

agentapi-proxy は、Webhook と AI エージェントを橋渡しする強力なプラットフォームです。様々な外部サービスからのイベントを受け取り、条件に基づいて自動的に AI エージェントとのセッションを開始できます。

次のセクションでは、実際に Webhook を作成する手順を見ていきましょう。
