# Webhook API Reference

## Table of Contents

- [Webhook Endpoints](#webhook-endpoints)
- [Webhook Types](#webhook-types)
- [Webhook Configuration](#webhook-configuration)
- [Webhook URLs](#webhook-urls)

## Webhook Endpoints

### POST /webhooks

Create a new webhook.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "My Webhook",
  "type": "github" | "custom",
  "status": "active" | "paused",
  "github": {
    "enterprise_url": "https://github.example.com",
    "allowed_events": ["pull_request", "push"],
    "allowed_repositories": ["owner/repo"]
  },
  "triggers": [
    {
      "name": "Trigger Name",
      "priority": 10,
      "enabled": true,
      "conditions": {
        "github": {...},
        "go_template": "..."
      },
      "session_config": {...},
      "stop_on_match": true
    }
  ],
  "session_config": {...},
  "max_sessions": 10
}
```

**Response:**
```json
{
  "id": "webhook-abc-123",
  "name": "My Webhook",
  "user_id": "user-123",
  "type": "github",
  "status": "active",
  "webhook_url": "https://api.example.com/hooks/github/webhook-abc-123",
  "secret": "64-character-hex-string",
  "signature_header": "X-Hub-Signature-256",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d @webhook-config.json
```

### GET /webhooks

List all webhooks.

**Permissions Required:** `session:list`

**Query Parameters:**
- `type`: Filter by webhook type (`github` or `custom`)
- `status`: Filter by status (`active` or `paused`)

**Response:**
```json
{
  "webhooks": [
    {
      "id": "webhook-123",
      "name": "My Webhook",
      "type": "github",
      "status": "active",
      "webhook_url": "https://api.example.com/hooks/github/webhook-123",
      "created_at": "2024-01-01T00:00:00Z",
      "trigger_count": 2,
      "delivery_count": 150,
      "last_delivery": {
        "received_at": "2024-01-15T10:30:00Z",
        "status": "processed",
        "session_id": "session-456"
      }
    }
  ]
}
```

**Example:**
```bash
# List all webhooks
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/webhooks

# List only GitHub webhooks
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/webhooks?type=github"

# List only active webhooks
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/webhooks?status=active"
```

### GET /webhooks/:id

Get a specific webhook.

**Permissions Required:** `session:list`

**Response:**
```json
{
  "id": "webhook-123",
  "name": "My Webhook",
  "user_id": "user-123",
  "type": "github",
  "status": "active",
  "webhook_url": "https://api.example.com/hooks/github/webhook-123",
  "secret": "****last4chars",
  "signature_header": "X-Hub-Signature-256",
  "github": {
    "allowed_events": ["pull_request"],
    "allowed_repositories": ["owner/repo"]
  },
  "triggers": [...],
  "max_sessions": 10,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z",
  "delivery_count": 150,
  "last_delivery": {...}
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/webhooks/webhook-123
```

### PUT /webhooks/:id

Update a webhook.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Updated Name",
  "status": "paused",
  "triggers": [...],
  "max_sessions": 20
}
```

**Response:** Same as GET /webhooks/:id

**Example:**
```bash
curl -X PUT https://api.example.com/webhooks/webhook-123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Updated Webhook",
    "status": "paused"
  }'
```

### DELETE /webhooks/:id

Delete a webhook.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "ok": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/webhooks/webhook-123 \
  -H "X-API-Key: YOUR_API_KEY"
```

### POST /webhooks/:id/regenerate-secret

Regenerate the webhook secret.

**Permissions Required:** `session:create`

**Response:**
```json
{
  "id": "webhook-123",
  "secret": "new-64-character-hex-string",
  "updated_at": "2024-01-15T10:30:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/webhooks/webhook-123/regenerate-secret \
  -H "X-API-Key: YOUR_API_KEY"
```

**Note:** After regenerating, update the webhook configuration in the external service (GitHub, Slack, etc.) with the new secret.

### POST /webhooks/:id/trigger

Trigger a webhook with a test payload.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "payload": {
    "event": "custom_event",
    "data": {
      "key": "value"
    }
  },
  "dry_run": true
}
```

**Parameters:**
- `payload`: The test payload to send to the webhook
- `dry_run`: (optional, default: false) If true, only evaluates triggers and returns what would happen without creating a session

**Response (dry_run=true):**
```json
{
  "matched_triggers": [
    {
      "trigger_name": "Critical incident",
      "would_create_session": true,
      "session_config": {
        "initial_message": "Incident: Test incident",
        "tags": {
          "source": "slack",
          "severity": "critical"
        }
      }
    }
  ],
  "dry_run": true
}
```

**Response (dry_run=false):**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "trigger_name": "Critical incident",
  "created_at": "2024-01-15T10:30:00Z"
}
```

**Example (dry run):**
```bash
curl -X POST https://api.example.com/webhooks/webhook-123/trigger \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "payload": {
      "event": "test_event",
      "severity": "critical"
    },
    "dry_run": true
  }'
```

**Example (actual trigger):**
```bash
curl -X POST https://api.example.com/webhooks/webhook-123/trigger \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "payload": {
      "event": "test_event",
      "severity": "critical"
    },
    "dry_run": false
  }'
```

**Note:** This endpoint does not perform signature verification since it requires API key authentication. Use it to test webhook triggers before configuring them in external services.

## Webhook Types

### GitHub Webhooks

**Type:** `github`

**Webhook URL Format:** `https://api.example.com/hooks/github/{webhook_id}`

**Configuration:**
```json
{
  "type": "github",
  "github": {
    "enterprise_url": "https://github.example.com",
    "allowed_events": ["pull_request", "push", "issues"],
    "allowed_repositories": ["owner/repo1", "owner/repo2"]
  }
}
```

**Signature Verification:**
- Header: `X-Hub-Signature-256` (SHA-256 HMAC)
- Algorithm: `sha256=<hmac_hex>`

### Custom Webhooks

**Type:** `custom`

**Webhook URL Format:** `https://api.example.com/hooks/custom/{webhook_id}`

**Configuration:**
```json
{
  "type": "custom",
  "signature_header": "X-Signature",
  "signature_type": "hmac"
}
```

**Signature Verification:**
- Header: `X-Signature` (configurable)
- Algorithm: `sha256=<hmac_hex>` (default)

## Webhook Configuration

### Status Values

| Status | Description |
|--------|-------------|
| `active` | Webhook is active and will process events |
| `paused` | Webhook is paused and will not process events |

### Signature Types

| Type | Description |
|------|-------------|
| `hmac` | HMAC signature verification (default) |
| `static` | Static token comparison |

### GitHub Configuration

```json
{
  "github": {
    "enterprise_url": "https://github.example.com",
    "allowed_events": ["pull_request", "push"],
    "allowed_repositories": ["owner/repo"]
  }
}
```

**Fields:**
- `enterprise_url`: GitHub Enterprise URL (optional, for GHES)
- `allowed_events`: List of allowed GitHub event types
- `allowed_repositories`: List of allowed repositories (format: `owner/repo`)

### Session Configuration

```json
{
  "session_config": {
    "environment": {
      "GITHUB_TOKEN": "ghp_...",
      "CUSTOM_VAR": "value"
    },
    "tags": {
      "source": "webhook",
      "type": "{{.event_type}}"
    },
    "initial_message_template": "Event: {{.action}} on {{.repository.full_name}}",
    "reuse_message_template": "New event: {{.action}}",
    "reuse_session": false,
    "mount_payload": true,
    "params": {
      "agentapi_version": "latest"
    }
  }
}
```

**Fields:**
- `environment`: Environment variables for the session
- `tags`: Tags for the session (supports Go templates)
- `initial_message_template`: Initial message when creating new session
- `reuse_message_template`: Message when reusing existing session
- `reuse_session`: Whether to reuse existing sessions (default: false)
- `mount_payload`: Whether to mount webhook payload as a file (default: false)
- `params`: Additional session parameters

### Max Sessions

```json
{
  "max_sessions": 10
}
```

Maximum number of concurrent sessions that can be created by this webhook. Default: 10.

## Webhook URLs

After creating a webhook, you'll receive a webhook URL to configure in the external service.

### GitHub Webhook Configuration

1. Go to your GitHub repository → Settings → Webhooks → Add webhook
2. **Payload URL**: `https://api.example.com/hooks/github/webhook-123`
3. **Content type**: `application/json`
4. **Secret**: The secret returned when creating the webhook
5. **Events**: Select the events you want to trigger (must match `allowed_events`)

### Custom Service Configuration

For custom services (Slack, Datadog, etc.):

1. **Webhook URL**: `https://api.example.com/hooks/custom/webhook-123`
2. **Content type**: `application/json`
3. **Secret/Token**: The secret returned when creating the webhook
4. **Headers**: Include `X-Signature: sha256=<hmac_hex>` header

## Error Responses

### 400 Bad Request

Invalid webhook configuration:
```json
{
  "error": "Invalid webhook configuration",
  "details": "at least one trigger is required"
}
```

### 401 Unauthorized

Signature verification failed:
```json
{
  "error": "Signature verification failed"
}
```

### 403 Forbidden

Insufficient permissions:
```json
{
  "error": "Insufficient permissions"
}
```

### 404 Not Found

Webhook not found:
```json
{
  "error": "Webhook not found"
}
```

## Best Practices

1. **Use specific trigger conditions** - Filter events as early as possible to avoid unnecessary session creation
2. **Set reasonable max_sessions** - Prevent resource exhaustion from webhook storms
3. **Use reuse_session sparingly** - Only reuse sessions when you need continuous conversation
4. **Secure your secrets** - Rotate secrets regularly using the regenerate-secret endpoint
5. **Monitor delivery counts** - Check webhook delivery statistics to ensure proper operation
6. **Pause instead of delete** - Pause webhooks temporarily instead of deleting to preserve configuration
