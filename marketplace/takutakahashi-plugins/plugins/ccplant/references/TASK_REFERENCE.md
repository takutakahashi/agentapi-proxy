# Task Management Reference

Tasks are units of work that can be associated with a session. They are useful for tracking agent progress, requesting user actions, and organizing work items.

## Task Types

| Type    | Description                                         |
|---------|-----------------------------------------------------|
| `agent` | Work performed by the AI agent (default)            |
| `user`  | Action required from a human (e.g., review a PR)   |

## Task Status

| Status | Description              |
|--------|--------------------------|
| `todo` | Task is pending (default)|
| `done` | Task is completed        |

## Task Scope

| Scope  | Description                               |
|--------|-------------------------------------------|
| `user` | Visible only to the task owner (default)  |
| `team` | Visible to all members of the specified team |

## Task Object

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
  "group_id": "sprint-2024-01",
  "team_id": "org/my-team",
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

## CLI Reference

The `agentapi-proxy client task` CLI is the recommended way to manage tasks. It handles authentication automatically using the `AGENTAPI_KEY` environment variable.

### Common Flags

| Flag           | Description                              | Required |
|----------------|------------------------------------------|----------|
| `--endpoint`   | agentapi-proxy base URL                  | Yes      |
| `--session-id` | Session ID to associate with the command | Yes      |

### task create

Create a new task.

```
agentapi-proxy client task create [flags]
```

**Flags:**

| Flag            | Description                                          | Default  |
|-----------------|------------------------------------------------------|----------|
| `--title`       | Task title                                           | Required |
| `--task-type`   | Task type: `user` or `agent`                         | `agent`  |
| `--scope`       | Task scope: `user` or `team`                         | `user`   |
| `--description` | Task description                                     |          |
| `--group-id`    | Group ID for grouping related tasks                  |          |
| `--team-id`     | Team ID (required when `--scope team`)               |          |
| `--link`        | Associated URL. Format: `url` or `url\|title`. Repeatable |     |

**Examples:**

```bash
# Create an agent task
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Implement feature X" \
  --task-type agent

# Create a user task with a PR link
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Review PR #123" \
  --task-type user \
  --description "Please approve the pull request" \
  --link "https://github.com/owner/repo/pull/123|PR #123"

# Create a team-scoped task
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Team code review" \
  --task-type user \
  --scope team \
  --team-id "myorg/backend-team"

# Create a task with multiple links
agentapi-proxy client task create \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Incident response" \
  --link "https://github.com/owner/repo/issues/456|Issue #456" \
  --link "https://runbook.example.com/incident"
```

### task list

List tasks with optional filters.

```
agentapi-proxy client task list [flags]
```

**Flags:**

| Flag          | Description                        |
|---------------|------------------------------------|
| `--status`    | Filter by status: `todo` or `done` |
| `--task-type` | Filter by type: `user` or `agent`  |
| `--scope`     | Filter by scope: `user` or `team`  |
| `--team-id`   | Filter by team ID                  |
| `--group-id`  | Filter by group ID                 |

**Examples:**

```bash
# List all tasks
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID

# List pending tasks
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --status todo

# List user tasks (actions required by humans)
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --task-type user

# List team tasks
agentapi-proxy client task list \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --scope team \
  --team-id "myorg/backend-team"
```

### task get

Get a specific task by ID.

```
agentapi-proxy client task get <taskId> [flags]
```

**Example:**

```bash
agentapi-proxy client task get 1b81fae1-a266-4538-a66c-2b0b0e274a81 \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID
```

### task update

Update an existing task.

```
agentapi-proxy client task update <taskId> [flags]
```

**Flags:**

| Flag               | Description                          |
|--------------------|--------------------------------------|
| `--title`          | New title                            |
| `--description`    | New description                      |
| `--status`         | New status: `todo` or `done`         |
| `--group-id`       | New group ID                         |
| `--session-id-new` | New session ID to associate with     |

**Examples:**

```bash
# Mark a task as done
agentapi-proxy client task update 1b81fae1-a266-4538-a66c-2b0b0e274a81 \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --status done

# Update title and description
agentapi-proxy client task update 1b81fae1-a266-4538-a66c-2b0b0e274a81 \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Updated task title" \
  --description "Updated description"
```

### task delete

Delete a task by ID.

```
agentapi-proxy client task delete <taskId> [flags]
```

**Example:**

```bash
agentapi-proxy client task delete 1b81fae1-a266-4538-a66c-2b0b0e274a81 \
  --endpoint http://proxy:8080 \
  --session-id $AGENTAPI_SESSION_ID
```

## REST API Reference

### POST /tasks

See [API_REFERENCE.md](API_REFERENCE.md#post-tasks) for full details.

### GET /tasks

See [API_REFERENCE.md](API_REFERENCE.md#get-tasks) for full details.

### GET /tasks/:taskId

See [API_REFERENCE.md](API_REFERENCE.md#get-taskstaskid) for full details.

### PATCH /tasks/:taskId

Update a task. Use the CLI for this operation (see above).

### DELETE /tasks/:taskId

See [API_REFERENCE.md](API_REFERENCE.md#delete-taskstaskid) for full details.

## Usage Patterns

### Agent Reporting Work Progress

An AI agent can create tasks to communicate what it is working on and report completion:

```bash
# At the start of work - create an agent task
TASK=$(agentapi-proxy client task create \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Implement user authentication" \
  --task-type agent)
TASK_ID=$(echo "$TASK" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

# ... perform work ...

# When done - mark as done
agentapi-proxy client task update "$TASK_ID" \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --status done
```

### Requesting Human Review

After creating a PR, an agent can create a user task to prompt a human to review:

```bash
# After creating a PR, notify the user
agentapi-proxy client task create \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Review and merge PR" \
  --task-type user \
  --description "The feature implementation is complete. Please review and merge." \
  --link "https://github.com/owner/repo/pull/123|PR #123"
```

### Grouping Related Tasks

Use `--group-id` to group related tasks together:

```bash
GROUP="sprint-2024-01-wk3"

agentapi-proxy client task create \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Write unit tests" \
  --task-type agent \
  --group-id "$GROUP"

agentapi-proxy client task create \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --title "Update documentation" \
  --task-type agent \
  --group-id "$GROUP"

# List tasks in the group
agentapi-proxy client task list \
  --endpoint $AGENTAPI_PROXY_BASE_URL \
  --session-id $AGENTAPI_SESSION_ID \
  --group-id "$GROUP"
```

## Environment Variables

The CLI uses the following environment variables when set:

| Variable                  | Description                         |
|---------------------------|-------------------------------------|
| `AGENTAPI_KEY`            | API key for authentication          |
| `AGENTAPI_PROXY_BASE_URL` | Base URL of the agentapi-proxy      |
| `AGENTAPI_SESSION_ID`     | Current session ID                  |
