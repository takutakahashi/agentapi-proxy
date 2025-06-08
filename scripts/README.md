# Scripts

This directory contains scripts for running and testing the agentapi-proxy.

## Mock AgentAPI Server

### start-mock-agentapi.sh

A mock implementation of the agentapi server for e2e testing. This script compiles and runs a Go program that implements the full agentapi OpenAPI specification.

#### Features

- **Complete API Implementation**: Implements all agentapi endpoints (`/status`, `/message`, `/messages`, `/events`)
- **Real-time SSE**: Server-Sent Events for live updates during agent processing
- **Conversation Management**: Tracks message history and agent status
- **Realistic Simulation**: Simulates agent thinking and response generation with delays
- **Easy Configuration**: Configurable port and verbose logging

#### Usage

```bash
# Start on default port 8080
./scripts/start-mock-agentapi.sh

# Start on custom port
./scripts/start-mock-agentapi.sh 9000

# Start with verbose logging
./scripts/start-mock-agentapi.sh -p 9000 -v

# Show help
./scripts/start-mock-agentapi.sh --help
```

#### API Endpoints

The mock server implements these endpoints according to the agentapi specification:

- `GET /status` - Returns agent status ("stable" or "running")
- `POST /message` - Send messages to the agent
- `GET /messages` - Retrieve conversation history
- `GET /events` - Server-Sent Events stream for real-time updates
- `GET /health` - Health check endpoint

#### Example Requests

```bash
# Check agent status
curl http://localhost:8080/status

# Send a message
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello!", "type": "user"}'

# Get conversation history
curl http://localhost:8080/messages

# Listen to events (SSE)
curl -N http://localhost:8080/events
```

### agentapi_with_github.sh

Original script for starting agentapi with GitHub integration.

## E2E Testing

The mock server is designed to work with the e2e test suite. See `test/e2e.sh` for a complete example of how to:

1. Start the mock server
2. Configure the proxy to route to it
3. Run tests against the complete system

## Development

### Building the Mock Server Manually

```bash
# Compile the mock server
go build -o mock-agentapi-server scripts/mock-agentapi-server.go

# Run it directly
./mock-agentapi-server 8080 -v
```

### Modifying Mock Behavior

Edit `mock-agentapi-server.go` to customize:

- Response delays and timing
- Simulated agent responses
- Error conditions for testing
- Additional endpoints

The mock server is designed to be easily extensible for different testing scenarios.