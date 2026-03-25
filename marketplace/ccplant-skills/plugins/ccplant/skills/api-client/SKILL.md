---
name: agentapi-proxy-api
description: |
  Interact with agentapi-proxy API using API Key authentication for session management.
  Use when you need to: (1) Create new agentapi sessions, (2) Search and list existing sessions,
  (3) Delete sessions, (4) Route requests to specific session instances, (5) Manage session sharing,
  (6) Access user settings and notifications, (7) Create and manage tasks associated with sessions,
  (8) Manage task groups for organizing tasks, (9) Create and manage memory entries for storing
  contextual information. Supports multiple authentication methods including static API keys
  (X-API-Key header) and Authorization Bearer tokens.
  Note: For schedule management, use the schedule-management skill instead. For webhook management,
  use the webhook-management skill instead. For SlackBot management, use the slackbot-management
  skill instead.
---

# agentapi-proxy API

This skill provides guidance for interacting with the agentapi-proxy API using API Key authentication.

## Authentication

agentapi-proxy supports multiple authentication methods. The most common are:

### Method 1: X-API-Key Header (Recommended)

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://api.example.com/endpoint
```

### Method 2: Authorization Bearer Token

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://api.example.com/endpoint
```

## Core Workflows

### Creating a Session

```bash
curl -X POST https://api.example.com/start \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "environment": {
      "GITHUB_TOKEN": "ghp_...",
      "CUSTOM_VAR": "value"
    },
    "tags": {
      "repository": "my-repo",
      "branch": "main"
    },
    "params": {
      "message": "Start working on task",
      "initial_message_wait_second": 2
    },
    "memory_key": {
      "project": "my-project",
      "team": "backend"
    }
  }'
```

**Additional Fields:**
- `params.message`: Initial message to send to the session
- `params.initial_message_wait_second`: Wait time (in seconds) before sending the initial message (default: 2)
- `memory_key`: Custom tag map for memory lookup. Memories matching these tags will be included in the session context.

Response:
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Searching Sessions

```bash
# List all sessions
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/search

# Filter by status
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/search?status=active"

# Filter by tags
curl -H "X-API-Key: YOUR_API_KEY" \
  "https://api.example.com/search?tag.repository=my-repo&tag.branch=main"
```

### Deleting a Session

```bash
curl -X DELETE https://api.example.com/sessions/SESSION_ID \
  -H "X-API-Key: YOUR_API_KEY"
```

### Routing to Session

All requests to `/:sessionId/*` are proxied to the agentapi instance for that session:

```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/SESSION_ID/message \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello", "type": "user"}'
```

### Viewing Notification History

```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/notifications/history
```

### Managing Tasks

Tasks are units of work associated with a session. There are two types: `agent` tasks (created by the AI agent) and `user` tasks (created for human action). Use the `agentapi-proxy client task` CLI for full task management.

**Create a task:**
```bash
# Agent task (default) - work performed by the agent
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID \
  --title "Implement feature X" \
  --task-type agent \
  --scope user

# User task - action required from a human
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID \
  --title "Review PR #123" \
  --task-type user \
  --scope user \
  --description "Please review and approve the pull request" \
  --link "https://github.com/owner/repo/pull/123|PR #123"
```

**List tasks:**
```bash
# List all tasks
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID

# Filter by status (todo / done)
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID \
  --status todo

# Filter by type
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID \
  --task-type user
```

**Update a task:**
```bash
agentapi-proxy client task update TASK_ID \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID \
  --status done
```

**Get / Delete a task:**
```bash
# Get
agentapi-proxy client task get TASK_ID \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID

# Delete
agentapi-proxy client task delete TASK_ID \
  --endpoint http://proxy:8080 \
  --session-id SESSION_ID
```

See [TASK_REFERENCE.md](references/TASK_REFERENCE.md) for complete task API documentation.

### Managing Task Groups

Task groups provide a way to organize and manage related tasks together.

**Preferred Method: Use MCP Tools**

When using Claude Code or environments with MCP support, use the built-in MCP tools:

```
# Create a task group
Use the mcp__ccplant-dev__create_task_group tool

# List task groups
Use the mcp__ccplant-dev__list_task_groups tool

# Delete a task group
Use the mcp__ccplant-dev__delete_task_group tool
```

**Alternative: Direct API Calls**

If MCP tools are not available, use curl:

**Create a task group:**
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

**List task groups:**
```bash
curl -H "X-API-Key: YOUR_API_KEY" \
  https://api.example.com/task-groups
```

**Update a task group:**
```bash
curl -X PUT https://api.example.com/task-groups/GROUP_ID \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Updated Group Name"
  }'
```

**Delete a task group:**
```bash
curl -X DELETE https://api.example.com/task-groups/GROUP_ID \
  -H "X-API-Key: YOUR_API_KEY"
```

### Managing Memory Entries

Memory entries allow storing and retrieving contextual information for agents and users. Supports user-scoped (private) and team-scoped (shared) memories.

**Preferred Method: Use MCP Tools**

When using Claude Code or environments with MCP support, use the built-in MCP tools:

```
# Create a memory entry
Use the mcp__ccplant-dev__create_memory tool

# List memory entries
Use the mcp__ccplant-dev__list_memories tool

# Get a specific memory
Use the mcp__ccplant-dev__get_memory tool

# Update a memory entry
Use the mcp__ccplant-dev__update_memory tool

# Delete a memory entry
Use the mcp__ccplant-dev__delete_memory tool
```

**Alternative: Direct API Calls**

If MCP tools are not available, use curl:

**Create a memory entry:**
```bash
curl -X POST https://api.example.com/memories \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Project conventions",
    "content": "Always use TypeScript for new files. Follow ESLint rules.",
    "scope": "user"
  }'
```

**List memory entries:**
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

**Update a memory entry:**
```bash
curl -X PUT https://api.example.com/memories/MEMORY_ID \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Updated conventions",
    "content": "Updated content"
  }'
```

**Delete a memory entry:**
```bash
curl -X DELETE https://api.example.com/memories/MEMORY_ID \
  -H "X-API-Key: YOUR_API_KEY"
```

## API Reference

For complete API endpoint documentation, permissions, and authentication details, see:
- [API_REFERENCE.md](references/API_REFERENCE.md) - Complete endpoint reference
- [AUTHENTICATION.md](references/AUTHENTICATION.md) - Authentication methods and configuration
- [PERMISSIONS.md](references/PERMISSIONS.md) - Role-based access control details
- [TASK_REFERENCE.md](references/TASK_REFERENCE.md) - Task management API reference

## Helper Scripts

Use `scripts/agentapi_request.sh` for quick API requests with automatic environment variable handling.
