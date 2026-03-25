---
name: schedule-management
description: |
  Manage agentapi-proxy schedules for delayed and recurring session execution.
  Use when you need to: (1) Create one-time or recurring schedules, (2) List existing schedules,
  (3) Update schedule configurations, (4) Delete schedules, (5) Manually trigger schedules,
  (6) Filter schedules by status, scope, or team. Supports cron expressions for recurring tasks
  and ISO 8601 timestamps for one-time delayed execution.
---

# Schedule Management

This skill provides guidance for managing agentapi-proxy schedules that automatically create sessions at specified times or intervals.

## Overview

Schedules enable automatic session creation at specific times or on recurring intervals. Each schedule has:
- **Name**: Descriptive name for the schedule
- **Execution Type**: One-time (scheduled_at) or recurring (cron_expr)
- **Session Config**: Environment variables, tags, and initial messages for created sessions
- **Status**: Active, paused, or completed
- **Scope**: User-level or team-level access

## Core Workflows

### Creating a Schedule

#### One-Time Delayed Execution

First, create a JSON file with your schedule configuration:

```bash
cat > schedule.json <<'EOF'
{
  "name": "Code Review Session",
  "scheduled_at": "2025-01-15T14:00:00Z",
  "session_config": {
    "tags": {
      "repository": "org/repo",
      "task": "code-review"
    },
    "params": {
      "message": "Review all open PRs"
    }
  }
}
EOF

agentapi-proxy client schedule create -f schedule.json
```

#### Recurring Execution (Cron)

```bash
cat > schedule-cron.json <<'EOF'
{
  "name": "Daily Standup Bot",
  "cron_expr": "0 9 * * 1-5",
  "timezone": "America/New_York",
  "session_config": {
    "tags": {
      "repository": "org/standup-bot",
      "type": "standup"
    },
    "params": {
      "message": "Generate daily standup report"
    },
    "environment": {
      "SLACK_WEBHOOK": "https://hooks.slack.com/..."
    }
  }
}
EOF

agentapi-proxy client schedule create -f schedule-cron.json
```

**Timezone Support:**
- `timezone`: IANA timezone name (e.g., "America/New_York", "Europe/London", "Asia/Tokyo")
- Default: "Asia/Tokyo"
- The cron expression is evaluated in the specified timezone

**Common Cron Expressions:**
- `0 9 * * 1-5` - Every weekday at 9:00 AM
- `0 */6 * * *` - Every 6 hours
- `0 0 * * 0` - Every Sunday at midnight
- `30 14 1 * *` - First day of every month at 2:30 PM

**Response:**
```json
{
  "id": "schedule-abc123",
  "name": "Daily Standup Bot",
  "status": "active",
  "cron_expr": "0 9 * * 1-5",
  "created_at": "2024-01-01T12:00:00Z",
  "next_run": "2024-01-02T09:00:00Z"
}
```

### Listing Schedules

```bash
# List all schedules
agentapi-proxy client schedule list

# Note: Filtering by status, scope, or team is done by the API automatically
# based on your authentication and permissions
```

### Getting a Specific Schedule

```bash
agentapi-proxy client schedule get SCHEDULE_ID
```

### Updating a Schedule

```bash
# Update specific fields using apply (patch)
echo '{"status":"paused"}' | agentapi-proxy client schedule apply SCHEDULE_ID

# Or update multiple fields
cat > update.json <<'EOF'
{
  "name": "Updated Schedule Name",
  "status": "paused",
  "cron_expr": "0 10 * * 1-5"
}
EOF

cat update.json | agentapi-proxy client schedule apply SCHEDULE_ID
```

### Deleting a Schedule

```bash
agentapi-proxy client schedule delete SCHEDULE_ID
```

### Manually Triggering a Schedule

**Note:** The `trigger` command is not yet implemented in the CLI client. Use the API directly if you need to manually trigger a schedule:

```bash
curl -X POST https://api.example.com/schedules/SCHEDULE_ID/trigger \
  -H "X-API-Key: YOUR_API_KEY"
```

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "triggered_at": "2024-01-02T15:30:00Z"
}
```

## Use Cases

### 1. Daily Reports

```json
{
  "name": "Daily Team Report",
  "cron_expr": "0 17 * * 1-5",
  "session_config": {
    "tags": {
      "type": "report",
      "team": "engineering"
    },
    "params": {
      "message": "Generate end-of-day team report"
    }
  }
}
```

### 2. Weekly Code Reviews

```json
{
  "name": "Weekly PR Review",
  "cron_expr": "0 10 * * 1",
  "session_config": {
    "tags": {
      "repository": "org/main-repo",
      "type": "code-review"
    },
    "params": {
      "message": "Review all open PRs from last week"
    }
  }
}
```

### 3. Incident Response Drills

```json
{
  "name": "Monthly Incident Drill",
  "cron_expr": "0 14 1 * *",
  "session_config": {
    "tags": {
      "type": "incident-drill",
      "team": "sre"
    },
    "params": {
      "message": "Run incident response drill"
    }
  }
}
```

### 4. Delayed Task Execution

```json
{
  "name": "Deploy After Hours",
  "scheduled_at": "2025-01-15T22:00:00Z",
  "session_config": {
    "tags": {
      "repository": "org/production",
      "type": "deployment"
    },
    "params": {
      "message": "Deploy version 2.0 to production"
    }
  }
}
```

## Schedule Status

- **active**: Schedule is enabled and will execute at the specified time
- **paused**: Schedule is temporarily disabled
- **completed**: One-time schedule that has already executed

## Access Control

- **User Scope**: Only the creating user can access and manage the schedule
- **Team Scope**: All team members can access and manage the schedule
- **Admin**: Can view and manage all schedules

## Reference Documentation

For complete API endpoint documentation and permissions, see:
- [API_REFERENCE.md](../references/API_REFERENCE.md#schedule-management-endpoints) - Complete schedule API reference
- [PERMISSIONS.md](../references/PERMISSIONS.md) - Role-based access control details
