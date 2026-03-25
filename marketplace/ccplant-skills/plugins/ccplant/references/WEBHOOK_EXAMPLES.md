# Webhook Integration Examples

## GitHub Pull Request Review

```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PR Review Bot",
    "type": "github",
    "github": {
      "allowed_events": ["pull_request"],
      "allowed_repositories": ["myorg/myrepo"]
    },
    "triggers": [
      {
        "name": "Review needed",
        "conditions": {
          "github": {
            "events": ["pull_request"],
            "actions": ["opened", "synchronize"],
            "draft": false
          }
        },
        "session_config": {
          "initial_message_template": "Review PR #{{.pull_request.number}}: {{.pull_request.title}}",
          "environment": {
            "GITHUB_TOKEN": "ghp_..."
          },
          "tags": {
            "repo": "{{.repository.full_name}}",
            "pr": "{{.pull_request.number}}"
          }
        }
      }
    ]
  }'
```

## Slack Incident Response

```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Slack Incidents",
    "type": "custom",
    "triggers": [
      {
        "name": "Critical incident",
        "conditions": {
          "go_template": "{{ and (eq .event.type \"incident\") (eq .event.severity \"critical\") }}"
        },
        "session_config": {
          "initial_message_template": "Incident: {{.event.title}}\nSeverity: {{.event.severity}}",
          "tags": {"source": "slack"}
        }
      }
    ]
  }'
```

## Datadog Monitoring Alert

```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Datadog Alerts",
    "type": "custom",
    "triggers": [
      {
        "name": "High CPU",
        "conditions": {
          "go_template": "{{ and (eq .alert_type \"metric_alert\") (gt .current_value 90) }}"
        },
        "session_config": {
          "initial_message_template": "CPU Alert: {{.host}} at {{.current_value}}%",
          "environment": {"DATADOG_HOST": "{{.host}}"}
        }
      }
    ]
  }'
```

## PagerDuty Incident

```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PagerDuty",
    "type": "custom",
    "triggers": [
      {
        "name": "Incident triggered",
        "conditions": {
          "go_template": "{{ eq .event.event_type \"incident.triggered\" }}"
        },
        "session_config": {
          "initial_message_template": "Incident {{.event.data.id}}: {{.event.data.title}}",
          "tags": {"incident": "{{.event.data.id}}"}
        }
      }
    ]
  }'
```

## Deployment Success/Failure

```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Deployment Monitor",
    "type": "custom",
    "triggers": [
      {
        "name": "Deployment failed",
        "priority": 100,
        "conditions": {
          "go_template": "{{ and (eq .event \"deployment\") (eq .status \"failed\") }}"
        },
        "session_config": {
          "initial_message_template": "Deployment failed: {{.service}} v{{.version}}",
          "tags": {"env": "{{.environment}}", "status": "failed"}
        },
        "stop_on_match": true
      },
      {
        "name": "Deployment succeeded",
        "priority": 10,
        "conditions": {
          "go_template": "{{ and (eq .event \"deployment\") (eq .status \"success\") }}"
        },
        "session_config": {
          "initial_message_template": "Deployed: {{.service}} v{{.version}} to {{.environment}}",
          "tags": {"env": "{{.environment}}", "status": "success"}
        }
      }
    ]
  }'
```

For detailed configuration options, see [WEBHOOK_REFERENCE.md](WEBHOOK_REFERENCE.md) and [WEBHOOK_TRIGGERS.md](WEBHOOK_TRIGGERS.md).
