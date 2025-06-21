# agentapi-proxy

A session-based proxy server for [coder/agentapi](https://github.com/coder/agentapi) that provides process provisioning and lifecycle management for multiple agentapi server instances.

## Features

- **Session Management**: Create and manage multiple agentapi server instances with unique session IDs
- **Process Provisioning**: Dynamically spawn agentapi servers on available ports
- **Environment Configuration**: Pass custom environment variables to agentapi server instances
- **Profile Management**: Create and manage user profiles with environment variables, repository history, system prompts, and message templates
- **Profile-based Sessions**: Start sessions using predefined profiles for consistent development environments
- **Script Support**: Execute custom startup scripts (with GitHub integration support)
- **Session Search**: Query and filter active sessions by user ID and status
- **Request Routing**: Proxy requests to appropriate agentapi server instances based on session ID
- **Authentication & Authorization**: Role-based access control with API key management
- **Session Persistence**: Optional session data persistence across server restarts
- **Graceful Shutdown**: Proper cleanup of all running sessions on server shutdown
- **Client Library**: Go client for programmatic interaction with the proxy server

## Architecture

The proxy acts as a reverse proxy and process manager:

1. **Session Creation**: `/start` endpoint creates new agentapi server instances
2. **Profile Management**: `/profiles/*` endpoints manage user profiles and configurations
3. **Profile-based Sessions**: `/start-with-profile` creates sessions using profile configurations
4. **Request Routing**: `/:sessionId/*` routes requests to the appropriate backend server
5. **Session Discovery**: `/search` endpoint lists and filters active sessions

Each session runs an independent agentapi server process on a unique port, allowing isolated workspaces for different users or projects. Profiles provide a way to standardize and reuse configurations across sessions.

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/takutakahashi/agentapi-proxy.git
cd agentapi-proxy

# Install dependencies
make install-deps

# Build the binary
make build
```

### Using Docker

```bash
docker pull ghcr.io/takutakahashi/agentapi-proxy:latest
```

## Usage

### Starting the Server

```bash
# Using the built binary
./bin/agentapi-proxy server

# With custom configuration
./bin/agentapi-proxy server --config config.json --port 8080 --verbose

# Using Docker
docker run -p 8080:8080 -v $(pwd)/config.json:/app/config.json ghcr.io/takutakahashi/agentapi-proxy:latest server
```

### Command Line Options

- `--port, -p`: Port to listen on (default: 8080)
- `--config, -c`: Configuration file path (default: config.json)
- `--verbose, -v`: Enable verbose logging

### Configuration

Create a `config.json` file:

```json
{
  "start_port": 9000
}
```

#### Configuration Fields

- `start_port`: Starting port number for agentapi server instances (default: 9000)

## API Endpoints

### Session Management

#### Create Session

**POST** `/start`

Create a new agentapi server instance.

```bash
curl -X POST http://localhost:8080/start \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice",
    "environment": {
      "GITHUB_TOKEN": "your-token",
      "WORKSPACE_NAME": "my-project"
    }
  }'
```

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

#### Create Session with Profile

**POST** `/start-with-profile`

Create a new agentapi server instance using a predefined profile.

```bash
curl -X POST http://localhost:8080/start-with-profile \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "profile_id": "profile-uuid-here",
    "environment": {
      "OVERRIDE_VAR": "session_value"
    },
    "tags": {
      "session_type": "development"
    }
  }'
```

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

#### Search Sessions

**GET** `/search`

List and filter active sessions.

```bash
# List all sessions
curl http://localhost:8080/search

# Filter by user ID
curl http://localhost:8080/search?user_id=alice

# Filter by status
curl http://localhost:8080/search?status=active
```

**Response:**
```json
{
  "sessions": [
    {
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "user_id": "alice",
      "status": "active",
      "started_at": "2024-01-01T12:00:00Z",
      "port": 9000
    }
  ]
}
```

#### Route to Session

**ANY** `/:sessionId/*`

Route requests to the agentapi server instance for the given session.

```bash
# Forward request to session's agentapi server
curl http://localhost:8080/550e8400-e29b-41d4-a716-446655440000/api/workspaces
```

### Profile Management

#### Create Profile

**POST** `/profiles`

Create a new user profile with environment variables, system prompts, and templates.

```bash
curl -X POST http://localhost:8080/profiles \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "Development Environment",
    "description": "Settings for development work",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true"
    },
    "system_prompt": "You are a development assistant. Help with code review and debugging.",
    "message_templates": [
      {
        "name": "Code Review",
        "content": "Please review this code: {{code}}",
        "variables": ["code"],
        "category": "review"
      }
    ]
  }'
```

#### List Profiles

**GET** `/profiles`

List all profiles for the authenticated user.

```bash
curl -H "X-API-Key: your-api-key" http://localhost:8080/profiles
```

#### Get Profile

**GET** `/profiles/:profileId`

Get details of a specific profile.

```bash
curl -H "X-API-Key: your-api-key" http://localhost:8080/profiles/profile-uuid-here
```

#### Update Profile

**PUT** `/profiles/:profileId`

Update an existing profile. Supports partial updates.

```bash
curl -X PUT http://localhost:8080/profiles/profile-uuid-here \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "name": "Updated Development Environment",
    "environment": {
      "NODE_ENV": "development",
      "DEBUG": "true",
      "NEW_VAR": "new_value"
    }
  }'
```

#### Delete Profile

**DELETE** `/profiles/:profileId`

Delete a profile.

```bash
curl -X DELETE -H "X-API-Key: your-api-key" http://localhost:8080/profiles/profile-uuid-here
```

For detailed API documentation, see [docs/api.md](docs/api.md) and [docs/profile-api.md](docs/profile-api.md).

## Client Library

Use the Go client library for programmatic access:

```go
package main

import (
    "context"
    "log"
    
    "github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func main() {
    // Create client
    c := client.NewClient("http://localhost:8080")
    
    // Start new session
    resp, err := c.Start(context.Background(), &client.StartRequest{
        UserID: "alice",
        Environment: map[string]string{
            "GITHUB_TOKEN": "your-token",
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Created session: %s", resp.SessionID)
    
    // Search sessions
    sessions, err := c.Search(context.Background(), "alice", "active")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Found %d sessions", len(sessions.Sessions))
}
```

## Development

### Prerequisites

- Go 1.23+
- [golangci-lint](https://golangci-lint.run/)
- [coder/agentapi](https://github.com/coder/agentapi) binary (for testing)

### Building and Testing

```bash
# Format code
make gofmt

# Run linting
make lint

# Run tests
make test

# Run full CI pipeline
make ci

# Build binary
make build

# Run end-to-end tests (requires agentapi binary)
make e2e
```

### Project Structure

```
├── cmd/
│   └── agentapi-proxy/     # Binary entry point
├── pkg/
│   ├── client/             # Go client library
│   ├── config/             # Configuration management
│   └── proxy/              # Core proxy server logic
│       └── scripts/        # Embedded startup scripts
├── docs/                   # Documentation
└── .github/workflows/      # CI/CD pipelines
```

## Scripts

The proxy supports custom startup scripts for agentapi servers:

- `agentapi_default.sh`: Default startup script
- `agentapi_with_github.sh`: Script with GitHub integration

Scripts are embedded in the binary and extracted to temporary files at runtime.

## Environment Variables

Sessions can receive custom environment variables:

- **GITHUB_TOKEN**: GitHub personal access token
- **WORKSPACE_NAME**: Custom workspace identifier  
- **DEBUG**: Enable debug mode for agentapi

## License

See LICENSE file for details.