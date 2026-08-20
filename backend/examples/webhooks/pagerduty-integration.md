# PagerDuty Webhook Integration

このガイドでは、PagerDutyからのインシデントwebhookを受信してagentapi-proxyでセッションを自動作成する方法を説明します。

## 概要

PagerDutyのインシデント通知を処理し、特定の条件（優先度、サービス、アクションなど）に基づいてAIエージェントセッションを自動的に起動できます。

## ユースケース

1. **インシデント対応の自動化**: 新しいインシデントが作成されたときに自動的に調査を開始
2. **エスカレーション時の対応**: インシデントがエスカレートされたときに追加の情報を収集
3. **解決後の分析**: インシデントが解決された後に根本原因分析（RCA）を実施

## セットアップ手順

### 1. Webhookの作成

```bash
curl -X POST https://your-agentapi-server.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PagerDuty Incident Response",
    "type": "custom",
    "signature_header": "X-Signature",
    "triggers": [
      {
        "name": "High Priority Incident Triggered",
        "priority": 10,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event.event_type",
              "operator": "eq",
              "value": "incident.triggered"
            },
            {
              "path": "$.event.data.incident.urgency",
              "operator": "eq",
              "value": "high"
            },
            {
              "path": "$.event.data.incident.service.name",
              "operator": "matches",
              "value": "(Production|API|Database)"
            }
          ]
        },
        "session_config": {
          "environment": {
            "INCIDENT_ID": "{{.event.data.incident.id}}",
            "INCIDENT_URL": "{{.event.data.incident.html_url}}",
            "SERVICE": "{{.event.data.incident.service.name}}"
          },
          "tags": {
            "source": "pagerduty",
            "incident_id": "{{.event.data.incident.id}}",
            "urgency": "{{.event.data.incident.urgency}}",
            "service": "{{.event.data.incident.service.name}}"
          },
          "initial_message_template": "🚨 PagerDuty: High Priority Incident\n\n**Incident Details:**\n- ID: {{.event.data.incident.id}}\n- Title: {{.event.data.incident.title}}\n- Urgency: {{.event.data.incident.urgency}}\n- Service: {{.event.data.incident.service.name}}\n- Status: {{.event.data.incident.status}}\n\n**Assigned To:**\n{{range .event.data.incident.assignments}}- {{.assignee.summary}}\n{{end}}\n\n**Description:**\n{{.event.data.incident.body.details}}\n\n**Tasks:**\n1. Review recent changes to {{.event.data.incident.service.name}}\n2. Check monitoring dashboards for anomalies\n3. Analyze logs for errors\n4. Identify the root cause\n5. Propose a remediation plan\n\n**Links:**\n- [View Incident]({{.event.data.incident.html_url}})",
          "params": {
            "github_token": "ghp_your_github_token_here"
          }
        }
      },
      {
        "name": "Incident Escalated",
        "priority": 5,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event.event_type",
              "operator": "eq",
              "value": "incident.escalated"
            }
          ]
        },
        "session_config": {
          "initial_message_template": "⚡ Incident Escalated\n\nIncident: {{.event.data.incident.title}}\nEscalation Level: {{.event.data.incident.escalation_level}}\n\nThis incident requires immediate attention. Provide additional context and escalation recommendations.",
          "tags": {
            "source": "pagerduty",
            "event_type": "escalated"
          }
        }
      },
      {
        "name": "Incident Resolved - RCA",
        "priority": 15,
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event.event_type",
              "operator": "eq",
              "value": "incident.resolved"
            },
            {
              "path": "$.event.data.incident.urgency",
              "operator": "in",
              "value": ["high", "critical"]
            }
          ]
        },
        "session_config": {
          "initial_message_template": "✅ Incident Resolved - Root Cause Analysis\n\nIncident: {{.event.data.incident.title}}\nResolved By: {{range .event.data.incident.last_status_change_by}}{{.summary}}{{end}}\nDuration: Started at {{.event.data.incident.created_at}}\n\nPerform a root cause analysis:\n1. Summarize what happened\n2. Identify the root cause\n3. Document lessons learned\n4. Recommend preventive measures",
          "tags": {
            "source": "pagerduty",
            "event_type": "resolved",
            "task": "rca"
          }
        }
      }
    ]
  }'
```

**レスポンス例:**

```json
{
  "id": "webhook-pd-123",
  "webhook_url": "https://your-agentapi-server.com/hooks/custom/webhook-pd-123",
  "secret": "pd1234abcd5678ef..."
}
```

### 2. PagerDutyでの設定

#### ステップ1: Webhook Extensionを作成

1. PagerDuty UIで **Integrations** → **Generic Webhooks (v3)** に移動
2. **New Webhook**をクリック
3. 以下を設定:
   - **Webhook URL**: `https://your-agentapi-server.com/hooks/custom/webhook-pd-123`
   - **Scope Type**: Service または Account
   - **Description**: agentapi-proxy webhook
   - **Event Subscription**: 以下のイベントを選択
     - `incident.triggered`
     - `incident.acknowledged`
     - `incident.escalated`
     - `incident.resolved`
   - **Custom Headers**: 署名ヘッダーを追加（後述）

#### ステップ2: サービスに関連付け

1. **Services** → 対象サービスを選択
2. **Integrations**タブ
3. **Add Another Integration**をクリック
4. 作成したWebhook Extensionを選択

### 3. 署名検証の設定

PagerDutyはwebhook v3で署名をサポートしています。

**署名の検証方法:**

PagerDutyは`X-PagerDuty-Signature`ヘッダーで署名を送信します。webhook作成時に`signature_header: "X-PagerDuty-Signature"`を指定することで、プロキシを挟まずに直接PagerDutyからのwebhookを受信できます：

```json
{
  "name": "PagerDuty Webhook",
  "type": "custom",
  "signature_header": "X-PagerDuty-Signature",
  "triggers": [...]
}
```

**署名計算（Python例）:**

```python
import hmac
import hashlib

def verify_pagerduty_signature(secret, payload, signature):
    """
    PagerDuty署名を検証
    """
    expected = hmac.new(
        secret.encode(),
        payload.encode(),
        hashlib.sha256
    ).hexdigest()
    return f'sha256={expected}'
```

**重要**: `signature_header`フィールドを使用することで、PagerDutyの`X-PagerDuty-Signature`ヘッダーを直接使用でき、ヘッダー変換やプロキシは不要です。

### 4. テストペイロード送信

```bash
# 署名を計算
WEBHOOK_SECRET="pd1234abcd5678ef..."
PAYLOAD='{
  "event": {
    "id": "01234567-89ab-cdef-0123-456789abcdef",
    "event_type": "incident.triggered",
    "resource_type": "incident",
    "occurred_at": "2026-01-11T12:30:00Z",
    "agent": {
      "html_url": "https://example.pagerduty.com/users/PXXX",
      "id": "PXXX",
      "self": "https://api.pagerduty.com/users/PXXX",
      "summary": "John Doe",
      "type": "user_reference"
    },
    "client": null,
    "data": {
      "incident": {
        "id": "PINC123",
        "type": "incident",
        "title": "Production API - High Error Rate",
        "urgency": "high",
        "status": "triggered",
        "html_url": "https://example.pagerduty.com/incidents/PINC123",
        "service": {
          "id": "PSERV456",
          "name": "Production API",
          "summary": "Production API"
        },
        "created_at": "2026-01-11T12:30:00Z",
        "body": {
          "type": "incident_body",
          "details": "Error rate exceeded 5% threshold"
        },
        "assignments": [
          {
            "at": "2026-01-11T12:30:00Z",
            "assignee": {
              "id": "PUSER789",
              "type": "user_reference",
              "summary": "Alice Smith",
              "html_url": "https://example.pagerduty.com/users/PUSER789"
            }
          }
        ]
      }
    }
  }
}'

# 署名を計算
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

# Webhookを送信
curl -X POST https://your-agentapi-server.com/hooks/custom/webhook-pd-123 \
  -H "X-Signature: sha256=$SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

## ペイロード例

### Incident Triggered

```json
{
  "event": {
    "id": "01234567-89ab-cdef-0123-456789abcdef",
    "event_type": "incident.triggered",
    "resource_type": "incident",
    "occurred_at": "2026-01-11T12:30:00Z",
    "agent": {
      "html_url": "https://example.pagerduty.com/users/PXXX",
      "id": "PXXX",
      "self": "https://api.pagerduty.com/users/PXXX",
      "summary": "Monitoring System",
      "type": "service_reference"
    },
    "data": {
      "incident": {
        "id": "PINC123",
        "type": "incident",
        "self": "https://api.pagerduty.com/incidents/PINC123",
        "html_url": "https://example.pagerduty.com/incidents/PINC123",
        "number": 1234,
        "title": "Database Connection Pool Exhausted",
        "created_at": "2026-01-11T12:30:00Z",
        "updated_at": "2026-01-11T12:30:00Z",
        "status": "triggered",
        "urgency": "high",
        "priority": {
          "id": "P1",
          "type": "priority",
          "summary": "P1",
          "self": "https://api.pagerduty.com/priorities/P1"
        },
        "service": {
          "id": "PSERV456",
          "type": "service_reference",
          "summary": "Database Service",
          "self": "https://api.pagerduty.com/services/PSERV456",
          "html_url": "https://example.pagerduty.com/services/PSERV456",
          "name": "Database Service"
        },
        "assignments": [
          {
            "at": "2026-01-11T12:30:00Z",
            "assignee": {
              "id": "PUSER789",
              "type": "user_reference",
              "summary": "Alice Smith",
              "self": "https://api.pagerduty.com/users/PUSER789",
              "html_url": "https://example.pagerduty.com/users/PUSER789"
            }
          }
        ],
        "body": {
          "type": "incident_body",
          "details": "Connection pool size exceeded. Current: 150, Max: 100"
        },
        "incident_key": "db-connection-pool-2026-01-11"
      }
    }
  }
}
```

### Incident Acknowledged

```json
{
  "event": {
    "id": "11111111-2222-3333-4444-555555555555",
    "event_type": "incident.acknowledged",
    "resource_type": "incident",
    "occurred_at": "2026-01-11T12:35:00Z",
    "agent": {
      "id": "PUSER789",
      "type": "user_reference",
      "summary": "Alice Smith",
      "html_url": "https://example.pagerduty.com/users/PUSER789"
    },
    "data": {
      "incident": {
        "id": "PINC123",
        "status": "acknowledged",
        "acknowledgements": [
          {
            "at": "2026-01-11T12:35:00Z",
            "acknowledger": {
              "id": "PUSER789",
              "type": "user_reference",
              "summary": "Alice Smith"
            }
          }
        ]
      }
    }
  }
}
```

### Incident Resolved

```json
{
  "event": {
    "id": "22222222-3333-4444-5555-666666666666",
    "event_type": "incident.resolved",
    "resource_type": "incident",
    "occurred_at": "2026-01-11T13:00:00Z",
    "agent": {
      "id": "PUSER789",
      "type": "user_reference",
      "summary": "Alice Smith"
    },
    "data": {
      "incident": {
        "id": "PINC123",
        "status": "resolved",
        "last_status_change_at": "2026-01-11T13:00:00Z",
        "last_status_change_by": [
          {
            "id": "PUSER789",
            "type": "user_reference",
            "summary": "Alice Smith"
          }
        ],
        "resolve_reason": {
          "type": "resolve_reason",
          "incident": "Fixed by restarting connection pool"
        }
      }
    }
  }
}
```

## トリガー条件の例

### 例1: High/Critical優先度のみ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.data.incident.urgency",
        "operator": "in",
        "value": ["high", "critical"]
      }
    ]
  }
}
```

### 例2: 特定サービスのみ

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.data.incident.service.name",
        "operator": "matches",
        "value": "^(Production|Database|API).*"
      }
    ]
  }
}
```

### 例3: エスカレーションレベル

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.event_type",
        "operator": "eq",
        "value": "incident.escalated"
      },
      {
        "path": "$.event.data.incident.escalation_level",
        "operator": "gt",
        "value": 2
      }
    ]
  }
}
```

### 例4: 営業時間外

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event.event_type",
        "operator": "eq",
        "value": "incident.triggered"
      },
      {
        "path": "$.event.occurred_at",
        "operator": "matches",
        "value": "T(1[89]|2[0-3]|0[0-6]):"
      }
    ]
  }
}
```

## 初期メッセージテンプレートの例

### 詳細なインシデント情報

```
🚨 PagerDuty Incident: {{.event.event_type}}

**Incident #{{.event.data.incident.number}}**
Title: {{.event.data.incident.title}}
Status: {{.event.data.incident.status}}
Urgency: {{.event.data.incident.urgency}}

**Service:**
{{.event.data.incident.service.name}}

**Assigned To:**
{{range .event.data.incident.assignments}}
- {{.assignee.summary}}
{{end}}

**Details:**
{{.event.data.incident.body.details}}

**Incident Key:**
{{.event.data.incident.incident_key}}

**Timeline:**
- Created: {{.event.data.incident.created_at}}
- Occurred: {{.event.occurred_at}}

**Actions Required:**
1. Investigate the incident
2. Identify the root cause
3. Implement a fix
4. Update the incident in PagerDuty

[View Incident]({{.event.data.incident.html_url}})
```

### 簡潔な通知

```
⚡ {{.event.data.incident.service.name}}
{{.event.data.incident.title}}
Urgency: {{.event.data.incident.urgency}}
[Details]({{.event.data.incident.html_url}})
```

## ベストプラクティス

1. **適切なイベントの選択**: 必要なイベントのみをサブスクライブ

2. **優先度による分類**: Urgency/Priorityに基づいて異なる対応

3. **自動エスカレーション**: エージェントが解決できない場合のエスカレーションパス

4. **RCAの自動化**: 解決後の根本原因分析を自動化

5. **メトリクスの追跡**: エージェントの対応時間やSuccess Rate を追跡

## トラブルシューティング

### Webhookが受信されない

**確認事項:**
- Webhook ExtensionがServiceに関連付けられているか
- イベントサブスクリプションが正しく設定されているか
- URLが正しく入力されているか

### 署名検証エラー

PagerDutyの署名形式を確認:
- V3 webhookでは署名が必須
- `X-PagerDuty-Signature`ヘッダーを`X-Signature`に変換

### トリガーがマッチしない

JSONPath条件を確認:
- PagerDutyのペイロード構造は深くネストされている
- `$.event.data.incident.*`のパスに注意

## 高度な使用例

### 複数の条件によるルーティング

```json
{
  "triggers": [
    {
      "name": "Database Issues - DBA Team",
      "conditions": {
        "jsonpath": [
          {
            "path": "$.event.data.incident.service.name",
            "operator": "contains",
            "value": "Database"
          }
        ]
      },
      "session_config": {
        "tags": {"team": "dba"}
      }
    },
    {
      "name": "API Issues - Backend Team",
      "conditions": {
        "jsonpath": [
          {
            "path": "$.event.data.incident.service.name",
            "operator": "contains",
            "value": "API"
          }
        ]
      },
      "session_config": {
        "tags": {"team": "backend"}
      }
    }
  ]
}
```

### インシデント重大度による分類

```json
{
  "triggers": [
    {
      "name": "Critical - Immediate Response",
      "priority": 1,
      "conditions": {
        "jsonpath": [
          {
            "path": "$.event.data.incident.priority.summary",
            "operator": "eq",
            "value": "P1"
          }
        ]
      }
    },
    {
      "name": "High - Response within 1 hour",
      "priority": 10,
      "conditions": {
        "jsonpath": [
          {
            "path": "$.event.data.incident.priority.summary",
            "operator": "eq",
            "value": "P2"
          }
        ]
      }
    }
  ]
}
```

## 参考リンク

- [PagerDuty Webhooks v3 Documentation](https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgw-overview)
- [PagerDuty API Reference](https://developer.pagerduty.com/api-reference/)
- [PagerDuty Incident Response](https://support.pagerduty.com/docs/incidents)
- [agentapi-proxy Webhook Documentation](../../docs/custom-webhook-design.md)
