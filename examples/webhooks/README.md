# Webhook Integration Examples

このディレクトリには、agentapi-proxyのカスタムwebhook機能を使用して様々なサービスと統合するための実用的なサンプルが含まれています。

## 概要

agentapi-proxyのカスタムwebhook機能を使用すると、任意のサービスからのイベントに基づいてAIエージェントセッションを自動的に起動できます。JSONペイロードを送信できるサービスであれば、統合可能です。

## 利用可能なサンプル

### 1. [Slack Integration](./slack-integration.md)

Slackからのwebhookイベントを処理し、インシデント対応やスラッシュコマンドに基づいてエージェントを起動します。

**主な機能:**
- インシデントアラート自動処理
- スラッシュコマンド対応
- チャンネルモニタリング

**ユースケース:**
- 重大なインシデント発生時の自動調査
- `/agent`コマンドによるオンデマンド起動
- 特定チャンネルでのキーワード検出

### 2. [Datadog Integration](./datadog-integration.md)

Datadogモニターからのアラートを処理し、パフォーマンス問題やエラー率の急増に自動対応します。

**主な機能:**
- メトリクスアラート処理
- APMアラート対応
- タグベースのフィルタリング

**ユースケース:**
- CPU/メモリ使用率の異常検出
- エラー率急増時の原因分析
- SLO違反時の自動対応

### 3. [PagerDuty Integration](./pagerduty-integration.md)

PagerDutyインシデントを処理し、トリガー、エスカレーション、解決の各フェーズでエージェントを起動します。

**主な機能:**
- インシデントライフサイクル管理
- 優先度ベースのルーティング
- 解決後の自動RCA

**ユースケース:**
- 高優先度インシデントの即座の対応
- エスカレーション時の追加調査
- インシデント解決後の根本原因分析

### 4. [Custom Services](./custom-services.md)

CI/CDシステム（GitLab、GitHub Actions、CircleCI）やモニタリングツール（Prometheus、Grafana）、カスタムアプリケーションからのwebhook統合例。

**主な機能:**
- CI/CDパイプライン統合
- モニタリングツール連携
- カスタムアプリケーション統合

**ユースケース:**
- デプロイ完了時の自動検証
- ビルド失敗時の原因分析
- カスタムイベントへの対応

## クイックスタート

### 1. Webhookの作成

```bash
curl -X POST https://your-agentapi-server.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My First Webhook",
    "type": "custom",
    "signature_header": "X-Signature",
    "triggers": [
      {
        "name": "Test Trigger",
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event_type",
              "operator": "eq",
              "value": "test"
            }
          ]
        },
        "session_config": {
          "initial_message_template": "Test event received: {{.message}}",
          "tags": {
            "source": "test"
          }
        }
      }
    ]
  }'
```

### 2. Webhookのテスト

```bash
# Webhook作成時に取得したURLとsecretを使用
WEBHOOK_URL="https://your-agentapi-server.com/hooks/custom/webhook-id"
WEBHOOK_SECRET="your-webhook-secret"

# テストペイロード
PAYLOAD='{"event_type":"test","message":"Hello from webhook!"}'

# 署名を計算
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

# Webhookを送信
curl -X POST "$WEBHOOK_URL" \
  -H "X-Signature: sha256=$SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

### 3. レスポンスの確認

成功時のレスポンス:

```json
{
  "message": "Session created",
  "session_id": "session-xyz-789",
  "webhook_id": "webhook-abc-123",
  "trigger_id": "trigger-def-456"
}
```

## 主要な概念

### JSONPath条件

JSONPath式を使用してペイロードの任意のフィールドにアクセスし、条件を評価できます。

**サポートされる演算子:**
- `eq`: 等価
- `ne`: 非等価
- `contains`: 文字列/配列に含まれる
- `matches`: 正規表現マッチ
- `in`: 値が配列に含まれる
- `exists`: パスが存在する

**例:**

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.severity",
        "operator": "in",
        "value": ["critical", "high"]
      },
      {
        "path": "$.event.environment",
        "operator": "eq",
        "value": "production"
      }
    ]
  }
}
```

### 署名検証

セキュリティのため、全てのwebhookリクエストはHMAC署名で検証されます。

**署名の計算:**

```bash
# Bash
echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET"
```

```python
# Python
import hmac
import hashlib

signature = hmac.new(
    secret.encode(),
    payload.encode(),
    hashlib.sha256
).hexdigest()
```

```javascript
// Node.js
const crypto = require('crypto');
const signature = crypto
  .createHmac('sha256', secret)
  .update(payload)
  .digest('hex');
```

**署名ヘッダーのカスタマイズ:**

デフォルトでは`X-Signature`ヘッダーを使用しますが、サービスによって異なるヘッダー名を使用する場合があります。webhook作成時に`signature_header`フィールドで指定できます：

```json
{
  "name": "Slack Webhook",
  "type": "custom",
  "signature_header": "X-Slack-Signature",
  "triggers": [...]
}
```

**サポートされるヘッダー例:**
- `X-Signature` (デフォルト)
- `X-Slack-Signature` (Slack)
- `X-Hub-Signature-256` (GitHub)
- `X-Datadog-Signature` (Datadog)
- その他任意のカスタムヘッダー名

これにより、プロキシを挟まずに直接各サービスからのwebhookを受信できます。

**署名検証タイプ:**

webhook作成時に`signature_type`フィールドで署名検証の方式を指定できます：

1. **`hmac`（デフォルト）**: HMAC-SHA256署名検証
   - 最もセキュアな方式
   - 本番環境で推奨

   ```json
   {
     "name": "Production Webhook",
     "type": "custom",
     "signature_type": "hmac",
     "signature_header": "X-Signature",
     "triggers": [...]
   }
   ```

2. **`static`**: 静的トークン比較
   - シンプルなトークン照合
   - HMAC署名を生成できないサービス向け
   - ヘッダー値とシークレットを直接比較

   ```json
   {
     "name": "Simple Token Webhook",
     "type": "custom",
     "signature_type": "static",
     "signature_header": "Authorization",
     "triggers": [...]
   }
   ```

   リクエスト例：
   ```bash
   curl -X POST https://your-server.com/hooks/custom/webhook-123 \
     -H "Authorization: your-webhook-secret" \
     -H "Content-Type: application/json" \
     -d '{"event": "test"}'
   ```

3. **`none`**: 署名検証なし
   - 開発・テスト環境専用
   - 本番環境では使用しないでください

   ```json
   {
     "name": "Development Webhook",
     "type": "custom",
     "signature_type": "none",
     "triggers": [...]
   }
   ```

   ⚠️ **警告**: `signature_type: "none"`は開発・テスト環境でのみ使用してください。本番環境では必ず`hmac`または`static`を使用してください。

### 初期メッセージテンプレート

Goのtext/templateを使用して、ペイロードデータから動的にメッセージを生成できます。

**例:**

```
🚨 Alert: {{.alert_title}}

**Details:**
- Service: {{.service_name}}
- Severity: {{.severity}}
- Host: {{.host}}

**Values:**
- Current: {{.current_value}}
- Threshold: {{.threshold}}

{{range .tags}}
- Tag: {{.}}
{{end}}

[View Alert]({{.alert_url}})
```

## トリガーの優先度

複数のトリガーがマッチする場合、優先度（小さい数値 = 高優先度）に基づいて評価されます。

```json
{
  "triggers": [
    {
      "name": "Critical - P1",
      "priority": 1,
      "conditions": {...}
    },
    {
      "name": "High - P2",
      "priority": 10,
      "conditions": {...}
    },
    {
      "name": "Medium - P3",
      "priority": 20,
      "conditions": {...}
    }
  ]
}
```

## ベストプラクティス

### 1. 適切な条件設定

不要なセッション作成を避けるため、条件を適切に設定してください。

```json
{
  "conditions": {
    "jsonpath": [
      {"path": "$.environment", "operator": "eq", "value": "production"},
      {"path": "$.severity", "operator": "in", "value": ["critical", "high"]},
      {"path": "$.alert_state", "operator": "eq", "value": "firing"}
    ]
  }
}
```

### 2. 意味のあるタグ

セッションにタグを付けることで、後から分析しやすくなります。

```json
{
  "session_config": {
    "tags": {
      "source": "datadog",
      "alert_type": "cpu",
      "environment": "production",
      "team": "platform"
    }
  }
}
```

### 3. 詳細なテンプレート

エージェントが理解しやすい形式でコンテキストを提供してください。

```
## Alert Details
...

## Investigation Tasks
1. Task 1
2. Task 2
...

## Resources
- [Link 1](url)
- [Link 2](url)
```

### 4. エラーハンドリング

Webhook送信にはリトライロジックを実装してください。

```python
for attempt in range(max_retries):
    try:
        send_webhook(url, secret, payload)
        break
    except Exception as e:
        if attempt < max_retries - 1:
            time.sleep(2 ** attempt)
        else:
            raise
```

## トラブルシューティング

### 署名検証エラー

```json
{
  "error": "Signature verification failed"
}
```

**解決方法:**
- Secretが正しいか確認
- ペイロードが改変されていないか確認
- 署名アルゴリズムがSHA256であることを確認

### トリガーがマッチしない

```json
{
  "message": "No matching trigger",
  "webhook_id": "webhook-abc-123"
}
```

**解決方法:**
- JSONPath条件がペイロード構造と一致するか確認
- [JSONPath Online Evaluator](https://jsonpath.com/)でテスト
- トリガーが有効（enabled: true）か確認

### Webhook URLが見つからない

```json
{
  "error": "Webhook not found"
}
```

**解決方法:**
- Webhook IDが正しいか確認
- Webhookが削除されていないか確認

## 高度なトピック

### 条件の組み合わせ

複数の条件を組み合わせて複雑なロジックを実装できます。

```json
{
  "triggers": [
    {
      "name": "Production Critical Alerts",
      "conditions": {
        "jsonpath": [
          {"path": "$.environment", "operator": "eq", "value": "production"},
          {"path": "$.severity", "operator": "eq", "value": "critical"},
          {"path": "$.service", "operator": "matches", "value": "^(api|database|auth)$"}
        ]
      }
    }
  ]
}
```

### 動的な環境変数

テンプレートを使用して、環境変数を動的に設定できます。

```json
{
  "session_config": {
    "environment": {
      "ALERT_ID": "{{.alert.id}}",
      "SERVICE_NAME": "{{.service.name}}",
      "SEVERITY": "{{.alert.severity}}"
    }
  }
}
```

### カスタム署名ヘッダー

デフォルトでは`X-Signature`ヘッダーを使用しますが、`X-Hub-Signature-256`（GitHub形式）もサポートしています。

## さらに学ぶ

- [Webhook設計ドキュメント](../../docs/custom-webhook-design.md)
- [agentapi-proxy API仕様](../../spec/openapi.json)
- [JSONPath公式ドキュメント](https://goessner.net/articles/JsonPath/)

## サポート

質問やフィードバックがある場合は、[GitHub Issues](https://github.com/takutakahashi/agentapi-proxy/issues)を作成してください。

## ライセンス

このプロジェクトは[MITライセンス](../../LICENSE)の下でライセンスされています。
