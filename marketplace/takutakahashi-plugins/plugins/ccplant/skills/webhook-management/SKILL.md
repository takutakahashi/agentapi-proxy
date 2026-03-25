---
name: webhook-management
description: |
  Manage agentapi-proxy webhooks for automated session creation.
  Use when you need to: (1) Create GitHub or custom webhooks, (2) Update webhook configurations,
  (3) List existing webhooks, (4) Delete webhooks, (5) Regenerate webhook secrets,
  (6) Configure webhook triggers and conditions. Supports GitHub webhooks and custom webhooks
  with Go template-based filtering.
---

# Webhook Management

This skill provides guidance for managing agentapi-proxy webhooks that automatically create sessions in response to external events.

## ⚠️ Important: Always Use This Skill for Webhook Operations

**When performing any webhook-related operations (creating, updating, listing, deleting, or modifying webhook configurations), always invoke this skill first rather than directly using curl or API calls.**

This ensures:
- Proper authentication and API endpoint configuration
- Correct payload structure and validation
- Access to up-to-date examples and best practices
- Consistent error handling and guidance

## Overview

Webhooks enable automatic session creation when events occur in external systems (GitHub, Slack, Datadog, custom services). Each webhook has:
- **Triggers**: Rules that determine when to create a session
- **Conditions**: Filters based on event properties
- **Session Config**: Environment variables, tags, and initial messages for created sessions

## Core Workflows

### Creating a Webhook

#### GitHub Webhook

First, create a JSON file with your webhook configuration:

```bash
cat > webhook-github.json <<'EOF'
{
  "name": "Pull Request Reviewer",
  "type": "github",
  "github": {
    "allowed_events": ["pull_request"],
    "allowed_repositories": ["owner/repo"]
  },
  "triggers": [
    {
      "name": "PR opened",
      "enabled": true,
      "conditions": {
        "github": {
          "events": ["pull_request"],
          "actions": ["opened"],
          "draft": false
        }
      },
      "session_config": {
        "initial_message_template": "Review PR #{{.pull_request.number}}: {{.pull_request.title}}",
        "tags": {
          "repository": "{{.repository.full_name}}",
          "pr": "{{.pull_request.number}}"
        },
        "reuse_session": false,
        "mount_payload": false
      }
    }
  ],
  "max_sessions": 10,
  "signature_type": "hmac",
  "signature_header": "X-Hub-Signature-256",
  "signature_prefix": "sha256="
}
EOF

agentapi-proxy client webhook create -f webhook-github.json
```

#### Custom Webhook (Slack, Datadog, Sentry, etc.)

```bash
cat > webhook-custom.json <<'EOF'
{
  "name": "Slack Incident Alerts",
  "type": "custom",
  "triggers": [
    {
      "name": "Critical incident",
      "conditions": {
        "go_template": "{{ and (eq .event.type \"incident\") (eq .event.severity \"critical\") }}"
      },
      "session_config": {
        "initial_message_template": "Incident: {{.event.title}}",
        "tags": {
          "source": "slack",
          "severity": "{{.event.severity}}"
        }
      }
    }
  ],
  "max_sessions": 5,
  "signature_type": "hmac",
  "signature_header": "X-Signature"
}
EOF

agentapi-proxy client webhook create -f webhook-custom.json
```

**For Static Token Verification (e.g., Sentry):**

```bash
cat > webhook-sentry.json <<'EOF'
{
  "name": "Sentry Error Alerts",
  "type": "custom",
  "secret": "your-static-secret-token",
  "signature_type": "static",
  "signature_header": "X-Sentry-Token",
  "triggers": [
    {
      "name": "Error event",
      "conditions": {
        "go_template": "{{ eq .event.level \"error\" }}"
      },
      "session_config": {
        "initial_message_template": "Sentry error: {{.event.title}}"
      }
    }
  ]
}
EOF

agentapi-proxy client webhook create -f webhook-sentry.json
```

**Response:**
```json
{
  "id": "webhook-123",
  "webhook_url": "https://api.example.com/hooks/github/webhook-123",
  "secret": "generated-secret-key"
}
```

### Listing Webhooks

```bash
agentapi-proxy client webhook list
```

### Getting a Specific Webhook

```bash
agentapi-proxy client webhook get WEBHOOK_ID
```

### Updating a Webhook

```bash
# Update specific fields using apply (patch)
echo '{"max_sessions":15}' | agentapi-proxy client webhook apply WEBHOOK_ID

# Or update multiple fields
cat > update.json <<'EOF'
{
  "name": "Updated Webhook Name",
  "status": "active"
}
EOF

cat update.json | agentapi-proxy client webhook apply WEBHOOK_ID
```

### Deleting a Webhook

```bash
agentapi-proxy client webhook delete WEBHOOK_ID
```

### Regenerating Webhook Secret

```bash
agentapi-proxy client webhook regenerate-secret WEBHOOK_ID
```

### Testing a Webhook

**Note:** The `test` or `trigger` command is not yet implemented in the CLI client. Use the API directly if you need to test a webhook:

```bash
# Dry run (test without creating a session)
curl -X POST https://api.example.com/webhooks/WEBHOOK_ID/trigger \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "payload": {
      "event": "test_event",
      "severity": "critical"
    },
    "dry_run": true
  }'

# Actual trigger (create a session)
curl -X POST https://api.example.com/webhooks/WEBHOOK_ID/trigger \
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

## Advanced Configuration

### Signature Verification

Webhooks support two types of signature verification:

**1. HMAC Signature (default)**
```json
{
  "signature_type": "hmac",
  "signature_header": "X-Hub-Signature-256",
  "signature_prefix": "sha256="
}
```

The webhook validates HMAC signatures automatically. The `signature_prefix` is auto-detected but can be explicitly set:
- GitHub: `sha256=`
- Slack: `v0=`
- Custom: any prefix or empty string

**2. Static Token**
```json
{
  "signature_type": "static",
  "signature_header": "X-Custom-Token",
  "secret": "your-static-token"
}
```

The webhook compares the header value directly against the secret.

### Concurrency Control

Limit the number of concurrent sessions created by a webhook:

```json
{
  "max_sessions": 10
}
```

Default: 10, Maximum: 100. When the limit is reached, new webhook events are queued.

### Session Reuse

Reuse existing sessions instead of creating new ones:

```json
{
  "session_config": {
    "reuse_session": true,
    "reuse_message_template": "New event: {{.event.title}}"
  }
}
```

When `reuse_session` is enabled:
- If an active session exists matching the same tags, send a new message to it
- If no session exists, create a new one with `initial_message_template`
- Use `reuse_message_template` for messages sent to existing sessions

### Mount Webhook Payload

Mount the webhook payload as a file in the container:

```json
{
  "session_config": {
    "mount_payload": true
  }
}
```

The payload will be available at `/webhook-payload.json` in the session container.

## Reference Documentation

For detailed information, see:
- [WEBHOOK_REFERENCE.md](references/WEBHOOK_REFERENCE.md) - Complete webhook API and configuration
- [WEBHOOK_TRIGGERS.md](references/WEBHOOK_TRIGGERS.md) - Trigger conditions and filtering
- [WEBHOOK_EXAMPLES.md](references/WEBHOOK_EXAMPLES.md) - Integration examples for various services
