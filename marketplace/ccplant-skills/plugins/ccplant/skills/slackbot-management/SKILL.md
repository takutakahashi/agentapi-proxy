---
name: slackbot-management
description: |
  Manage agentapi-proxy SlackBots for automated session creation from Slack events.
  Use when you need to: (1) Create SlackBot configurations, (2) Update SlackBot settings,
  (3) List existing SlackBots, (4) Delete SlackBots, (5) Configure channel filters and
  message templates. SlackBots use Socket Mode (WebSocket) to receive Slack events and
  automatically create sessions based on configured templates.
---

# SlackBot Management

This skill provides guidance for managing agentapi-proxy SlackBots that automatically create sessions in response to Slack events.

## Overview

SlackBots enable automatic session creation when events occur in Slack. Each SlackBot has:
- **Name**: Descriptive name for the SlackBot
- **Scope**: User-level or team-level access
- **Bot Token**: Kubernetes secret containing the Slack bot token
- **Channel Filters**: Optional list of allowed channel names
- **Session Config**: Environment variables, tags, and initial messages for created sessions

SlackBots use Socket Mode (WebSocket) to receive events from Slack, eliminating the need for public webhook endpoints.

## Core Workflows

### Creating a SlackBot

#### Basic SlackBot

```bash
cat > slackbot-basic.json <<'EOF'
{
  "name": "My Slack Bot",
  "session_config": {
    "initial_message_template": "New Slack message from {{.event.user}} in <#{{.event.channel}}>: {{.event.text}}"
  }
}
EOF

agentapi-proxy client slackbot create -f slackbot-basic.json
```

#### Team-Scoped SlackBot with Channel and Event Filters

```bash
cat > slackbot-team.json <<'EOF'
{
  "name": "Team Bot",
  "scope": "team",
  "team_id": "myorg/backend",
  "bot_token_secret_name": "my-slack-bot-token",
  "bot_token_secret_key": "bot-token",
  "allowed_channel_names": ["dev", "backend"],
  "allowed_event_types": ["message", "app_mention"],
  "notify_on_session_created": true,
  "allow_bot_messages": false,
  "max_sessions": 10,
  "session_config": {
    "initial_message_template": "{{.event.text}}",
    "tags": {
      "channel": "{{.event.channel}}"
    }
  }
}
EOF

agentapi-proxy client slackbot create -f slackbot-team.json
```

**Fields:**
- `name` (required): SlackBot name
- `scope`: `user` (default) or `team`
- `team_id`: Required when `scope` is `team`
- `bot_token_secret_name`: Kubernetes secret name containing the bot token
- `bot_token_secret_key`: Key within the secret containing the bot token (default: "bot-token")
- `bot_token`: Direct bot token (xoxb-...) - alternative to secret reference
- `app_token`: Direct app-level token for Socket Mode (xapp-...) - alternative to secret reference
- `allowed_channel_names`: Optional list of allowed channel names (without #). Supports partial matching. If omitted, all channels are allowed.
- `allowed_event_types`: Optional list of Slack event types to process (e.g., ["message", "app_mention"]). If omitted, all event types are allowed.
- `notify_on_session_created`: Post a notification message with the session URL when a session is created (default: true)
- `allow_bot_messages`: Process messages from other bots (default: false)
- `max_sessions`: Maximum concurrent sessions (default: 10)
- `session_config`: Configuration for created sessions
  - `initial_message_template`: Go template for new thread sessions
  - `reuse_message_template`: Go template for messages sent to existing thread sessions
  - `tags`: Tags to apply to created sessions (supports Go template values)
  - `environment`: Environment variables for the session (supports Go template values)
  - `params`: SlackBotSessionParams
    - `agent_type`: Agent type (e.g., "claude-agentapi")
    - `oneshot`: Auto-delete session after response (default: false)

**Response:**
```json
{
  "id": "slackbot-abc123",
  "name": "Team Bot",
  "status": "active",
  "scope": "team",
  "team_id": "myorg/backend",
  "owner_id": "alice",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:00Z"
}
```

### Listing SlackBots

```bash
# List all SlackBots
agentapi-proxy client slackbot list

# Note: Filtering by status, scope, or team is done by the API automatically
# based on your authentication and permissions
```

### Getting a Specific SlackBot

```bash
agentapi-proxy client slackbot get SLACKBOT_ID
```

### Updating a SlackBot

```bash
# Update specific fields using apply (patch)
echo '{"status":"inactive"}' | agentapi-proxy client slackbot apply SLACKBOT_ID

# Or update multiple fields
cat > update.json <<'EOF'
{
  "name": "Updated Bot Name",
  "status": "inactive",
  "allowed_channel_names": ["dev", "backend", "general"]
}
EOF

cat update.json | agentapi-proxy client slackbot apply SLACKBOT_ID
```

**Note:** Omitted fields are not changed.

### Deleting a SlackBot

```bash
agentapi-proxy client slackbot delete SLACKBOT_ID
```

## Use Cases

### 1. Support Bot

Automatically create sessions when users post in a support channel:

```json
{
  "name": "Support Bot",
  "allowed_channel_names": ["support", "help"],
  "session_config": {
    "initial_message_template": "Support request from {{.event.user}}: {{.event.text}}",
    "tags": {
      "type": "support",
      "channel": "{{.event.channel}}"
    }
  }
}
```

### 2. Code Review Bot

Create sessions for code review requests:

```json
{
  "name": "Code Review Bot",
  "allowed_channel_names": ["code-review"],
  "session_config": {
    "initial_message_template": "Review request: {{.event.text}}",
    "tags": {
      "type": "code-review",
      "user": "{{.event.user}}"
    }
  }
}
```

### 3. Team Incident Bot

Team-scoped bot for incident response:

```json
{
  "name": "Incident Bot",
  "scope": "team",
  "team_id": "myorg/sre",
  "allowed_channel_names": ["incidents"],
  "session_config": {
    "initial_message_template": "Incident alert: {{.event.text}}",
    "tags": {
      "type": "incident",
      "severity": "high"
    },
    "environment": {
      "PAGERDUTY_TOKEN": "pd_token"
    }
  }
}
```

## Template Variables

The `initial_message_template` and `reuse_message_template` support Go template syntax with access to Slack event data:

**Common variables:**
- `{{.event.type}}`: Event type (e.g., "message", "app_mention")
- `{{.event.user}}`: User ID who triggered the event
- `{{.event.channel}}`: Channel ID where the event occurred
- `{{.event.text}}`: Message text
- `{{.event.ts}}`: Event timestamp
- `{{.event.thread_ts}}`: Thread timestamp (for threaded messages)
- `{{.team_id}}`: Slack workspace team ID
- `{{.api_app_id}}`: Slack app ID

**Slack formatting:**
- `<@{{.event.user}}>`: Mention user
- `<#{{.event.channel}}>`: Link to channel

**Example:**
```
New message from <@{{.event.user}}> in <#{{.event.channel}}>: {{.event.text}}
```

## Bot Token Setup

SlackBots require a Slack bot token and app-level token. There are two ways to provide them:

### Method 1: Kubernetes Secret (Recommended)

1. Create a Slack App at https://api.slack.com/apps
2. Enable Socket Mode and generate an App-Level Token with `connections:write` scope
3. Install the app to your workspace and note the Bot User OAuth Token
4. Create a Kubernetes secret:

```bash
kubectl create secret generic my-slack-bot-token \
  --from-literal=bot-token=xoxb-your-bot-token \
  --from-literal=app-token=xapp-your-app-token
```

5. Reference the secret in your SlackBot configuration:

```bash
cat > slackbot-with-secret.json <<'EOF'
{
  "name": "My Slack Bot",
  "bot_token_secret_name": "my-slack-bot-token",
  "bot_token_secret_key": "bot-token",
  "session_config": {
    "initial_message_template": "{{.event.text}}"
  }
}
EOF

agentapi-proxy client slackbot create -f slackbot-with-secret.json
```

### Method 2: Direct Token Provision

Provide tokens directly in the request (they will be stored in a Kubernetes secret automatically):

```bash
cat > slackbot-with-tokens.json <<'EOF'
{
  "name": "My Slack Bot",
  "bot_token": "xoxb-your-bot-token",
  "app_token": "xapp-your-app-token",
  "session_config": {
    "initial_message_template": "{{.event.text}}"
  }
}
EOF

agentapi-proxy client slackbot create -f slackbot-with-tokens.json
```

**Note:** Tokens are write-only and will not be returned in GET requests for security.

## Access Control

- **User Scope**: Only the creating user can access and manage the SlackBot
- **Team Scope**: All team members can access and manage the SlackBot
- **Admin**: Can view and manage all SlackBots

## Reference Documentation

For complete API endpoint documentation and permissions, see:
- [API_REFERENCE.md](../references/API_REFERENCE.md#slackbot-management-endpoints) - Complete SlackBot API reference
- [PERMISSIONS.md](../references/PERMISSIONS.md) - Role-based access control details
