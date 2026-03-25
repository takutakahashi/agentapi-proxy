# agentapi-proxy API Reference

## Table of Contents

- [Session Management Endpoints](#session-management-endpoints)
- [Session Sharing Endpoints](#session-sharing-endpoints)
- [Schedule Management Endpoints](#schedule-management-endpoints)
- [Webhook Management Endpoints](#webhook-management-endpoints)
- [Task Management Endpoints](#task-management-endpoints)
- [Task Group Management Endpoints](#task-group-management-endpoints)
- [Memory Management Endpoints](#memory-management-endpoints)
- [SlackBot Management Endpoints](#slackbot-management-endpoints)
- [User & Settings Endpoints](#user--settings-endpoints)
- [Notification Endpoints](#notification-endpoints)
- [Authentication Endpoints](#authentication-endpoints)

## Session Management Endpoints

### POST /start

Create a new agentapi session.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "environment": {
    "GITHUB_TOKEN": "ghp_token",
    "CUSTOM_VAR": "value"
  },
  "tags": {
    "repository": "agentapi-proxy",
    "branch": "main",
    "env": "production"
  }
}
```

**Note:** `user_id` is automatically assigned from the authenticated user's token.

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/start \
  -H "X-API-Key: ap_user_alice_987654321fedcba" \
  -H "Content-Type: application/json" \
  -d '{
    "environment": {
      "GITHUB_TOKEN": "ghp_..."
    },
    "tags": {
      "repository": "my-project"
    }
  }'
```

### GET /search

Search and filter existing sessions.

**Permissions Required:** `session:list`

**Query Parameters:**
- `status`: Filter by session status (e.g., `active`)
- `tag.{key}`: Filter by tag key-value pairs (e.g., `tag.repository=my-repo`)

**Response:**
```json
{
  "sessions": [
    {
      "session_id": "abc123",
      "user_id": "alice",
      "status": "active",
      "started_at": "2024-01-01T12:00:00Z",
      "port": 9000,
      "tags": {
        "repository": "agentapi-proxy",
        "branch": "main"
      }
    }
  ]
}
```

**Examples:**
```bash
# List all sessions
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/search

# Filter by status
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/search?status=active"

# Filter by multiple tags
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/search?tag.repository=my-repo&tag.env=production"
```

**Access Control:**
- Non-admin users can only see their own sessions
- Admin users can see all sessions

### DELETE /sessions/:sessionId

Delete a specific session.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "ok": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/sessions/550e8400-e29b-41d4-a716-446655440000 \
  -H "X-API-Key: ap_user_alice_987654321fedcba"
```

### GET/POST /:sessionId/:path

Proxy requests to the agentapi instance for the specified session.

**Permissions Required:** `session:access`

**Parameters:**
- `sessionId` (path): Session ID
- `path` (path): Path to forward to agentapi

**Description:**
- All requests to `/{sessionId}/*` are proxied to the corresponding agentapi server instance
- Supports GET and POST methods
- Users can only access their own sessions

**Response Codes:**
- `200`: Response from agentapi server
- `403`: Forbidden - can only access own sessions
- `404`: Session not found
- `502`: Bad Gateway - agentapi server unavailable

**Examples:**
```bash
# Send a message to the agent (POST)
curl -X POST https://api.example.com/550e8400-e29b-41d4-a716-446655440000/message \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello", "type": "user"}'

# Get session status (GET)
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/550e8400-e29b-41d4-a716-446655440000/status

# Get conversation history (GET)
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/550e8400-e29b-41d4-a716-446655440000/messages
```

## Session Sharing Endpoints

### POST /sessions/:sessionId/share

Create a share token for read-only access to a session.

**Permissions Required:** `session:access`

**Response:**
```json
{
  "share_token": "sh_abc123def456"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/sessions/550e8400-e29b-41d4-a716-446655440000/share \
  -H "X-API-Key: YOUR_API_KEY"
```

### GET /sessions/:sessionId/share

Get the share token for a session.

**Permissions Required:** `session:access`

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/sessions/550e8400-e29b-41d4-a716-446655440000/share
```

### DELETE /sessions/:sessionId/share

Revoke the share token for a session.

**Permissions Required:** `session:access`

**Example:**
```bash
curl -X DELETE https://api.example.com/sessions/550e8400-e29b-41d4-a716-446655440000/share \
  -H "X-API-Key: YOUR_API_KEY"
```

### GET/HEAD /s/:shareToken/:path

Access a shared session in read-only mode (no authentication required).

**Security:** No authentication required

**Parameters:**
- `shareToken` (path): Share token (32-character hex string)
- `path` (path): Path to forward to agentapi

**Description:**
- All GET/HEAD requests to `/s/{shareToken}/*` are proxied to the corresponding agentapi server instance in read-only mode
- Only GET and HEAD methods are allowed for shared sessions
- POST/PUT/DELETE methods are not allowed and will return 403 Forbidden

**Response Codes:**
- `200`: Response from agentapi server
- `403`: Shared sessions are read-only (POST/PUT/DELETE not allowed)
- `404`: Invalid share token or session not found
- `410`: Share link has expired
- `502`: Bad Gateway - agentapi server unavailable

**Example:**
```bash
curl https://api.example.com/s/sh_abc123def456/messages
```

## Schedule Management Endpoints

### POST /schedules

Create a new schedule for delayed or recurring session execution.

**Permissions Required:** `session:create`

**Fields:**
- `name` (required): Schedule name
- `scheduled_at`: ISO 8601 timestamp for one-time execution (either this or `cron_expr` must be set)
- `cron_expr`: Cron expression for recurring execution (either this or `scheduled_at` must be set)
- `timezone`: Timezone for the schedule (default: `Asia/Tokyo`)
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`
- `session_config`: Configuration for created sessions

**Request Body:**
```json
{
  "name": "Daily Standup",
  "cron_expr": "0 9 * * 1-5",
  "session_config": {
    "tags": {
      "repository": "org/standup-bot"
    },
    "params": {
      "message": "Generate daily standup"
    }
  }
}
```

**Response:**
```json
{
  "id": "schedule-abc123",
  "name": "Daily Standup",
  "status": "active",
  "cron_expr": "0 9 * * 1-5",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Examples:**

One-time delayed execution:
```bash
curl -X POST https://api.example.com/schedules \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Code Review",
    "scheduled_at": "2025-01-15T14:00:00Z",
    "session_config": {
      "tags": {
        "repository": "org/repo"
      },
      "params": {
        "message": "Review PRs"
      }
    }
  }'
```

Recurring execution (cron):
```bash
curl -X POST https://api.example.com/schedules \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Daily Standup",
    "cron_expr": "0 9 * * 1-5",
    "session_config": {
      "tags": {
        "repository": "org/standup-bot"
      },
      "params": {
        "message": "Generate daily standup"
      }
    }
  }'
```

### GET /schedules

List schedules. Non-admin users can see their own schedules and team-scoped schedules they have access to.

**Permissions Required:** `session:list`

**Query Parameters:**
- `status`: Filter by schedule status (e.g., `active`, `paused`)
- `scope`: Filter by resource scope (e.g., `user`, `team`)
- `team_id`: Filter by team ID (e.g., `org/team-slug`)

**Response:**
```json
{
  "schedules": [
    {
      "id": "schedule-abc123",
      "name": "Daily Standup",
      "status": "active",
      "cron_expr": "0 9 * * 1-5",
      "created_at": "2024-01-01T12:00:00Z",
      "next_run": "2024-01-02T09:00:00Z"
    }
  ]
}
```

**Examples:**
```bash
# List all schedules
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/schedules

# Filter by status
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/schedules?status=active"

# Filter by team
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/schedules?team_id=org/my-team"
```

### GET /schedules/:id

Get a specific schedule by ID.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "id": "schedule-abc123",
  "name": "Daily Standup",
  "status": "active",
  "cron_expr": "0 9 * * 1-5",
  "session_config": {
    "tags": {
      "repository": "org/standup-bot"
    },
    "params": {
      "message": "Generate daily standup"
    }
  },
  "created_at": "2024-01-01T12:00:00Z",
  "next_run": "2024-01-02T09:00:00Z"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/schedules/schedule-abc123
```

**Access Control:**
- Users can only access their own schedules
- Admin users can access all schedules

### PUT /schedules/:id

Update an existing schedule.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Updated Schedule Name",
  "status": "paused",
  "cron_expr": "0 10 * * 1-5"
}
```

**Response:**
```json
{
  "id": "schedule-abc123",
  "name": "Updated Schedule Name",
  "status": "paused",
  "cron_expr": "0 10 * * 1-5",
  "updated_at": "2024-01-02T12:00:00Z"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/schedules/schedule-abc123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Updated Schedule Name",
    "status": "paused"
  }'
```

**Access Control:**
- Users can only update their own schedules

### DELETE /schedules/:id

Delete a schedule by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "message": "Schedule deleted successfully",
  "id": "schedule-abc123"
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/schedules/schedule-abc123 \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- Users can only delete their own schedules

### POST /schedules/:id/trigger

Manually trigger a schedule to immediately create a new session.

**Permissions Required:** `session:create`

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "triggered_at": "2024-01-02T15:30:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/schedules/schedule-abc123/trigger \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- Users can only trigger their own schedules

## Webhook Management Endpoints

Webhooks enable automatic session creation when events occur in external systems (GitHub, Slack, Datadog, custom services).

### POST /webhooks

Create a new webhook for GitHub or custom events.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Pull Request Reviewer",
  "type": "github",
  "scope": "user",
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
          "actions": ["opened", "synchronize"],
          "base_branches": ["main"],
          "draft": false
        }
      },
      "session_config": {
        "initial_message_template": "Review PR #{{.pull_request.number}}: {{.pull_request.title}}",
        "tags": {
          "repository": "{{.repository.full_name}}",
          "pr": "{{.pull_request.number}}"
        }
      }
    }
  ]
}
```

**Fields:**
- `name` (required): Webhook name
- `type` (required): `github` or `custom`
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`
- `secret`: Custom secret for HMAC signature verification (optional, auto-generated if not provided)
- `github`: GitHub-specific configuration (for type=github)
  - `allowed_events`: List of allowed GitHub events
  - `allowed_repositories`: Repository patterns (e.g., `myorg/*`)
- `triggers` (required): Array of trigger configurations
  - `name`: Trigger name
  - `enabled`: Whether trigger is active
  - `conditions`: Conditions for matching events
  - `session_config`: Configuration for created sessions
- `signature_header`: HTTP header containing the signature (default: `X-Signature`)
- `signature_type`: Signature verification type - `hmac` (default) or `static`
- `signature_prefix`: Prefix to strip from signature header before verification (auto-detected if empty)
- `max_sessions`: Maximum concurrent sessions for this webhook (default: 10, min: 1, max: 100)

**Response:**
```json
{
  "id": "webhook-123",
  "name": "Pull Request Reviewer",
  "type": "github",
  "status": "active",
  "webhook_url": "https://api.example.com/hooks/github/webhook-123",
  "secret": "generated-secret-key",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/webhooks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PR Review Bot",
    "type": "github",
    "github": {
      "allowed_events": ["pull_request"],
      "allowed_repositories": ["myorg/*"]
    },
    "triggers": [{
      "name": "Review PRs",
      "conditions": {
        "github": {
          "events": ["pull_request"],
          "actions": ["opened"]
        }
      },
      "session_config": {
        "initial_message_template": "Review PR #{{.pull_request.number}}"
      }
    }]
  }'
```

### GET /webhooks

List webhooks accessible to the authenticated user.

**Permissions Required:** `session:list`

**Query Parameters:**
- `type`: Filter by webhook type (`github`, `custom`)
- `status`: Filter by status (`active`, `paused`, `disabled`)
- `scope`: Filter by resource scope (`user`, `team`)
- `team_id`: Filter by team ID

**Response:**
```json
{
  "webhooks": [
    {
      "id": "webhook-123",
      "name": "PR Review Bot",
      "type": "github",
      "status": "active",
      "webhook_url": "https://api.example.com/hooks/github/webhook-123",
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-02T10:00:00Z"
    }
  ]
}
```

**Example:**
```bash
# List all webhooks
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/webhooks

# Filter by type
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/webhooks?type=github"
```

**Access Control:**
- Non-admin users can see their own webhooks and team-scoped webhooks they have access to
- Admin users can see all webhooks

### GET /webhooks/:id

Get a specific webhook by ID.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "id": "webhook-123",
  "name": "PR Review Bot",
  "type": "github",
  "status": "active",
  "scope": "user",
  "owner_id": "alice",
  "webhook_url": "https://api.example.com/hooks/github/webhook-123",
  "github": {
    "allowed_events": ["pull_request"],
    "allowed_repositories": ["myorg/*"]
  },
  "triggers": [...],
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-02T10:00:00Z"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/webhooks/webhook-123
```

**Access Control:**
- Users can only access their own webhooks

### PUT /webhooks/:id

Update an existing webhook.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Updated Webhook Name",
  "status": "paused",
  "triggers": [...]
}
```

**Response:**
```json
{
  "id": "webhook-123",
  "name": "Updated Webhook Name",
  "status": "paused",
  "updated_at": "2024-01-02T12:00:00Z"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/webhooks/webhook-123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "Updated Name", "status": "paused"}'
```

**Access Control:**
- Users can only update their own webhooks

### DELETE /webhooks/:id

Delete a webhook by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "message": "Webhook deleted successfully",
  "id": "webhook-123"
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/webhooks/webhook-123 \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- Users can only delete their own webhooks

### POST /webhooks/:id/regenerate-secret

Generate a new secret for webhook signature verification.

**Permissions Required:** `session:create`

**Response:**
```json
{
  "id": "webhook-123",
  "secret": "new-generated-secret-key"
}
```

**Note:** The new secret is shown only once. Update your webhook configuration in the external service immediately.

**Example:**
```bash
curl -X POST https://api.example.com/webhooks/webhook-123/regenerate-secret \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- Users can only regenerate secrets for their own webhooks

### POST /webhooks/:id/trigger

Manually trigger a webhook with a test payload.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "payload": {
    "event": "test_event",
    "data": "test_data"
  },
  "dry_run": true
}
```

**Fields:**
- `payload` (required): Test payload to evaluate
- `dry_run`: If true, only evaluates triggers without creating a session (default: false)

**Response:**
```json
{
  "matched": true,
  "trigger_id": "trigger-456",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Trigger matched and session created"
}
```

**Example:**
```bash
# Dry run test
curl -X POST https://api.example.com/webhooks/webhook-123/trigger \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"payload": {"test": "data"}, "dry_run": true}'

# Actual trigger
curl -X POST https://api.example.com/webhooks/webhook-123/trigger \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"payload": {"test": "data"}, "dry_run": false}'
```

**Access Control:**
- Users can only trigger their own webhooks

### POST /hooks/github/:id

Receive webhook payloads from GitHub (webhook endpoint).

**Security:** No authentication required (uses webhook signature verification)

**Note:** This is the webhook URL that should be configured in GitHub. The ID is the webhook ID returned when creating the webhook.

**Example GitHub webhook configuration:**
- Payload URL: `https://api.example.com/hooks/github/webhook-123`
- Content type: `application/json`
- Secret: Use the secret provided when creating the webhook

### POST /hooks/custom/:id

Receive webhook payloads from custom services (Slack, Datadog, PagerDuty, etc.).

**Security:** No authentication required (uses webhook signature verification)

**Note:** This is the webhook URL that should be configured in external services. The ID is the webhook ID returned when creating the webhook.

### POST /hooks/slack/:id

Receive Slack event payloads via the Slack Events API.

**Security:** No authentication required (uses Slack signature verification)

**Parameters:**
- `id` (path): SlackBot ID (UUID) or 'default' for server-configured default bot

**Request Body:**
```json
{
  "type": "event_callback",
  "event": {
    "type": "message",
    "channel": "C01234567",
    "user": "U01234567",
    "text": "Hello, world!",
    "ts": "1628000000.000000",
    "thread_ts": "1628000000.000000"
  }
}
```

**Response:**
```json
{
  "challenge": "challenge-token-for-url-verification"
}
```

**Description:**
- Receives Slack Events API payloads
- Verifies Slack v0 HMAC-SHA256 signature
- Handles URL verification challenges
- Creates or reuses a session per Slack thread
- Triggers the claude-posts sidecar for Slack thread replies

**Note:** This is the webhook URL that should be configured in Slack Events API. The ID is either a registered SlackBot UUID or 'default' to use the server startup configuration.

## Task Management Endpoints

Tasks are units of work associated with a session. They can be used to track work items for both agents and users.

### Task Object

```json
{
  "id": "uuid",
  "title": "string",
  "description": "string",
  "status": "todo | done",
  "task_type": "agent | user",
  "scope": "user | team",
  "owner_id": "string",
  "session_id": "string",
  "group_id": "string",
  "team_id": "string",
  "links": [
    {
      "id": "uuid",
      "url": "https://example.com",
      "title": "optional title"
    }
  ],
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

### POST /tasks

Create a new task.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "title": "Review PR #123",
  "description": "Please review and approve the pull request",
  "task_type": "user",
  "scope": "user",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "group_id": "optional-group-id",
  "team_id": "optional-team-id",
  "links": [
    {
      "url": "https://github.com/owner/repo/pull/123",
      "title": "PR #123"
    }
  ]
}
```

**Fields:**
- `title` (required): Task title
- `task_type` (required): `agent` (default) or `user`
- `scope` (required): `user` (default) or `team`
- `session_id`: ID of the session to associate with (optional)
- `description`: Optional description
- `group_id`: Optional group ID for grouping tasks
- `team_id`: Required when `scope` is `team`
- `links`: Optional array of associated URLs

**Response:**
```json
{
  "id": "1b81fae1-a266-4538-a66c-2b0b0e274a81",
  "title": "Review PR #123",
  "description": "Please review and approve the pull request",
  "status": "todo",
  "task_type": "user",
  "scope": "user",
  "owner_id": "alice",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "links": [
    {
      "id": "6c7a10ec-feb5-4d2f-9b84-9df8f50f0b1d",
      "url": "https://github.com/owner/repo/pull/123",
      "title": "PR #123"
    }
  ],
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/tasks \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Review PR #123",
    "task_type": "user",
    "scope": "user",
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "links": [{"url": "https://github.com/owner/repo/pull/123", "title": "PR #123"}]
  }'
```

**CLI equivalent:**
```bash
agentapi-proxy client task create \
  --endpoint https://api.example.com \
  --session-id SESSION_ID \
  --title "Review PR #123" \
  --task-type user \
  --scope user \
  --link "https://github.com/owner/repo/pull/123|PR #123"
```

### GET /tasks

List tasks with optional filters.

**Permissions Required:** `session:list`

**Query Parameters:**
- `status`: Filter by status (`todo`, `done`)
- `task_type`: Filter by type (`agent`, `user`)
- `scope`: Filter by scope (`user`, `team`)
- `team_id`: Filter by team ID
- `group_id`: Filter by group ID

**Response:**
```json
{
  "tasks": [
    {
      "id": "1b81fae1-a266-4538-a66c-2b0b0e274a81",
      "title": "Review PR #123",
      "status": "todo",
      "task_type": "user",
      "scope": "user",
      "owner_id": "alice",
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "links": [],
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ],
  "total": 1
}
```

**Examples:**
```bash
# List all tasks
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/tasks

# Filter pending tasks
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/tasks?status=todo"

# Filter by type
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/tasks?task_type=user"
```

**CLI equivalent:**
```bash
agentapi-proxy client task list \
  --endpoint https://api.example.com \
  --session-id SESSION_ID \
  --status todo \
  --task-type user
```

**Access Control:**
- Non-admin users can only see their own tasks
- Admin users can see all tasks

### GET /tasks/:taskId

Get a specific task by ID.

**Permissions Required:** `session:read`

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/tasks/1b81fae1-a266-4538-a66c-2b0b0e274a81
```

**CLI equivalent:**
```bash
agentapi-proxy client task get TASK_ID \
  --endpoint https://api.example.com \
  --session-id SESSION_ID
```

### PUT /tasks/:taskId

Update an existing task. Use the CLI for this operation.

**Permissions Required:** `session:create`

**Updatable Fields:**
- `title`: New title
- `description`: New description
- `status`: New status (`todo` or `done`)
- `group_id`: New group ID (set to empty string to remove from group)
- `session_id`: New session ID to associate with
- `links`: Replaces all existing links when present

**CLI (recommended):**
```bash
# Mark a task as done
agentapi-proxy client task update TASK_ID \
  --endpoint https://api.example.com \
  --session-id SESSION_ID \
  --status done

# Update title and description
agentapi-proxy client task update TASK_ID \
  --endpoint https://api.example.com \
  --session-id SESSION_ID \
  --title "New title" \
  --description "New description"
```

### DELETE /tasks/:taskId

Delete a task by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/tasks/1b81fae1-a266-4538-a66c-2b0b0e274a81 \
  -H "X-API-Key: YOUR_API_KEY"
```

**CLI equivalent:**
```bash
agentapi-proxy client task delete TASK_ID \
  --endpoint https://api.example.com \
  --session-id SESSION_ID
```

## Task Group Management Endpoints

Task groups provide a way to organize and manage related tasks together.

### POST /task-groups

Create a new task group.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Sprint 23 Tasks",
  "description": "Tasks for the current sprint",
  "scope": "user",
  "team_id": "optional-team-id"
}
```

**Fields:**
- `name` (required): Group name
- `description`: Optional description
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`

**Response:**
```json
{
  "id": "group-abc123",
  "name": "Sprint 23 Tasks",
  "description": "Tasks for the current sprint",
  "scope": "user",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/task-groups \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Sprint 23 Tasks",
    "description": "Tasks for the current sprint",
    "scope": "user"
  }'
```

### GET /task-groups

List task groups accessible to the authenticated user.

**Permissions Required:** `session:list`

**Query Parameters:**
- `scope`: Filter by scope (`user`, `team`)
- `team_id`: Filter by team ID (required when scope=team)

**Response:**
```json
{
  "task_groups": [
    {
      "id": "group-abc123",
      "name": "Sprint 23 Tasks",
      "description": "Tasks for the current sprint",
      "scope": "user",
      "owner_id": "alice",
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ],
  "total": 1
}
```

**Examples:**
```bash
# List all task groups
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/task-groups

# Filter by scope
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/task-groups?scope=user"
```

**Access Control:**
- Non-admin users can only see their own task groups
- Admin users can see all task groups

### GET /task-groups/:groupId

Get a specific task group by ID.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "id": "group-abc123",
  "name": "Sprint 23 Tasks",
  "description": "Tasks for the current sprint",
  "scope": "user",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/task-groups/group-abc123
```

### PUT /task-groups/:groupId

Update an existing task group.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Updated Group Name",
  "description": "Updated description"
}
```

**Note:** Omitted fields are not changed.

**Response:**
```json
{
  "id": "group-abc123",
  "name": "Updated Group Name",
  "description": "Updated description",
  "updated_at": "2024-01-02T12:00:00Z"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/task-groups/group-abc123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "Updated Group Name"}'
```

### DELETE /task-groups/:groupId

Delete a task group by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/task-groups/group-abc123 \
  -H "X-API-Key: YOUR_API_KEY"
```

## Memory Management Endpoints

Memory entries allow storing and retrieving contextual information for agents and users. Supports user-scoped (private) and team-scoped (shared) memories.

### POST /memories

Create a new memory entry.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "title": "Project conventions",
  "content": "Always use TypeScript for new files. Follow ESLint rules.",
  "scope": "user",
  "team_id": "optional-team-id"
}
```

**Fields:**
- `title` (required): Memory title
- `content` (required): Memory content
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`

**Response:**
```json
{
  "id": "mem-abc123",
  "title": "Project conventions",
  "content": "Always use TypeScript for new files. Follow ESLint rules.",
  "scope": "user",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/memories \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Project conventions",
    "content": "Always use TypeScript for new files.",
    "scope": "user"
  }'
```

### GET /memories

List memory entries accessible to the authenticated user.

**Permissions Required:** `session:list`

**Query Parameters:**
- `scope`: Filter by scope (`user`, `team`)
- `team_id`: Filter by team ID (required when scope=team)
- `q`: Full-text search query (searches title and content, case-insensitive)

**Response:**
```json
{
  "memories": [
    {
      "id": "mem-abc123",
      "title": "Project conventions",
      "content": "Always use TypeScript for new files.",
      "scope": "user",
      "owner_id": "alice",
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ],
  "total": 1
}
```

**Examples:**
```bash
# List all memories
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/memories

# Search memories
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/memories?q=typescript"

# Filter by team
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/memories?scope=team&team_id=myorg/team-slug"
```

**Access Control:**
- User-scoped: Only the owner can see their memories
- Team-scoped: All team members can see the memories
- Admin privilege does NOT bypass these restrictions

### GET /memories/:memoryId

Get a specific memory entry by ID.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "id": "mem-abc123",
  "title": "Project conventions",
  "content": "Always use TypeScript for new files.",
  "scope": "user",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/memories/mem-abc123
```

**Access Control:**
- User-scoped: Only the owner can access
- Team-scoped: Only team members can access
- Admin privilege does NOT bypass these restrictions

### PUT /memories/:memoryId

Update an existing memory entry.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "title": "Updated conventions",
  "content": "Updated content"
}
```

**Note:** Omitted fields are not changed.

**Response:**
```json
{
  "id": "mem-abc123",
  "title": "Updated conventions",
  "content": "Updated content",
  "updated_at": "2024-01-02T12:00:00Z"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/memories/mem-abc123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title": "Updated conventions"}'
```

**Access Control:**
- User-scoped: Only the owner can update
- Team-scoped: Only team members can update

### DELETE /memories/:memoryId

Delete a memory entry by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/memories/mem-abc123 \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- User-scoped: Only the owner can delete
- Team-scoped: Only team members can delete

## SlackBot Management Endpoints

SlackBot configurations enable automated session creation in response to Slack events. Each SlackBot corresponds to a Slack App installation and receives events via Socket Mode (WebSocket).

### POST /slackbots

Create a new SlackBot configuration.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "My Slack Bot",
  "scope": "user",
  "team_id": "optional-team-id",
  "bot_token_secret_name": "my-slack-bot-token",
  "bot_token_secret_key": "bot-token",
  "allowed_channel_names": ["dev", "backend"],
  "session_config": {
    "initial_message_template": "New Slack message from {{.event.user}} in <#{{.event.channel}}>: {{.event.text}}",
    "tags": {
      "channel": "{{.event.channel}}"
    }
  }
}
```

**Fields:**
- `name` (required): SlackBot name
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`
- `bot_token_secret_name`: Kubernetes secret name containing the bot token
- `bot_token_secret_key`: Key within the secret containing the bot token
- `allowed_channel_names`: Optional list of allowed channel names (without #)
- `session_config`: Configuration for created sessions
  - `initial_message_template`: Go template for initial message
  - `tags`: Tags to apply to created sessions

**Response:**
```json
{
  "id": "slackbot-abc123",
  "name": "My Slack Bot",
  "status": "active",
  "scope": "user",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST https://api.example.com/slackbots \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Team Bot",
    "scope": "team",
    "team_id": "myorg/backend",
    "bot_token_secret_name": "my-slack-bot-token",
    "bot_token_secret_key": "bot-token",
    "allowed_channel_names": ["dev", "backend"],
    "session_config": {
      "initial_message_template": "{{.event.text}}",
      "tags": {
        "channel": "{{.event.channel}}"
      }
    }
  }'
```

### GET /slackbots

List SlackBots accessible to the authenticated user.

**Permissions Required:** `session:list`

**Query Parameters:**
- `status`: Filter by SlackBot status (e.g., `active`, `inactive`)
- `scope`: Filter by resource scope (`user`, `team`)
- `team_id`: Filter by team ID

**Response:**
```json
{
  "slackbots": [
    {
      "id": "slackbot-abc123",
      "name": "My Slack Bot",
      "status": "active",
      "scope": "user",
      "owner_id": "alice",
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ]
}
```

**Examples:**
```bash
# List all SlackBots
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/slackbots

# Filter by scope
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/slackbots?scope=team"

# Filter by team
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/slackbots?team_id=myorg/backend"
```

**Access Control:**
- Non-admin users can see their own SlackBots and team-scoped SlackBots they have access to
- Admin users can see all SlackBots

### GET /slackbots/:id

Get a specific SlackBot by ID.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "id": "slackbot-abc123",
  "name": "My Slack Bot",
  "status": "active",
  "scope": "user",
  "owner_id": "alice",
  "bot_token_secret_name": "my-slack-bot-token",
  "bot_token_secret_key": "bot-token",
  "allowed_channel_names": ["dev", "backend"],
  "session_config": {
    "initial_message_template": "{{.event.text}}",
    "tags": {
      "channel": "{{.event.channel}}"
    }
  },
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/slackbots/slackbot-abc123
```

**Access Control:**
- Users can only access their own SlackBots and team-scoped SlackBots they have access to

### PUT /slackbots/:id

Update an existing SlackBot.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "name": "Updated Bot Name",
  "status": "inactive",
  "allowed_channel_names": ["dev", "backend", "general"]
}
```

**Note:** Omitted fields are not changed.

**Response:**
```json
{
  "id": "slackbot-abc123",
  "name": "Updated Bot Name",
  "status": "inactive",
  "updated_at": "2024-01-02T12:00:00Z"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/slackbots/slackbot-abc123 \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "Updated Bot Name"}'
```

**Access Control:**
- Users can only update their own SlackBots
- Team members can update team-scoped SlackBots

### DELETE /slackbots/:id

Delete a SlackBot by ID.

**Permissions Required:** `session:delete`

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X DELETE https://api.example.com/slackbots/slackbot-abc123 \
  -H "X-API-Key: YOUR_API_KEY"
```

**Access Control:**
- Users can only delete their own SlackBots
- Team members can delete team-scoped SlackBots

## User & Settings Endpoints

### GET /user/info

Get authenticated user information.

**Permissions Required:** `session:read`

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/user/info
```

### GET /settings/:name

Get a specific setting value.

**Permissions Required:** `session:read`

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/settings/theme
```

### PUT /settings/:name

Update a setting value.

**Permissions Required:** `session:create`

**Request Body:**
```json
{
  "value": "dark"
}
```

**Example:**
```bash
curl -X PUT https://api.example.com/settings/theme \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"value": "dark"}'
```

### DELETE /settings/:name

Delete a setting.

**Permissions Required:** `session:create`

**Example:**
```bash
curl -X DELETE https://api.example.com/settings/theme \
  -H "X-API-Key: YOUR_API_KEY"
```

### GET /users/me/api-key

Get or create a personal API key.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "api_key": "ap_personal_xyz123"
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/users/me/api-key
```

### POST /users/me/api-key

Generate a new personal API key (rotates the existing key).

**Permissions Required:** `session:read`

**Example:**
```bash
curl -X POST https://api.example.com/users/me/api-key \
  -H "X-API-Key: YOUR_API_KEY"
```

## Notification Endpoints

### POST /notification/subscribe

Subscribe to push notifications.

**Permissions Required:** `session:read`

**Request Body:**
```json
{
  "endpoint": "https://...",
  "keys": {
    "p256dh": "...",
    "auth": "..."
  }
}
```

**Example:**
```bash
curl -X POST https://api.example.com/notification/subscribe \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{...}'
```

### GET /notification/subscribe

Get current subscription information.

**Permissions Required:** `session:read`

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/notification/subscribe
```

### DELETE /notification/subscribe

Unsubscribe from notifications.

**Permissions Required:** `session:read`

**Example:**
```bash
curl -X DELETE https://api.example.com/notification/subscribe \
  -H "X-API-Key: YOUR_API_KEY"
```

### GET /notifications/history

Get notification history for the current user.

**Permissions Required:** `session:read`

**Response:**
```json
{
  "notifications": [
    {
      "id": "notif-123",
      "title": "Session completed",
      "body": "Your session has finished successfully",
      "url": "https://example.com/sessions/abc123",
      "created_at": "2024-01-02T15:30:00Z",
      "read": false
    }
  ]
}
```

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/notifications/history
```

## Authentication Endpoints

These endpoints are typically used for OAuth flows and do not require API key authentication.

### GET /health

Health check endpoint (no authentication required).

**Example:**
```bash
curl https://api.example.com/health
```

### GET /auth/status

Check authentication status.

**Example:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/auth/status
```

### POST /oauth/authorize

Initiate OAuth authorization flow (no authentication required).

### GET /oauth/callback

OAuth callback endpoint (no authentication required).

### POST /oauth/logout

Logout from OAuth session.

### POST /oauth/refresh

Refresh OAuth access token.

## Error Responses

### 401 Unauthorized

Invalid or missing API key:
```json
{
  "error": "Invalid API key"
}
```

Expired API key:
```json
{
  "error": "API key expired"
}
```

### 403 Forbidden

Insufficient permissions:
```json
{
  "error": "Insufficient permissions"
}
```

Session access denied (non-owner):
```json
{
  "error": "Access denied"
}
```

### 404 Not Found

Session not found:
```json
{
  "error": "Session not found"
}
```
