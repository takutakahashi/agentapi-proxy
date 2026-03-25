# Webhook Triggers and Conditions

## Overview

Triggers define when a webhook should create a session. Each trigger has:
- **Name**: Human-readable identifier
- **Priority**: Evaluation order (higher = evaluated first)
- **Enabled**: Whether the trigger is active
- **Conditions**: Rules for matching events
- **Session Config**: Configuration for created sessions
- **Stop on Match**: Stop evaluating triggers after this one matches

## Trigger Structure

```json
{
  "name": "Pull Request Opened",
  "priority": 10,
  "enabled": true,
  "conditions": {
    "github": {...},
    "go_template": "..."
  },
  "session_config": {...},
  "stop_on_match": true
}
```

## Condition Types

### GitHub Conditions

For GitHub webhooks only.

```json
{
  "github": {
    "events": ["pull_request", "push"],
    "actions": ["opened", "synchronize"],
    "branches": ["main", "develop"],
    "repositories": ["owner/repo"],
    "labels": ["bug", "feature"],
    "paths": ["src/**", "docs/**"],
    "base_branches": ["main"],
    "draft": false,
    "sender": ["user1", "user2"]
  }
}
```

**Fields:**
- `events`: GitHub event types (e.g., `pull_request`, `push`, `issues`)
- `actions`: Event actions (e.g., `opened`, `closed`, `synchronize`)
- `branches`: Branch names (for push events)
- `repositories`: Repository names (`owner/repo` format)
- `labels`: PR/Issue labels
- `paths`: File paths (glob patterns)
- `base_branches`: Base branch for PRs
- `draft`: Filter draft PRs (true/false/null)
- `sender`: GitHub usernames

### Go Template Conditions

Most flexible option, supports both GitHub and custom webhooks.

```json
{
  "go_template": "{{ and (eq .action \"opened\") (not .pull_request.draft) }}"
}
```

**Available Functions:**
- `eq`, `ne`: Equality comparison
- `and`, `or`, `not`: Logical operators
- `contains`, `hasPrefix`, `hasSuffix`: String functions
- `toLower`, `toUpper`, `trimSpace`: String manipulation
- `len`: Length of string/array
- `in`: Check if value in array
- `matches`: Regex matching

**Examples:**

```go
// PR opened and not draft
{{ and (eq .action "opened") (not .pull_request.draft) }}

// Title contains "feat:" or "fix:"
{{ or (contains .pull_request.title "feat:") (contains .pull_request.title "fix:") }}

// Multiple conditions
{{ and
  (eq .action "opened")
  (not .pull_request.draft)
  (in .pull_request.base.ref (list "main" "develop"))
}}
```

## Priority and Evaluation

Triggers are evaluated in priority order (highest first). If `stop_on_match` is true, evaluation stops after the first match.

```json
{
  "triggers": [
    {
      "name": "Urgent PR",
      "priority": 100,
      "conditions": {
        "github": {
          "labels": ["urgent", "critical"]
        }
      },
      "stop_on_match": true
    },
    {
      "name": "Regular PR",
      "priority": 10,
      "conditions": {
        "github": {
          "events": ["pull_request"],
          "actions": ["opened"]
        }
      }
    }
  ]
}
```

## Examples

### GitHub: PR Review Request

```json
{
  "name": "PR needs review",
  "conditions": {
    "github": {
      "events": ["pull_request"],
      "actions": ["opened", "synchronize"],
      "draft": false,
      "base_branches": ["main"]
    }
  },
  "session_config": {
    "initial_message_template": "Review PR #{{.pull_request.number}}: {{.pull_request.title}}\n\nAuthor: {{.pull_request.user.login}}\nFiles changed: {{.pull_request.changed_files}}",
    "tags": {
      "repository": "{{.repository.full_name}}",
      "pr": "{{.pull_request.number}}"
    }
  }
}
```

### Custom: Slack Incident

```json
{
  "name": "Critical incident",
  "conditions": {
    "go_template": "{{ and (eq .event.type \"incident\") (eq .event.severity \"critical\") (eq .event.environment \"production\") }}"
  },
  "session_config": {
    "initial_message_template": "🚨 Critical Incident\n\nTitle: {{.event.title}}\nEnvironment: {{.event.environment}}\nReported by: {{.user.name}}\n\nInvestigate and respond.",
    "tags": {
      "source": "slack",
      "severity": "critical",
      "incident_id": "{{.event.id}}"
    }
  }
}
```

### Custom: Datadog Alert

```json
{
  "name": "High CPU alert",
  "conditions": {
    "go_template": "{{ and (eq .alert_type \"metric_alert\") (gt .current_value 90) }}"
  },
  "session_config": {
    "initial_message_template": "⚠️ High CPU Alert\n\nHost: {{.host}}\nCurrent: {{.current_value}}%\nThreshold: {{.threshold}}%\n\nScale or investigate.",
    "environment": {
      "DATADOG_HOST": "{{.host}}"
    }
  }
}
```

## Combining Conditions

GitHub conditions and Go template conditions can be combined. They are evaluated with AND logic.

```json
{
  "conditions": {
    "github": {
      "events": ["pull_request"]
    },
    "go_template": "{{ and (gt .pull_request.changed_files 10) (contains .pull_request.title \"refactor\") }}"
  }
}
```

This matches only when:
1. Event is `pull_request` (GitHub condition)
2. Changed files > 10 AND title contains "refactor" (Go template condition)
