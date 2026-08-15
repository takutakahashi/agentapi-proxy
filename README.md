# agentapi-proxy

A session-based proxy server for [coder/agentapi](https://github.com/coder/agentapi) that provides process provisioning and lifecycle management for multiple agentapi server instances.

## Features

- **Session Management**: Create and manage multiple agentapi server instances with unique session IDs
- **Process Provisioning**: Dynamically spawn agentapi servers on available ports
- **Environment Configuration**: Pass custom environment variables to agentapi server instances
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
2. **Request Routing**: `/:sessionId/*` routes requests to the appropriate backend server
3. **Session Discovery**: `/search` endpoint lists and filters active sessions

Each session runs an independent agentapi server process on a unique port, allowing isolated workspaces for different users or projects.

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

### API-only container

Use the API-only image when agent execution and session tooling are hosted by
separate workers/session managers. The image contains only the statically linked
`ccplant` binary, CA certificates, and timezone data; it runs as UID/GID 999 and
does not include Claude, Codex, Cursor, Docker, mise, uv, GitHub MCP, otelcol,
Git, or shell-based session tooling.

```bash
# Build locally (linux/amd64 or linux/arm64)
docker build --target api -t ccplant-api:local backend

# Run with the default file-backed store
docker run --rm -p 8080:8080 \
  -e AGENTAPI_KV_STORE_BACKEND=libsql \
  -e AGENTAPI_KV_STORE_DATABASE_URL=file:///tmp/agentapi-api.db \
  ccplant-api:local

curl --fail http://localhost:8080/health
```

Production deployments should set `AGENTAPI_KV_STORE_BACKEND` and
`AGENTAPI_KV_STORE_DATABASE_URL` (plus `AGENTAPI_KV_STORE_AUTH_TOKEN` when the
database requires one). Set `AGENTAPI_SESSION_MANAGER_API_URL` and
`AGENTAPI_SESSION_MANAGER_API_TOKEN` when session execution is remote.
Authentication, Redis, encryption, asset storage, and provider-specific environment variables remain
feature-dependent; the Helm chart renders these from `api.*` values. Mount a
JSON/YAML config and add `server --config /path/to/config` only when file-based
configuration is preferred. Static API/OpenAPI content is embedded in the Go
binary, so `backend/public` and source configuration examples are not copied.

The published multi-architecture image is
`ghcr.io/ccplant/ccplant-api:<release-tag>`. The existing
`ccplant-backend` image remains the session/runtime default and is required by
session-manager, provisioner, and direct/local session modes. The Helm chart
uses `ccplant-api` for the backend API and worker Deployments by default.
Session Pod images can be overridden with the Helm `kubernetesSession.image`
value (or `sessionManager.kubernetesSession.image` for the dedicated manager),
and with `AGENTAPI_K8S_SESSION_IMAGE` when running without Helm.

## Usage

Grafana Cloud Application Observability 向けの OpenTelemetry 設定は
[Grafana Cloud APM setup](../docs/grafana-cloud-apm.md) を参照してください。

### Starting the Server

```bash
# Using the built binary
./bin/ccplant server

# With custom configuration
./bin/ccplant server --config config.json --port 8080 --verbose

# Using Docker
docker run -p 8080:8080 -v $(pwd)/config.json:/app/config.json ghcr.io/takutakahashi/agentapi-proxy:latest server
```

### Command Line Options

- `--port, -p`: Port to listen on (default: 8080)
- `--config, -c`: Configuration file path (default: config.json)
- `--verbose, -v`: Enable verbose logging

### Checking a Helm Installation

Use `doctor` to inspect the latest Helm release revision and verify every
Kubernetes Secret referenced by its user-supplied values. The command checks
that each Secret exists and that referenced keys contain non-empty data. It
never prints Secret values.

```bash
agentapi-proxy doctor \
	--namespace agentapi-ui
```

By default, both the `agentapi-proxy` and `agentapi-ui` releases are checked.
Use one or more `--release` flags to select specific releases:

```bash
agentapi-proxy doctor -n agentapi-ui \
  --release agentapi-proxy \
  --release agentapi-ui
```

The command uses the standard Kubernetes client configuration resolution
(in-cluster credentials or `KUBECONFIG`) and exits non-zero if a release or
startup Deployment is unavailable, or if a startup-required Secret, key, or
value is missing. Secrets for optional features such as GitHub, VAPID, SCIA,
Slack, and ingress TLS are reported as `WARN` when incomplete. Sensitive-looking
literal values and missing Service endpoints are also non-blocking warnings.

### Planning a Helm Chart Migration

Use the read-only migration preflight before moving split backend/frontend
releases to the `ccplant` chart:

```bash
agentapi-proxy helm migrate plan \
  --namespace agentapi-ui \
  --version 0.3.2 \
  --values-out ccplant-shadow-values.yaml
```

The command checks the stable `control` Service, shared session RBAC, referenced
Secrets, runtime resource ownership, PVCs, and legacy session callbacks. It
generates mode-`0600` shadow values and prints suggested Helm/kubectl commands,
but never changes the cluster or executes those commands. A blocking finding
causes a non-zero exit status. Use `--output json` or `--output yaml` for
automation.

After the shadow release is installed, verify it without changing routing:

```bash
agentapi-proxy helm migrate verify \
  --namespace agentapi-ui \
  --target-release ccplant
```

`verify` checks rollout readiness, Service endpoints, `/health` through the
Kubernetes API Service proxy, current `control` routing, runtime resources, and
the shared release-independent worker Leases. Run it once during shadowing and
again after switching `control`; the reported phase changes from `shadow` to
`cutover`.

### Configuration

Configuration is managed through environment variables and Kubernetes ConfigMaps. See the Helm chart values for detailed configuration options.

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

For detailed API documentation, see [docs/api.md](docs/api.md).

For External Session Manager setup, including allocator-mode registration and
External Session Manager configuration, see [docs/external-session-manager.md](docs/external-session-manager.md).

## Authentication

agentapi-proxy supports flexible authentication mechanisms:

- **Static API Keys**: Pre-configured API keys with role-based permissions
- **GitHub Token Authentication**: Authenticate users via GitHub personal access tokens
- **GitHub OAuth Flow**: Full OAuth2 flow for web applications
- **Hybrid Mode**: Combine multiple authentication methods

### Quick Start

#### Static API Keys
```json
{
  "auth": {
    "enabled": true,
    "static": {
      "enabled": true,
      "header_name": "X-API-Key",
      "api_keys": [
        {
          "key": "your-api-key",
          "user_id": "alice",
          "role": "admin",
          "permissions": ["*"]
        }
      ]
    }
  }
}
```

#### GitHub OAuth Setup
```json
{
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "oauth": {
        "client_id": "${GITHUB_CLIENT_ID}",
        "client_secret": "${GITHUB_CLIENT_SECRET}",
        "scope": "read:user read:org project"
      }
    }
  }
}
```

For detailed setup instructions:
- [GitHub Token Authentication](docs/github-authentication.md)
- [GitHub OAuth Flow](docs/github-oauth.md)
- [GitHub OAuth Quick Start](docs/github-oauth-quickstart.md)
- [RBAC Configuration](docs/rbac.md)

### Try the OAuth Demo
Check out the [OAuth Demo Application](examples/oauth-demo/) to see GitHub OAuth in action.

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

- **CCPLANT_BINARY_PATH**: Path to the proxy executable used by containers,
  provisioners, ACP startup commands, and generated hooks. Defaults to
  `agentapi-proxy`.
- **GITHUB_TOKEN**: GitHub personal access token
- **WORKSPACE_NAME**: Custom workspace identifier  
- **DEBUG**: Enable debug mode for agentapi

## License

See LICENSE file for details.
