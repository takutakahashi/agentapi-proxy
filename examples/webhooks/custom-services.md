# Custom Services Webhook Integration

このガイドでは、様々なカスタムサービスからのwebhookを統合する方法を説明します。

## 概要

agentapi-proxyのカスタムwebhook機能は、JSONペイロードを送信できる任意のサービスと統合できます。このドキュメントでは、一般的なパターンと複数のサービスの統合例を紹介します。

## 汎用パターン

### 基本的なWebhook作成

```bash
curl -X POST https://your-agentapi-server.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Custom Service Webhook",
    "type": "custom",
    "triggers": [
      {
        "name": "Event Trigger",
        "enabled": true,
        "conditions": {
          "jsonpath": [
            {
              "path": "$.event_type",
              "operator": "eq",
              "value": "deployment"
            }
          ]
        },
        "session_config": {
          "initial_message_template": "Event received: {{.event_type}}",
          "tags": {
            "source": "custom"
          }
        }
      }
    ]
  }'
```

## CI/CD統合

### GitLab CI/CD

**Webhook設定（.gitlab-ci.yml）:**

```yaml
notify_agentapi:
  stage: notify
  only:
    - main
  script:
    - |
      PAYLOAD=$(cat <<EOF
      {
        "event_type": "deployment",
        "pipeline": {
          "id": "$CI_PIPELINE_ID",
          "url": "$CI_PIPELINE_URL",
          "status": "$CI_PIPELINE_STATUS"
        },
        "commit": {
          "sha": "$CI_COMMIT_SHA",
          "message": "$CI_COMMIT_MESSAGE",
          "author": "$CI_COMMIT_AUTHOR"
        },
        "project": {
          "name": "$CI_PROJECT_NAME",
          "url": "$CI_PROJECT_URL"
        },
        "environment": "production",
        "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      }
      EOF
      )
    - |
      SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')
    - |
      curl -X POST "$WEBHOOK_URL" \
        -H "X-Signature: sha256=$SIGNATURE" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD"
  variables:
    WEBHOOK_URL: "https://your-agentapi-server.com/hooks/custom/webhook-id"
    WEBHOOK_SECRET: "$AGENTAPI_WEBHOOK_SECRET"
```

**トリガー条件例:**

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.event_type",
        "operator": "eq",
        "value": "deployment"
      },
      {
        "path": "$.environment",
        "operator": "eq",
        "value": "production"
      },
      {
        "path": "$.pipeline.status",
        "operator": "in",
        "value": ["success", "failed"]
      }
    ]
  }
}
```

### GitHub Actions

**Workflow設定（.github/workflows/notify.yml）:**

```yaml
name: Notify Agent API

on:
  deployment_status:
  workflow_run:
    workflows: ["CI"]
    types:
      - completed

jobs:
  notify:
    runs-on: ubuntu-latest
    steps:
      - name: Send Webhook
        env:
          WEBHOOK_URL: ${{ secrets.AGENTAPI_WEBHOOK_URL }}
          WEBHOOK_SECRET: ${{ secrets.AGENTAPI_WEBHOOK_SECRET }}
        run: |
          PAYLOAD=$(cat <<EOF
          {
            "event_type": "workflow_run",
            "workflow": {
              "name": "${{ github.workflow }}",
              "run_id": "${{ github.run_id }}",
              "run_number": "${{ github.run_number }}",
              "status": "${{ job.status }}"
            },
            "repository": {
              "name": "${{ github.repository }}",
              "url": "${{ github.repositoryUrl }}"
            },
            "commit": {
              "sha": "${{ github.sha }}",
              "message": "${{ github.event.head_commit.message }}",
              "author": "${{ github.event.head_commit.author.name }}"
            },
            "actor": "${{ github.actor }}",
            "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
          }
          EOF
          )

          SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

          curl -X POST "$WEBHOOK_URL" \
            -H "X-Signature: sha256=$SIGNATURE" \
            -H "Content-Type: application/json" \
            -d "$PAYLOAD"
```

### CircleCI

**config.yml:**

```yaml
version: 2.1

jobs:
  notify:
    docker:
      - image: cimg/base:stable
    steps:
      - run:
          name: Send Webhook
          command: |
            PAYLOAD=$(cat <<EOF
            {
              "event_type": "build_complete",
              "build": {
                "number": "$CIRCLE_BUILD_NUM",
                "url": "$CIRCLE_BUILD_URL",
                "status": "$CIRCLE_JOB_STATUS"
              },
              "project": {
                "name": "$CIRCLE_PROJECT_REPONAME",
                "branch": "$CIRCLE_BRANCH"
              },
              "commit": {
                "sha": "$CIRCLE_SHA1",
                "author": "$CIRCLE_USERNAME"
              },
              "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
            }
            EOF
            )

            SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | sed 's/^.* //')

            curl -X POST "$WEBHOOK_URL" \
              -H "X-Signature: sha256=$SIGNATURE" \
              -H "Content-Type: application/json" \
              -d "$PAYLOAD"

workflows:
  build-and-notify:
    jobs:
      - build
      - notify:
          requires:
            - build
```

## モニタリング & アラート統合

### Prometheus Alertmanager

**alertmanager.yml:**

```yaml
receivers:
  - name: 'agentapi-proxy'
    webhook_configs:
      - url: 'https://your-agentapi-server.com/hooks/custom/webhook-id'
        send_resolved: true
        http_config:
          headers:
            X-Signature: 'sha256=SIGNATURE_HERE'

route:
  group_by: ['alertname', 'cluster']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'agentapi-proxy'
```

**ペイロード例:**

```json
{
  "receiver": "agentapi-proxy",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "HighErrorRate",
        "severity": "critical",
        "service": "api"
      },
      "annotations": {
        "summary": "High error rate detected",
        "description": "Error rate is above 5%"
      },
      "startsAt": "2026-01-11T12:30:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://prometheus:9090/..."
    }
  ],
  "groupLabels": {
    "alertname": "HighErrorRate"
  },
  "commonLabels": {
    "alertname": "HighErrorRate",
    "severity": "critical"
  },
  "commonAnnotations": {
    "summary": "High error rate detected"
  },
  "externalURL": "http://alertmanager:9093"
}
```

**トリガー条件:**

```json
{
  "conditions": {
    "jsonpath": [
      {
        "path": "$.status",
        "operator": "eq",
        "value": "firing"
      },
      {
        "path": "$.commonLabels.severity",
        "operator": "in",
        "value": ["critical", "warning"]
      }
    ]
  }
}
```

### Grafana

**Grafana Contact Point設定:**

1. **Alerting** → **Contact points**
2. **New contact point**
3. **Integration**: Webhook
4. **URL**: `https://your-agentapi-server.com/hooks/custom/webhook-id`
5. **HTTP Method**: POST
6. **Custom Headers**:
   ```
   X-Signature: sha256=SIGNATURE
   ```

**ペイロード例:**

```json
{
  "receiver": "agentapi-proxy",
  "status": "firing",
  "orgId": 1,
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "DiskSpaceLow",
        "grafana_folder": "Infrastructure",
        "host": "server-01"
      },
      "annotations": {
        "description": "Disk space is below 10%",
        "summary": "Low disk space on server-01"
      },
      "startsAt": "2026-01-11T12:30:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "dashboardURL": "https://grafana.example.com/d/...",
      "panelURL": "https://grafana.example.com/d/.../...",
      "valueString": "[ var='A' labels={host=server-01} value=8.5 ]"
    }
  ],
  "title": "[FIRING:1] DiskSpaceLow",
  "message": "Disk space is below 10%"
}
```

## カスタムアプリケーション統合

### Node.js

```javascript
const crypto = require('crypto');
const axios = require('axios');

async function sendWebhook(webhookUrl, secret, payload) {
  // 署名を計算
  const payloadStr = JSON.stringify(payload);
  const signature = crypto
    .createHmac('sha256', secret)
    .update(payloadStr)
    .digest('hex');

  // Webhookを送信
  try {
    const response = await axios.post(webhookUrl, payload, {
      headers: {
        'X-Signature': `sha256=${signature}`,
        'Content-Type': 'application/json'
      }
    });
    console.log('Webhook sent successfully:', response.data);
    return response.data;
  } catch (error) {
    console.error('Webhook failed:', error.message);
    throw error;
  }
}

// 使用例
const payload = {
  event_type: 'order_placed',
  order: {
    id: 'ORD-12345',
    customer: 'john@example.com',
    total: 99.99,
    items: 3
  },
  timestamp: new Date().toISOString()
};

sendWebhook(
  'https://your-agentapi-server.com/hooks/custom/webhook-id',
  'your-webhook-secret',
  payload
);
```

### Python

```python
import hmac
import hashlib
import json
import requests
from datetime import datetime

def send_webhook(webhook_url, secret, payload):
    """
    agentapi-proxyにwebhookを送信
    """
    # ペイロードをJSON文字列に変換
    payload_str = json.dumps(payload)

    # 署名を計算
    signature = hmac.new(
        secret.encode(),
        payload_str.encode(),
        hashlib.sha256
    ).hexdigest()

    # Webhookを送信
    headers = {
        'X-Signature': f'sha256={signature}',
        'Content-Type': 'application/json'
    }

    response = requests.post(webhook_url, data=payload_str, headers=headers)
    response.raise_for_status()

    return response.json()

# 使用例
payload = {
    'event_type': 'user_signup',
    'user': {
        'id': 'user-123',
        'email': 'alice@example.com',
        'plan': 'premium'
    },
    'timestamp': datetime.utcnow().isoformat() + 'Z'
}

result = send_webhook(
    'https://your-agentapi-server.com/hooks/custom/webhook-id',
    'your-webhook-secret',
    payload
)
print('Session created:', result['session_id'])
```

### Go

```go
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type WebhookPayload struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp string                 `json:"timestamp"`
}

func sendWebhook(webhookURL, secret string, payload WebhookPayload) error {
	// ペイロードをJSON文字列に変換
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// 署名を計算
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payloadBytes)
	signature := hex.EncodeToString(mac.Sum(nil))

	// Webhookリクエストを作成
	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Signature", fmt.Sprintf("sha256=%s", signature))
	req.Header.Set("Content-Type", "application/json")

	// Webhookを送信
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook failed with status %d", resp.StatusCode)
	}

	return nil
}

func main() {
	payload := WebhookPayload{
		EventType: "task_completed",
		Data: map[string]interface{}{
			"task_id":  "task-789",
			"status":   "success",
			"duration": 45.2,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	err := sendWebhook(
		"https://your-agentapi-server.com/hooks/custom/webhook-id",
		"your-webhook-secret",
		payload,
	)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println("Webhook sent successfully")
	}
}
```

## ベストプラクティス

### 1. ペイロード設計

```json
{
  "event_type": "deployment",
  "version": "1.0",
  "id": "unique-event-id",
  "timestamp": "2026-01-11T12:30:00Z",
  "source": "ci/cd-system",
  "data": {
    "deployment": {
      "id": "deploy-123",
      "environment": "production",
      "status": "success"
    },
    "service": {
      "name": "api-server",
      "version": "v2.1.0"
    }
  },
  "metadata": {
    "triggered_by": "john@example.com",
    "build_number": 456
  }
}
```

**推奨事項:**
- `event_type`フィールドでイベントタイプを明示
- `timestamp`フィールドでイベント発生時刻を記録
- `id`フィールドで重複検出を可能に
- ネストされた構造で関連データをグループ化

### 2. エラーハンドリング

```python
import time
import requests

def send_webhook_with_retry(webhook_url, secret, payload, max_retries=3):
    """
    リトライ機能付きwebhook送信
    """
    for attempt in range(max_retries):
        try:
            result = send_webhook(webhook_url, secret, payload)
            return result
        except requests.exceptions.RequestException as e:
            if attempt < max_retries - 1:
                wait_time = 2 ** attempt  # Exponential backoff
                print(f"Retry {attempt + 1}/{max_retries} after {wait_time}s...")
                time.sleep(wait_time)
            else:
                print(f"Failed after {max_retries} attempts")
                raise
```

### 3. セキュリティ

```python
import os

# 環境変数から安全にsecretを取得
WEBHOOK_SECRET = os.environ.get('AGENTAPI_WEBHOOK_SECRET')
if not WEBHOOK_SECRET:
    raise ValueError('AGENTAPI_WEBHOOK_SECRET environment variable not set')

# Secretをコードにハードコードしない
# ❌ secret = 'my-secret-key'
# ✅ secret = os.environ['AGENTAPI_WEBHOOK_SECRET']
```

### 4. ログとモニタリング

```python
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

def send_webhook_with_logging(webhook_url, secret, payload):
    logger.info(f"Sending webhook: event_type={payload.get('event_type')}")

    try:
        result = send_webhook(webhook_url, secret, payload)
        logger.info(f"Webhook sent successfully: session_id={result.get('session_id')}")
        return result
    except Exception as e:
        logger.error(f"Webhook failed: {e}")
        raise
```

## トラブルシューティング

### 署名検証失敗

```bash
# デバッグ: 署名を手動で計算して確認
PAYLOAD='{"event_type":"test"}'
SECRET="your-webhook-secret"

# 期待される署名を計算
echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET"

# 実際に送信している署名と比較
```

### ペイロード形式エラー

```bash
# JSONフォーマットを検証
echo "$PAYLOAD" | python -m json.tool

# または jq を使用
echo "$PAYLOAD" | jq '.'
```

### タイムアウト

```python
# タイムアウトを延長
response = requests.post(
    webhook_url,
    json=payload,
    headers=headers,
    timeout=30  # 30秒
)
```

## 参考リンク

- [agentapi-proxy Webhook Documentation](../../docs/custom-webhook-design.md)
- [JSONPath Online Evaluator](https://jsonpath.com/)
- [HMAC Signature Calculator](https://www.freeformatter.com/hmac-generator.html)
