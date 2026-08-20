# Xapi Documentation

## Overview

This document describes the extended API (xapi) implementation for agentapi-proxy, which provides session-based proxy capabilities for [coder/agentapi](https://github.com/coder/agentapi) with enhanced features and management.

## API Specification

### Base URL
The agentapi-proxy server runs on port 8080 by default, providing access to both proxy management and forwarded agentapi endpoints.

### Authentication
Currently, no authentication is required for API access.

## Core Endpoints

### Session Management

#### POST /start
Creates a new agentapi server instance with a unique session ID.

**Request Body:**
```json
{
  "user_id": "string",
  "environment": {
    "KEY": "value"
  },
  "tags": {
    "tag_key": "tag_value"
  }
}
```

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Features:**
- Supports custom environment variables
- Allows tagging for session categorization
- Returns unique session identifier for future requests

#### GET /search
Retrieves and filters existing sessions.

**Query Parameters:**
- `user_id`: Filter by user ID
- `status`: Filter by session status ('active', 'inactive')
- `tag.{key}`: Filter by tag values (e.g., `tag.repository=agentapi-proxy`)

**Response:**
```json
{
  "sessions": [
    {
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "user_id": "alice",
      "status": "active",
      "started_at": "2024-01-01T12:00:00Z",
      "tags": {
        "repository": "agentapi-proxy",
        "branch": "main",
        "env": "production"
      }
    }
  ]
}
```

#### DELETE /sessions/:sessionId
Terminates a specific session and cleans up associated resources.

**Parameters:**
- `sessionId`: The session identifier to terminate

**Response:**
```json
{
  "message": "Session terminated successfully"
}
```

### Request Forwarding

#### ANY /:sessionId/*
Forwards all requests to the appropriate agentapi server instance.

**Path Parameters:**
- `sessionId`: Target session identifier
- `*`: Any path to be forwarded to the agentapi server

**Behavior:**
- Preserves all HTTP methods (GET, POST, PUT, DELETE, etc.)
- Forwards headers and request body
- Returns the exact response from the target agentapi server
- Handles both REST API calls and Server-Sent Events (SSE)

## AgentAPI Integration

The proxy integrates with the core agentapi endpoints as defined in the [OpenAPI specification](https://github.com/coder/agentapi/blob/main/openapi.json):

### Forwarded Endpoints

#### GET /:sessionId/events
Server-Sent Events endpoint for real-time conversation updates.

**Response:** Stream of SSE events containing:
- Conversation state changes
- Agent status updates
- Message processing events

#### POST /:sessionId/message
Send messages to the agent for processing.

**Request Body:**
```json
{
  "content": "Your message content",
  "type": "user"
}
```

**Message Types:**
- `user`: Standard user messages (requires agent status 'stable')
- `raw`: Raw messages that bypass status checks

#### GET /:sessionId/messages
Retrieve conversation history for the session.

**Response:**
```json
{
  "messages": [
    {
      "id": "message-id",
      "content": "Message content",
      "role": "user|assistant",
      "timestamp": "2024-01-01T12:00:00Z"
    }
  ]
}
```

#### GET /:sessionId/status
Get current agent status for the session.

**Response:**
```json
{
  "status": "stable|running"
}
```

**Status Values:**
- `stable`: Agent is ready to receive user messages
- `running`: Agent is currently processing

## Configuration

### Server Configuration
The proxy server can be configured via `config.json` or environment variables. See the main README for detailed configuration options.

### Environment Variables
Sessions support custom environment variables passed during creation:

- `GITHUB_TOKEN`: GitHub personal access token for repository access
- `WORKSPACE_NAME`: Custom workspace identifier
- `DEBUG`: Enable debug mode for agentapi instances
- Custom variables as needed per session

### Scripts
The proxy includes embedded startup scripts:

- `agentapi_default.sh`: Standard agentapi startup
- `agentapi_with_github.sh`: Enhanced startup with GitHub integration

## Error Handling

### Common Error Responses

#### 404 Not Found
```json
{
  "error": "Session not found",
  "session_id": "invalid-session-id"
}
```

#### 400 Bad Request
```json
{
  "error": "Invalid request format",
  "details": "Missing required field: user_id"
}
```

#### 500 Internal Server Error
```json
{
  "error": "Failed to start agentapi server",
  "details": "Port allocation failed"
}
```

## Usage Examples

### Complete Workflow

1. **Create a session:**
```bash
curl -X POST http://localhost:8080/start \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "developer",
    "environment": {
      "GITHUB_TOKEN": "ghp_xxx"
    },
    "tags": {
      "project": "ai-assistant",
      "environment": "development"
    }
  }'
```

2. **Send a message to the agent:**
```bash
curl -X POST http://localhost:8080/550e8400-e29b-41d4-a716-446655440000/message \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Hello, how can you help me today?",
    "type": "user"
  }'
```

3. **Check agent status:**
```bash
curl http://localhost:8080/550e8400-e29b-41d4-a716-446655440000/status
```

4. **Search for sessions:**
```bash
curl "http://localhost:8080/search?user_id=developer&tag.project=ai-assistant"
```

5. **Clean up session:**
```bash
curl -X DELETE http://localhost:8080/sessions/550e8400-e29b-41d4-a716-446655440000
```

## Architecture Notes

### Pod Management
- Each session runs as an independent Kubernetes Pod
- Pods are managed through Kubernetes Deployments for lifecycle management
- Session cleanup properly terminates associated Pods and resources
- Graceful shutdown handles all active sessions

### Request Routing
- URL path-based routing directs requests to Kubernetes Services
- Full HTTP method support with header and body preservation
- WebSocket and SSE support for real-time communication
- DNS-based service discovery for backend connectivity

### Session Lifecycle
1. **Creation**: Deployment creation, Pod startup, Service registration
2. **Active**: Request forwarding via Service DNS, status monitoring
3. **Termination**: Pod cleanup, resource deallocation (Deployment, Service, PVC)

This extended API provides a robust foundation for managing multiple agentapi instances in Kubernetes while maintaining the core functionality and adding session-based isolation and management capabilities.