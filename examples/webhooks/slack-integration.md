# Slack Webhook Integration

このガイドでは、Slackからのwebhookイベントを受信してagentapi-proxyでセッションを自動作成する方法を説明します。

## 概要

Slackのアウトゴーイングwebhook、スラッシュコマンド、またはWorkflow Builderからのイベントを処理し、特定の条件に基づいてAIエージェントセッションを自動的に起動できます。

## ユースケース

1. **インシデント対応**: 重大なインシデントが発生したときに自動的にエージェントを起動
2. **スラッシュコマンド**: `/agent`コマンドでオンデマンドでエージェントを呼び出し
3. **チャンネル監視**: 特定のチャンネルでのメンションやキーワードに反応

## セットアップ手順

### 1. Webhookの作成

```bash
curl -X POST https://your-agentapi-server.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Slack Incident Response",
    "type": "custom",
    "triggers": [
      {
        "name": "Critical Incident Alert",
        "priority": 10,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event.type",
              "operator": "eq",
              "value": "incident"
            },
            {
              "path": "$.event.severity",
              "operator": "in",
              "value": ["critical", "high"]
            },
            {
              "path": "$.channel.name",
              "operator": "eq",
              "value": "incidents"
            }
          ]
        },
        "session_config": {
          "environment": {
            "SLACK_CHANNEL": "{{.channel.name}}",
            "INCIDENT_ID": "{{.event.incident_id}}"
          },
          "tags": {
            "source": "slack",
            "type": "incident",
            "severity": "{{.event.severity}}"
          },
          "initial_message_template": "🚨 Critical Incident Alert\n\nIncident: {{.event.title}}\nSeverity: {{.event.severity}}\nChannel: #{{.channel.name}}\nReported by: {{.user.name}}\n\nDescription:\n{{.event.description}}\n\nPlease investigate this incident and provide a resolution plan.",
          "params": {
            "github_token": "ghp_your_github_token_here"
          }
        }
      },
      {
        "name": "Agent Slash Command",
        "priority": 20,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.command",
              "operator": "eq",
              "value": "/agent"
            }
          ]
        },
        "session_config": {
          "initial_message_template": "Slack command received from {{.user_name}}: {{.text}}",
          "tags": {
            "source": "slack",
            "type": "slash_command"
          }
        }
      }
    ]
  }'
```

**レスポンス例:**

```json
{
  "id": "webhook-abc-123",
  "name": "Slack Incident Response",
  "type": "custom",
  "webhook_url": "https://your-agentapi-server.com/hooks/custom/webhook-abc-123",
  "secret": "a1b2c3d4e5f6...64文字のHEX文字列",
  "triggers": [...],
  "created_at": "2026-01-11T12:00:00Z"
}
```

### 2. Slackでの設定

#### 方法A: Slack Workflow Builderを使用

1. Slack Workflow Builderを開く
2. 新しいワークフローを作成
3. トリガーを設定（例: 特定のチャンネルへの投稿）
4. ステップを追加: "Send a webhook"
5. Webhook URL: `https://your-agentapi-server.com/hooks/custom/webhook-abc-123`
6. ヘッダーを追加:
   - `X-Signature`: 署名を計算して設定（後述）
   - `Content-Type`: `application/json`
7. ペイロードを設定:

```json
{
  "event": {
    "type": "incident",
    "incident_id": "INC-2026-001",
    "title": "{{workflow.title}}",
    "severity": "critical",
    "description": "{{workflow.description}}"
  },
  "channel": {
    "name": "{{workflow.channel_name}}",
    "id": "{{workflow.channel_id}}"
  },
  "user": {
    "name": "{{workflow.user_name}}",
    "id": "{{workflow.user_id}}"
  },
  "timestamp": "{{workflow.timestamp}}"
}
```

#### 方法B: Slackアプリのスラッシュコマンド

1. Slack APIページで新しいアプリを作成
2. "Slash Commands"を有効化
3. コマンドを作成（例: `/agent`）
4. Request URL: `https://your-agentapi-server.com/hooks/custom/webhook-abc-123`
5. 署名検証を設定（後述）

### 3. 署名検証の実装

Slackは`X-Slack-Signature`ヘッダーで署名を送信します。これを`X-Signature`形式に変換する必要がある場合があります。

**署名計算（Slack Signing Secret使用）:**

```python
import hmac
import hashlib
import time

def generate_slack_signature(webhook_secret, timestamp, body):
    """
    Slackの署名を生成
    webhook_secret: agentapi-proxyのwebhook secret
    timestamp: 現在のUNIXタイムスタンプ
    body: リクエストボディ（JSON文字列）
    """
    sig_basestring = f'v0:{timestamp}:{body}'
    signature = hmac.new(
        webhook_secret.encode(),
        sig_basestring.encode(),
        hashlib.sha256
    ).hexdigest()
    return f'sha256={signature}'
```

**注意**: Slackアプリから直接webhookを送信する場合は、Slackの署名をagentapi-proxy形式に変換するプロキシを挟むか、agentapi-proxyの署名検証を調整する必要があります。

### 4. テストペイロード送信

```bash
# 署名を計算
WEBHOOK_SECRET="a1b2c3d4e5f6..."  # webhook作成時に取得したsecret
PAYLOAD='{"event":{"type":"incident","severity":"critical","title":"Database down"},"channel":{"name":"incidents"},"user":{"name":"john.doe"}}'

# HMAC-SHA256署名を計算
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

# Webhookを送信
curl -X POST https://your-agentapi-server.com/hooks/custom/webhook-abc-123 \
  -H "X-Signature: sha256=$SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

**成功レスポンス:**

```json
{
  "message": "Session created",
  "session_id": "session-xyz-789",
  "webhook_id": "webhook-abc-123",
  "trigger_id": "trigger-def-456"
}
```

## ペイロード例

### インシデントアラート

```json
{
  "event": {
    "type": "incident",
    "incident_id": "INC-2026-001",
    "title": "Production API Down",
    "severity": "critical",
    "description": "All API endpoints returning 503",
    "started_at": "2026-01-11T12:30:00Z"
  },
  "channel": {
    "name": "incidents",
    "id": "C01234567"
  },
  "user": {
    "name": "alice",
    "id": "U01234567",
    "email": "alice@example.com"
  },
  "alert_url": "https://your-monitoring.com/alerts/123"
}
```

### スラッシュコマンド

Slackスラッシュコマンドからのペイロード形式:

```json
{
  "token": "verification_token",
  "team_id": "T01234567",
  "team_domain": "example",
  "channel_id": "C01234567",
  "channel_name": "general",
  "user_id": "U01234567",
  "user_name": "alice",
  "command": "/agent",
  "text": "help me debug the authentication issue",
  "response_url": "https://hooks.slack.com/commands/...",
  "trigger_id": "123.456"
}
```

### メンション通知

```json
{
  "event": {
    "type": "app_mention",
    "text": "<@U01234567> can you help with deployment?",
    "user": "U01234567",
    "channel": "C01234567",
    "ts": "1641902400.000100"
  },
  "team_id": "T01234567",
  "event_time": 1641902400
}
```

## トリガー条件の例

### 例1: 重大度によるフィルタリング

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.severity",
        "operator": "in",
        "value": ["critical", "high"]
      }
    ]
  }
}
```

### 例2: 特定チャンネルのみ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.channel.name",
        "operator": "matches",
        "value": "^(incidents|alerts)$"
      }
    ]
  }
}
```

### 例3: キーワード検出

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.title",
        "operator": "matches",
        "value": "(?i)(down|outage|critical)"
      }
    ]
  }
}
```

### 例4: 営業時間外のみ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.type",
        "operator": "eq",
        "value": "incident"
      },
      {
        "path": "$.metadata.is_business_hours",
        "operator": "eq",
        "value": false
      }
    ]
  }
}
```

## ベストプラクティス

1. **署名検証を必ず有効にする**: セキュリティのため、署名検証は必須です

2. **適切な条件設定**: 不要なセッション作成を避けるため、条件を適切に設定

3. **タグの活用**: セッションにタグを付けることで、後から分析しやすくなります

4. **テンプレートの最適化**: 初期メッセージテンプレートを工夫して、エージェントが文脈を理解しやすくする

5. **エラーハンドリング**: Webhookが失敗した場合の再試行ロジックを実装

## トラブルシューティング

### 署名検証エラー

```json
{
  "error": "Signature verification failed"
}
```

**解決方法:**
- Webhookのsecretが正しいか確認
- 署名計算のアルゴリズムを確認（SHA256推奨）
- ペイロードが改変されていないか確認

### トリガーがマッチしない

```json
{
  "message": "No matching trigger",
  "webhook_id": "webhook-abc-123"
}
```

**解決方法:**
- JSONPath条件を確認
- ペイロード構造がConditionsと一致するか確認
- トリガーが有効（enabled: true）か確認

### Webhookが見つからない

```json
{
  "error": "Webhook not found"
}
```

**解決方法:**
- Webhook IDが正しいか確認
- Webhookが削除されていないか確認

## 参考リンク

- [Slack API Documentation](https://api.slack.com/)
- [Slack Workflow Builder](https://slack.com/help/articles/360035692513)
- [Slack Slash Commands](https://api.slack.com/interactivity/slash-commands)
- [agentapi-proxy Webhook Documentation](../../docs/custom-webhook-design.md)
