# AgentAPI Client Architecture Documentation

## Overview

This document describes the AgentAPI client architecture for the UI application. The client is designed with a two-layer architecture to provide both direct API access and session management capabilities.

## Architecture Layers

### Layer 1: AgentAPIClient

The `AgentAPIClient` is the foundational layer that handles direct communication with AgentAPI endpoints based on the [official OpenAPI specification](https://github.com/coder/agentapi/blob/main/openapi.json).

#### Core Responsibilities
- **HTTP Communication**: Handles all HTTP requests and responses
- **Authentication**: Manages API key authentication and authorization
- **Error Handling**: Provides consistent error handling with retry logic
- **Rate Limiting**: Respects and reports API rate limits
- **WebSocket Support**: Manages real-time connections for live updates

#### Key Features
- Automatic retry with exponential backoff
- Request timeout handling
- Debug logging capabilities
- TypeScript type safety
- Rate limit awareness

#### Example Usage
```typescript
import { AgentAPIClient, createAgentAPIClient } from './lib/agentapi-client';

// Create client with configuration
const client = createAgentAPIClient({
  baseURL: 'https://api.example.com',
  apiKey: 'your-api-key',
  timeout: 10000,
  debug: true
});

// Basic agent operations
const agents = await client.getAgents();
const agent = await client.createAgent({
  name: 'My Agent',
  config: { type: 'chatbot', parameters: {} }
});

// Session management
const sessions = await client.getSessions();
const session = await client.createSession({
  user_id: 'user123',
  metadata: { source: 'web-ui' }
});
```

### Layer 2: AgentAPIProxyClient

The `AgentAPIProxyClient` builds on top of `AgentAPIClient` to provide high-level session management and operational control. This layer is inspired by the [agentapi-proxy](https://github.com/takutakahashi/agentapi-proxy) implementation.

#### Core Responsibilities
- **Session Lifecycle**: Manages session creation, monitoring, and cleanup
- **Session Routing**: Routes requests to appropriate session instances
- **Resource Management**: Handles session resources and cleanup
- **Bulk Operations**: Provides batch operations across multiple sessions

#### Key Features
- Session pooling and reuse
- Automatic session cleanup
- Batch operations (start, search, delete)
- Session health monitoring
- Event aggregation across sessions

#### Conceptual Usage
```typescript
import { AgentAPIProxyClient } from './lib/agentapi-proxy-client';

// Create proxy client
const proxyClient = new AgentAPIProxyClient({
  agentApiConfig: {
    baseURL: 'https://api.example.com',
    apiKey: 'your-api-key'
  },
  maxSessions: 10,
  sessionTimeout: 300000
});

// High-level operations
await proxyClient.start('user123');
const results = await proxyClient.search({ query: 'example' });
await proxyClient.delete('session-id');

// Session management
const sessionClient = proxyClient.getSession('session-id');
await sessionClient.sendMessage({ content: 'Hello' });
```

## Current Implementation Status

### Implemented Components

#### 1. AgentAPIClient (`src/lib/agentapi-client.ts`)
- ✅ Full HTTP client implementation
- ✅ Authentication and authorization
- ✅ Error handling with retry logic
- ✅ Session management methods
- ✅ WebSocket support
- ✅ Rate limiting awareness
- ✅ TypeScript type definitions

#### 2. RealAgentAPIClient (`src/lib/real-agentapi-client.ts`)
- ✅ Direct AgentAPI integration
- ✅ Server-Sent Events (SSE) support
- ✅ Status and message management
- ✅ Lightweight implementation
- ✅ Based on OpenAPI specification

#### 3. UnifiedAgentAPIClient (`src/lib/unified-agentapi-client.ts`)
- ✅ Combines real and mock clients
- ✅ Automatic fallback mechanisms
- ✅ Mode switching capabilities
- ✅ Feature availability detection
- ✅ Health check integration

### Missing Components

#### AgentAPIProxyClient Implementation
- ❌ Session pooling and management
- ❌ Bulk operations interface
- ❌ Resource cleanup automation
- ❌ Event aggregation
- ❌ Session routing logic

## API Reference

### AgentAPIClient Methods

#### Agent Management
```typescript
// List agents with filtering
getAgents(params?: AgentListParams): Promise<AgentListResponse>

// Get specific agent
getAgent(id: string): Promise<Agent>

// Create new agent
createAgent(data: CreateAgentRequest): Promise<Agent>

// Update existing agent
updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent>

// Delete agent
deleteAgent(id: string): Promise<void>
```

#### Session Management
```typescript
// List sessions
getSessions(params?: SessionListParams): Promise<SessionListResponse>

// Create new session
createSession(data: CreateSessionRequest): Promise<Session>

// Delete session
deleteSession(sessionId: string): Promise<void>

// Get session-specific client
getSessionAgentAPI(sessionId: string): Promise<AgentAPIClient>
```

#### Messaging
```typescript
// Get session messages
getSessionMessages(sessionId: string, params?: SessionMessageListParams): Promise<SessionMessageListResponse>

// Send message to session
sendSessionMessage(sessionId: string, data: SendSessionMessageRequest): Promise<SessionMessage>

// Get session status
getSessionStatus(sessionId: string): Promise<AgentStatus>
```

#### Real-time Events
```typescript
// Create WebSocket connection
createWebSocket(options?: WebSocketOptions): WebSocket

// Subscribe to agent updates
subscribeToAgents(ws: WebSocket, agentId?: string): void

// Subscribe to metrics updates
subscribeToMetrics(ws: WebSocket, interval?: number): void

// Subscribe to session events (SSE)
subscribeToSessionEvents(
  sessionId: string,
  onMessage: (message: SessionMessage) => void,
  onStatus?: (status: AgentStatus) => void,
  onError?: (error: Error) => void
): EventSource
```

#### Metrics and Configuration
```typescript
// Get system metrics
getMetrics(): Promise<Metrics>

// Get agent-specific metrics
getAgentMetrics(agentId: string): Promise<AgentMetrics>

// Get metrics history
getMetricsHistory(params: MetricsHistoryParams): Promise<Metrics[]>

// Get system configuration
getConfig(): Promise<SystemConfig>

// Update system configuration
updateConfig(config: Partial<SystemConfig>): Promise<SystemConfig>
```

#### Utility Methods
```typescript
// Health check
healthCheck(): Promise<boolean>

// Set API key
setApiKey(apiKey: string): void

// Toggle debug mode
setDebug(debug: boolean): void
```

### RealAgentAPIClient Methods

#### Core Methods
```typescript
// Get agent status
getStatus(): Promise<AgentStatus>

// Get all messages
getMessages(): Promise<MessagesResponse>

// Send message
sendMessage(request: SendMessageRequest): Promise<SendMessageResponse>

// Check if agent is ready
isReady(): Promise<boolean>

// Health check
healthCheck(): Promise<boolean>
```

#### Event Handling
```typescript
// Create EventSource for real-time updates
createEventSource(): EventSource

// Subscribe to events with callbacks
subscribeToEvents(
  onMessage: (message: Message) => void,
  onStatusChange: (status: AgentStatus) => void,
  onError?: (error: Error) => void
): EventSource
```

### UnifiedAgentAPIClient Methods

#### Mode Management
```typescript
// Get current client mode
getCurrentMode(): ClientMode

// Check if feature is available
isFeatureAvailable(feature: string): boolean

// Force mode change
setMode(mode: ClientMode): Promise<void>

// Get debug information
getDebugInfo(): { mode: ClientMode, hasRealClient: boolean, hasMockClient: boolean }
```

#### Unified Interface
```typescript
// Core messaging (available in all modes)
getStatus(): Promise<AgentStatus>
getMessages(): Promise<MessagesResponse>
sendMessage(request: SendMessageRequest): Promise<SendMessageResponse>

// Advanced features (mock mode only)
getAgents?(params?: AgentListParams): Promise<AgentListResponse>
createAgent?(data: CreateAgentRequest): Promise<Agent>
getMetrics?(): Promise<Metrics>
```

## Type Definitions

### Core Types

#### Agent Types
```typescript
interface Agent {
  id: string;
  name: string;
  status: 'active' | 'inactive' | 'error';
  created_at: string;
  updated_at: string;
  config: AgentConfig;
  metrics?: AgentMetrics;
}

interface AgentConfig {
  type: string;
  parameters: Record<string, unknown>;
  timeout?: number;
  retry_policy?: {
    max_retries: number;
    backoff_factor: number;
  };
}
```

#### Session Types
```typescript
interface Session {
  session_id: string;
  user_id: string;
  status: 'active' | 'inactive' | 'error';
  created_at: string;
  updated_at: string;
  environment?: Record<string, string>;
  metadata?: Record<string, unknown>;
  tags?: Record<string, string>;
}

interface SessionMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: string;
  session_id: string;
  metadata?: Record<string, unknown>;
}
```

#### Configuration Types
```typescript
interface AgentAPIClientConfig {
  baseURL: string;
  apiKey?: string;
  timeout?: number;
  retryAttempts?: number;
  retryDelay?: number;
  debug?: boolean;
}

interface RealAgentAPIConfig {
  baseURL: string;
  timeout?: number;
  debug?: boolean;
}

interface UnifiedAgentAPIConfig {
  mode?: ClientMode;
  realApiConfig?: RealAgentAPIConfig;
  mockApiConfig?: AgentAPIClientConfig;
  fallbackToMock?: boolean;
  healthCheckTimeout?: number;
}
```

#### Error Types
```typescript
interface APIError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
  timestamp: string;
}

class AgentAPIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: Record<string, unknown>
  )
}

class RealAgentAPIError extends Error {
  constructor(
    public status: number,
    public type: string,
    message: string,
    public detail?: string
  )
}
```

## Factory Functions

### Creating Clients

#### AgentAPIClient
```typescript
// Basic client creation
const client = createAgentAPIClient({
  baseURL: 'https://api.example.com',
  apiKey: 'your-api-key'
});

// Client from storage settings
const client = createAgentAPIClientFromStorage('repo/name');

// Default client instance
import { agentAPI } from './lib/agentapi-client';
```

#### RealAgentAPIClient
```typescript
// Basic client creation
const client = createRealAgentAPIClient({
  baseURL: 'https://api.example.com'
});

// Client from storage settings
const client = createRealAgentAPIClientFromStorage('repo/name');

// Default client instance
import { realAgentAPI } from './lib/real-agentapi-client';
```

#### UnifiedAgentAPIClient
```typescript
// Basic unified client
const client = createUnifiedAgentAPIClient({
  mode: 'auto',
  fallbackToMock: true
});

// Client from storage settings
const client = createUnifiedAgentAPIClientFromStorage('repo/name');

// Default unified client instance
import { unifiedAgentAPI } from './lib/unified-agentapi-client';
```

### Configuration from Storage

Both clients support loading configuration from browser storage:

```typescript
// Global settings
const config = getAgentAPIConfigFromStorage();

// Repository-specific settings
const config = getAgentAPIConfigFromStorage('owner/repo');

// Real API configuration
const realConfig = getRealAgentAPIConfigFromStorage('owner/repo');
```

## Error Handling

### Error Types

#### AgentAPIError
- Thrown by `AgentAPIClient`
- Includes HTTP status code and error details
- Supports retry logic for transient errors

#### RealAgentAPIError
- Thrown by `RealAgentAPIClient`
- Follows RFC 7807 Problem Details format
- Includes error type and detail information

### Error Handling Patterns

```typescript
try {
  const agent = await client.createAgent(agentData);
} catch (error) {
  if (error instanceof AgentAPIError) {
    if (error.status === 429) {
      // Handle rate limiting
      console.log('Rate limited, try again later');
    } else if (error.status >= 500) {
      // Handle server errors
      console.log('Server error, retrying...');
    }
  }
}
```

## Best Practices

### 1. Client Selection
- Use `RealAgentAPIClient` for simple messaging applications
- Use `AgentAPIClient` for full-featured applications
- Use `UnifiedAgentAPIClient` for flexible mode switching

### 2. Configuration Management
- Store configuration in browser storage for persistence
- Use environment variables for default values
- Implement repository-specific configurations

### 3. Error Handling
- Always implement proper error handling
- Use retry logic for transient errors
- Provide user feedback for rate limiting

### 4. Real-time Updates
- Use WebSocket for AgentAPI features
- Use Server-Sent Events for real-time messaging
- Implement reconnection logic for stability

### 5. Performance Optimization
- Implement request caching where appropriate
- Use batch operations for multiple requests
- Monitor rate limits and implement backoff strategies

## Migration Guide

### From Direct API Calls to AgentAPIClient

Before:
```typescript
const response = await fetch('/api/agents', {
  headers: { 'Authorization': `Bearer ${apiKey}` }
});
const agents = await response.json();
```

After:
```typescript
const client = createAgentAPIClient({ baseURL, apiKey });
const agents = await client.getAgents();
```

### From Individual Clients to Unified Client

Before:
```typescript
const realClient = createRealAgentAPIClient(realConfig);
const mockClient = createAgentAPIClient(mockConfig);

// Manual switching logic
const client = isProduction ? realClient : mockClient;
```

After:
```typescript
const client = createUnifiedAgentAPIClient({
  mode: 'auto',
  fallbackToMock: true
});

// Automatic mode detection and switching
const status = await client.getStatus();
```

## Future Enhancements

### Planned Features
1. **AgentAPIProxyClient Implementation**
   - Session pooling and management
   - Bulk operations interface
   - Resource cleanup automation

2. **OpenAPI Integration**
   - Automated type generation from OpenAPI spec
   - Runtime validation against schema
   - Auto-updating client definitions

3. **Enhanced Monitoring**
   - Client-side metrics collection
   - Performance monitoring
   - Error tracking and reporting

4. **Advanced Features**
   - Request caching mechanisms
   - Offline support and queuing
   - Multi-tenant session management

### Contributing
When implementing new features:

1. Follow the existing TypeScript patterns
2. Add comprehensive error handling
3. Include unit tests for new functionality
4. Update documentation with examples
5. Ensure backward compatibility

## Related Resources

- [AgentAPI OpenAPI Specification](https://github.com/coder/agentapi/blob/main/openapi.json)
- [AgentAPI Repository](https://github.com/coder/agentapi)
- [AgentAPI Proxy Implementation](https://github.com/takutakahashi/agentapi-proxy)
- [Project Setup Guide](../CLAUDE.md)