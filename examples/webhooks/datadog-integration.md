# Datadog Webhook Integration

このガイドでは、Datadogからのアラートwebhookを受信してagentapi-proxyでセッションを自動作成する方法を説明します。

## 概要

Datadogモニターからのアラート通知を処理し、特定の条件（重大度、ホスト、メトリクスなど）に基づいてAIエージェントセッションを自動的に起動できます。

## ユースケース

1. **パフォーマンス問題の自動調査**: CPU/メモリ/ディスク使用率が閾値を超えたときに自動調査
2. **エラー率の急増対応**: エラー率が急上昇したときに原因分析を開始
3. **SLO違反対応**: SLOが違反したときに自動的に対応策を提案

## セットアップ手順

### 1. Webhookの作成

```bash
curl -X POST https://your-agentapi-server.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Datadog Performance Alerts",
    "type": "custom",
    "signature_header": "X-Signature",
    "triggers": [
      {
        "name": "High CPU Alert",
        "priority": 10,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.alert_type",
              "operator": "eq",
              "value": "metric alert"
            },
            {
              "path": "$.metric",
              "operator": "matches",
              "value": "system\\.cpu\\.*"
            },
            {
              "path": "$.current_value",
              "operator": "gt",
              "value": 80
            },
            {
              "path": "$.tags",
              "operator": "contains",
              "value": "env:production"
            }
          ]
        },
        "session_config": {
          "environment": {
            "ALERT_METRIC": "{{.metric}}",
            "CURRENT_VALUE": "{{.current_value}}",
            "THRESHOLD": "{{.threshold}}",
            "HOST": "{{.host}}"
          },
          "tags": {
            "source": "datadog",
            "alert_type": "cpu",
            "severity": "{{.alert_transition}}"
          },
          "initial_message_template": "⚠️ Datadog Alert: High CPU Usage\n\nHost: {{.host}}\nMetric: {{.metric}}\nCurrent Value: {{.current_value}}%\nThreshold: {{.threshold}}%\nStatus: {{.alert_transition}}\n\nTags: {{range .tags}}{{.}} {{end}}\n\nPlease investigate the high CPU usage and provide recommendations to resolve it.",
          "params": {
            "github_token": "ghp_your_github_token_here"
          }
        }
      },
      {
        "name": "Error Rate Spike",
        "priority": 5,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.alert_type",
              "operator": "eq",
              "value": "metric alert"
            },
            {
              "path": "$.metric",
              "operator": "matches",
              "value": ".*error.*rate.*"
            },
            {
              "path": "$.alert_transition",
              "operator": "in",
              "value": ["Triggered", "Re-Triggered"]
            }
          ]
        },
        "session_config": {
          "initial_message_template": "🔴 Error Rate Alert\n\nService: {{.service}}\nError Rate: {{.current_value}}\nBaseline: {{.threshold}}\n\nAnalyze recent deployments and logs to identify the root cause.",
          "tags": {
            "source": "datadog",
            "alert_type": "error_rate"
          }
        }
      },
      {
        "name": "Disk Space Critical",
        "priority": 1,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.metric",
              "operator": "matches",
              "value": "system\\.disk\\..*"
            },
            {
              "path": "$.current_value",
              "operator": "gt",
              "value": 90
            },
            {
              "path": "$.alert_transition",
              "operator": "eq",
              "value": "Triggered"
            }
          ]
        },
        "session_config": {
          "initial_message_template": "💾 Critical: Disk Space Alert\n\nHost: {{.host}}\nDisk Usage: {{.current_value}}%\nThreshold: {{.threshold}}%\n\nURGENT: Identify large files and directories that can be cleaned up immediately.",
          "tags": {
            "source": "datadog",
            "alert_type": "disk",
            "priority": "critical"
          }
        }
      }
    ]
  }'
```

**レスポンス例:**

```json
{
  "id": "webhook-datadog-123",
  "webhook_url": "https://your-agentapi-server.com/hooks/custom/webhook-datadog-123",
  "secret": "dd1234abcd5678ef..."
}
```

### 2. Datadogでの設定

#### ステップ1: Webhook Integrationを追加

1. Datadog UIで **Integrations** → **Integrations** に移動
2. "Webhooks"を検索して選択
3. **Configuration**タブで **New**をクリック
4. 以下を設定:
   - **Name**: `agentapi-proxy`
   - **URL**: `https://your-agentapi-server.com/hooks/custom/webhook-datadog-123`
   - **Custom Headers**:
     ```json
     {
       "X-Signature": "$SIGNATURE",
       "Content-Type": "application/json"
     }
     ```
   - **Payload**: 以下のJSONテンプレート

```json
{
  "alert_id": "$ALERT_ID",
  "alert_title": "$ALERT_TITLE",
  "alert_type": "$ALERT_TYPE",
  "alert_transition": "$ALERT_TRANSITION",
  "alert_status": "$ALERT_STATUS",
  "alert_metric": "$ALERT_METRIC",
  "metric": "$METRIC_NAME",
  "current_value": "$ALERT_METRIC_VALUE",
  "threshold": "$ALERT_THRESHOLD",
  "host": "$HOSTNAME",
  "service": "$SERVICE",
  "tags": $TAGS_JSON,
  "link": "$LINK",
  "snapshot": "$SNAPSHOT",
  "event_msg": "$EVENT_MSG",
  "last_updated": "$LAST_UPDATED",
  "priority": "$PRIORITY"
}
```

#### ステップ2: モニターにWebhookを追加

1. 既存のモニターを開くか、新しいモニターを作成
2. **Say what's happening** セクションで `@webhook-agentapi-proxy` を追加
3. モニターを保存

### 3. 署名の計算

Datadogは標準的なwebhook署名を提供していないため、agentapi-proxyのsecretを使用してカスタム署名を計算する必要があります。

**署名の追加方法:**

1. **Lambda/Cloud Functions経由**（推奨）: Datadogからのwebhookを受け取り、署名を追加してagentapi-proxyに転送
2. **カスタムヘッダー名の使用**: Datadogが独自の署名ヘッダーを使用する場合、`signature_header`フィールドで指定

**Node.jsでの署名計算例:**

```javascript
const crypto = require('crypto');

function generateDatadogSignature(secret, payload) {
  const hmac = crypto.createHmac('sha256', secret);
  hmac.update(JSON.stringify(payload));
  return 'sha256=' + hmac.digest('hex');
}

// 使用例
const secret = 'dd1234abcd5678ef...';
const payload = {
  alert_id: '12345',
  metric: 'system.cpu.user',
  current_value: 95
};

const signature = generateDatadogSignature(secret, payload);
console.log('X-Signature:', signature);
```

**ヒント**: Datadogが独自の署名ヘッダー（例: `X-Datadog-Signature`）を使用する場合、webhook作成時に`"signature_header": "X-Datadog-Signature"`を指定することで、プロキシなしで直接受信できます。

### 4. テストペイロード送信

```bash
# 署名を計算
WEBHOOK_SECRET="dd1234abcd5678ef..."
PAYLOAD='{
  "alert_id": "12345",
  "alert_title": "High CPU on api-server-01",
  "alert_type": "metric alert",
  "alert_transition": "Triggered",
  "metric": "system.cpu.user",
  "current_value": 95.5,
  "threshold": 80,
  "host": "api-server-01",
  "tags": ["env:production", "service:api", "team:backend"]
}'

# 署名を計算
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

# Webhookを送信
curl -X POST https://your-agentapi-server.com/hooks/custom/webhook-datadog-123 \
  -H "X-Signature: sha256=$SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

## ペイロード例

### CPU使用率アラート

```json
{
  "alert_id": "12345",
  "alert_title": "High CPU on api-server-01",
  "alert_type": "metric alert",
  "alert_transition": "Triggered",
  "alert_status": "Alert",
  "alert_metric": "system.cpu.user",
  "metric": "system.cpu.user",
  "current_value": 95.5,
  "threshold": 80.0,
  "host": "api-server-01",
  "service": "api",
  "tags": ["env:production", "service:api", "team:backend", "region:us-east-1"],
  "link": "https://app.datadoghq.com/monitors/12345",
  "snapshot": "https://p.datadoghq.com/snapshot/...",
  "event_msg": "CPU usage is above 80%",
  "last_updated": "2026-01-11T12:30:00Z",
  "priority": "P1"
}
```

### メモリ使用率アラート

```json
{
  "alert_id": "67890",
  "alert_title": "Memory usage critical on db-server-02",
  "alert_type": "metric alert",
  "alert_transition": "Triggered",
  "metric": "system.mem.used_percent",
  "current_value": 92.3,
  "threshold": 85.0,
  "host": "db-server-02",
  "service": "postgres",
  "tags": ["env:production", "service:database", "team:platform"]
}
```

### エラー率アラート

```json
{
  "alert_id": "11111",
  "alert_title": "Error rate spike on payment service",
  "alert_type": "metric alert",
  "alert_transition": "Triggered",
  "metric": "trace.flask.request.errors.rate",
  "current_value": 15.2,
  "threshold": 5.0,
  "service": "payment-service",
  "tags": ["env:production", "service:payment", "team:payments"],
  "event_msg": "Error rate increased from 2% to 15%"
}
```

### APMアラート

```json
{
  "alert_id": "22222",
  "alert_title": "API latency increased",
  "alert_type": "apm alert",
  "alert_transition": "Triggered",
  "metric": "trace.api.request.duration.p99",
  "current_value": 2500,
  "threshold": 1000,
  "service": "api-gateway",
  "tags": ["env:production", "endpoint:/api/users", "team:platform"]
}
```

## トリガー条件の例

### 例1: 本番環境のみ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.tags",
        "operator": "contains",
        "value": "env:production"
      }
    ]
  }
}
```

### 例2: 特定のサービス

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.service",
        "operator": "in",
        "value": ["api", "payment-service", "auth-service"]
      }
    ]
  }
}
```

### 例3: 重大度による分類

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.current_value",
        "operator": "gt",
        "value": 90
      },
      {
        "path": "$.alert_transition",
        "operator": "eq",
        "value": "Triggered"
      }
    ]
  }
}
```

### 例4: 複数メトリクスの組み合わせ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.metric",
        "operator": "matches",
        "value": "(cpu|memory|disk)"
      },
      {
        "path": "$.priority",
        "operator": "in",
        "value": ["P1", "P2"]
      }
    ]
  }
}
```

## 初期メッセージテンプレートの例

### 詳細な調査指示

```
🔍 Datadog Alert: {{.alert_title}}

**Alert Details:**
- Metric: {{.metric}}
- Current Value: {{.current_value}}
- Threshold: {{.threshold}}
- Host: {{.host}}
- Status: {{.alert_transition}}

**Environment:**
Tags: {{range .tags}}
  - {{.}}
{{end}}

**Investigation Tasks:**
1. Check recent deployments to {{.service}}
2. Review logs for {{.host}} in the last hour
3. Analyze resource usage trends
4. Identify potential causes
5. Provide recommendations for remediation

**Links:**
- [View Alert]({{.link}})
- [Snapshot]({{.snapshot}})
```

### 簡潔な調査指示

```
⚠️ {{.alert_title}}

Host: {{.host}}
{{.metric}}: {{.current_value}} (threshold: {{.threshold}})

Investigate and provide immediate recommendations.
```

## ベストプラクティス

1. **タグの活用**: Datadogのタグを使用して適切にフィルタリング

2. **優先度の設定**: 重要度に応じてトリガーの優先度を設定

3. **閾値の調整**: false positiveを避けるため、閾値を適切に設定

4. **テンプレートの最適化**: エージェントが理解しやすい形式でコンテキストを提供

5. **通知のグループ化**: 関連するアラートをグループ化して、重複セッションを避ける

## トラブルシューティング

### Datadogからwebhookが送信されない

**確認事項:**
- モニターに `@webhook-agentapi-proxy` が正しく追加されているか
- Webhook URLが正しいか
- Webhook integrationが有効になっているか

### 署名検証エラー

署名検証を一時的に無効化してテストする（開発環境のみ）:

```bash
# 署名なしでテスト
curl -X POST https://your-agentapi-server.com/hooks/custom/webhook-datadog-123 \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

本番環境では必ず署名検証を有効にしてください。

### ペイロードフォーマットエラー

Datadogのペイロード変数が正しくマッピングされているか確認:
- `$ALERT_METRIC_VALUE` は数値として送信される
- `$TAGS_JSON` はJSON配列として送信される

## 高度な使用例

### Lambda経由での署名追加

AWS Lambdaを使用してDatadog webhookに署名を追加:

```python
import json
import hmac
import hashlib
import urllib3

def lambda_handler(event, context):
    # Datadogからのペイロード
    payload = json.loads(event['body'])

    # agentapi-proxyのsecret
    secret = 'dd1234abcd5678ef...'

    # 署名を計算
    payload_str = json.dumps(payload)
    signature = hmac.new(
        secret.encode(),
        payload_str.encode(),
        hashlib.sha256
    ).hexdigest()

    # agentapi-proxyに転送
    http = urllib3.PoolManager()
    response = http.request(
        'POST',
        'https://your-agentapi-server.com/hooks/custom/webhook-datadog-123',
        body=payload_str,
        headers={
            'X-Signature': f'sha256={signature}',
            'Content-Type': 'application/json'
        }
    )

    return {
        'statusCode': 200,
        'body': json.dumps({'message': 'Forwarded to agentapi-proxy'})
    }
```

## 参考リンク

- [Datadog Webhooks Documentation](https://docs.datadoghq.com/integrations/webhooks/)
- [Datadog Monitors](https://docs.datadoghq.com/monitors/)
- [Datadog Alert Variables](https://docs.datadoghq.com/monitors/notify/variables/)
- [agentapi-proxy Webhook Documentation](../../docs/custom-webhook-design.md)
