# Browser Access to AgentAPI

This document explains how to use the browser-compatible AgentAPI client that has been implemented in this UI.

## Overview

The browser-compatible AgentAPI client provides direct access to the actual agentapi from your web browser. This implementation is based on the [official agentapi OpenAPI specification](https://github.com/coder/agentapi/blob/main/openapi.json).

## Features

- **Real-time messaging**: Send messages to the agent and receive responses
- **Live status monitoring**: Monitor agent status (stable/running)
- **Server-Sent Events**: Real-time updates for messages and status changes
- **Error handling**: Robust error handling with retry logic
- **Browser-compatible**: Works directly in modern web browsers

## Usage

### Quick Start

1. Set up your environment variables (copy `.env.example` to `.env.local`):
   ```bash
   cp .env.example .env.local
   ```

2. Configure the AgentAPI URL in your `.env.local`:
   ```env
   NEXT_PUBLIC_REAL_AGENTAPI_URL=http://localhost:8080
   ```

3. Start the development server:
   ```bash
   bun run dev
   ```

4. Open your browser and navigate to `/agentapi` to access the chat interface.

### Using the Chat Interface

1. **Status Indicator**: The top-right shows the connection status and agent status
   - Green dot = Connected/Stable
   - Yellow dot = Agent Running
   - Red dot = Disconnected/Error

2. **Sending Messages**: 
   - Type your message in the text area
   - Press Enter to send (Shift+Enter for new line)
   - Wait for agent to be "stable" before sending new messages

3. **Real-time Updates**: Messages and status changes are updated in real-time via Server-Sent Events

## API Client

### RealAgentAPIClient

The `RealAgentAPIClient` class provides programmatic access to the agentapi:

```typescript
import { createRealAgentAPIClient } from '@/lib/real-agentapi-client';

const client = createRealAgentAPIClient({
  baseURL: 'http://localhost:8080',
  timeout: 10000,
  debug: true
});

// Get agent status
const status = await client.getStatus();

// Send a message
const response = await client.sendMessage({
  content: "Hello, agent!",
  type: "user"
});

// Subscribe to real-time events
const eventSource = client.subscribeToEvents(
  (message) => console.log('New message:', message),
  (status) => console.log('Status changed:', status),
  (error) => console.error('Error:', error)
);
```

### Available Methods

- `getStatus()`: Get current agent status
- `getMessages()`: Retrieve message history
- `sendMessage(request)`: Send a message to the agent
- `subscribeToEvents()`: Subscribe to real-time updates
- `createEventSource()`: Create raw EventSource connection
- `isReady()`: Check if agent is ready to receive messages
- `healthCheck()`: Check if API is available

## Environment Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXT_PUBLIC_REAL_AGENTAPI_URL` | AgentAPI server URL | `http://localhost:8080` |
| `AGENTAPI_TIMEOUT` | Request timeout (ms) | `10000` |
| `NODE_ENV` | Environment mode | `development` |

## CORS Configuration

For browser access to work, the agentapi server must be configured to allow CORS requests from your UI domain. The agentapi server should include headers like:

```
Access-Control-Allow-Origin: http://localhost:3000
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
```

## Troubleshooting

### Connection Issues

1. **"AgentAPI is not available"**: 
   - Check that the agentapi server is running
   - Verify the `NEXT_PUBLIC_REAL_AGENTAPI_URL` is correct
   - Ensure CORS is properly configured

2. **"Real-time connection lost"**:
   - Check network connectivity
   - The client will attempt to reconnect automatically

3. **"Agent is currently running"**:
   - Wait for the agent status to change to "stable"
   - This is normal when the agent is processing a request

### Development

To enable debug logging, set the client debug mode:

```typescript
client.setDebug(true);
```

Or set `NODE_ENV=development` in your environment.

## Technical Details

### Message Types

- **user**: Normal user messages (saved to history)
- **raw**: Raw terminal input (not saved to history)

### Event Types

- **message_update**: New or updated message
- **status_change**: Agent status changed

### Error Handling

The client uses RFC 7807 Problem Details format for errors and includes automatic retry logic for network errors and 5xx responses.