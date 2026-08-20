# AgentAPI Documentation

This document provides comprehensive information about the AgentAPI for UI development integration.

## Overview

AgentAPI is a powerful backend service that provides agent management capabilities, metrics collection, and configuration management. This UI client provides a web interface to interact with all AgentAPI functionality.

## Architecture

The AgentAPI follows RESTful principles and is organized into several main functional areas:

### Core Components

1. **Agent Management** - Create, configure, and manage agents
2. **Metrics & Monitoring** - Real-time performance data and analytics  
3. **API Documentation** - Interactive API exploration and testing
4. **Configuration** - System settings and preferences

## API Endpoints

Based on the UI structure, the AgentAPI provides the following main endpoint categories:

### Agent Management (`/agents`)

Manage and monitor your agents through these endpoints:

#### List Agents
```http
GET /api/v1/agents
```

**Response:**
```json
{
  "agents": [
    {
      "id": "string",
      "name": "string", 
      "status": "active|inactive|error",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z",
      "config": {}
    }
  ],
  "total": 0,
  "page": 1,
  "limit": 20
}
```

#### Create Agent
```http
POST /api/v1/agents
```

**Request:**
```json
{
  "name": "string",
  "config": {
    "type": "string",
    "parameters": {}
  }
}
```

#### Get Agent Details
```http
GET /api/v1/agents/{agent_id}
```

#### Update Agent
```http
PUT /api/v1/agents/{agent_id}
```

#### Delete Agent  
```http
DELETE /api/v1/agents/{agent_id}
```

### Metrics & Monitoring (`/metrics`)

Access real-time performance data and analytics:

#### Get System Metrics
```http
GET /api/v1/metrics
```

**Response:**
```json
{
  "system": {
    "cpu_usage": 0.45,
    "memory_usage": 0.67,
    "disk_usage": 0.23
  },
  "agents": {
    "total": 10,
    "active": 8,
    "inactive": 2,
    "error": 0
  },
  "requests": {
    "total": 1500,
    "per_minute": 25,
    "success_rate": 0.98
  }
}
```

#### Get Agent Metrics
```http
GET /api/v1/metrics/agents/{agent_id}
```

#### Get Historical Data
```http
GET /api/v1/metrics/history?from=2024-01-01&to=2024-01-02&interval=1h
```

### Configuration (`/settings`)

Manage system configuration and preferences:

#### Get Configuration
```http
GET /api/v1/config
```

#### Update Configuration
```http
PUT /api/v1/config
```

**Request:**
```json
{
  "logging": {
    "level": "info|debug|warn|error",
    "format": "json|text"
  },
  "auth": {
    "enabled": true,
    "provider": "string"
  },
  "limits": {
    "max_agents": 100,
    "request_rate": 1000
  }
}
```

## Authentication

AgentAPI supports multiple authentication methods:

### API Key Authentication
Include your API key in the request headers:

```http
Authorization: Bearer YOUR_API_KEY
```

### Environment Variables
Set the following environment variables for the UI client:

```bash
AGENTAPI_BASE_URL=http://localhost:8080
AGENTAPI_API_KEY=your_api_key_here
AGENTAPI_TIMEOUT=30000
```

## Data Models

### Agent Model
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
```

### Agent Configuration
```typescript
interface AgentConfig {
  type: string;
  parameters: Record<string, any>;
  timeout?: number;
  retry_policy?: {
    max_retries: number;
    backoff_factor: number;
  };
}
```

### Metrics Model
```typescript
interface Metrics {
  timestamp: string;
  system: SystemMetrics;
  agents: AgentMetrics[];
  requests: RequestMetrics;
}

interface SystemMetrics {
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  uptime: number;
}

interface AgentMetrics {
  agent_id: string;
  requests_processed: number;
  success_rate: number;
  average_response_time: number;
  last_activity: string;
}

interface RequestMetrics {
  total: number;
  per_minute: number;
  success_rate: number;
  error_rate: number;
}
```

## Error Handling

AgentAPI uses standard HTTP status codes and provides detailed error information:

### Error Response Format
```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human readable error message",
    "details": {},
    "timestamp": "2024-01-01T00:00:00Z"
  }
}
```

### Common Error Codes
- `400` - Bad Request: Invalid request format or parameters
- `401` - Unauthorized: Invalid or missing authentication
- `403` - Forbidden: Insufficient permissions
- `404` - Not Found: Resource does not exist
- `429` - Too Many Requests: Rate limit exceeded
- `500` - Internal Server Error: Server-side error

## Rate Limiting

AgentAPI implements rate limiting to ensure fair usage:

- **Default Limit**: 1000 requests per hour per API key
- **Headers**: Rate limit information is included in response headers:
  ```http
  X-RateLimit-Limit: 1000
  X-RateLimit-Remaining: 999
  X-RateLimit-Reset: 1640995200
  ```

## WebSocket Support

For real-time updates, AgentAPI provides WebSocket endpoints:

```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/websocket');

// Subscribe to agent events
ws.send(JSON.stringify({
  type: 'subscribe',
  channel: 'agents',
  agent_id: 'optional_agent_id'
}));

// Subscribe to metrics updates
ws.send(JSON.stringify({
  type: 'subscribe', 
  channel: 'metrics',
  interval: 5000
}));
```

## UI Integration Examples

### React/Next.js Integration

```typescript
// API client setup
import { AgentAPIClient } from './lib/agentapi-client';

const apiClient = new AgentAPIClient({
  baseURL: process.env.NEXT_PUBLIC_AGENTAPI_URL,
  apiKey: process.env.AGENTAPI_API_KEY,
});

// Fetch agents
export async function getAgents() {
  try {
    const response = await apiClient.get('/agents');
    return response.data;
  } catch (error) {
    console.error('Failed to fetch agents:', error);
    throw error;
  }
}

// Create agent
export async function createAgent(config: AgentConfig) {
  return await apiClient.post('/agents', config);
}
```

### State Management (React Query Example)

```typescript
import { useQuery, useMutation, useQueryClient } from 'react-query';

export function useAgents() {
  return useQuery('agents', getAgents, {
    refetchInterval: 30000, // Refresh every 30 seconds
  });
}

export function useCreateAgent() {
  const queryClient = useQueryClient();
  
  return useMutation(createAgent, {
    onSuccess: () => {
      queryClient.invalidateQueries('agents');
    },
  });
}
```

## Best Practices

### 1. Error Handling
Always implement proper error handling and user feedback:

```typescript
try {
  const agents = await getAgents();
  setAgents(agents);
} catch (error) {
  setError('Failed to load agents. Please try again.');
  console.error('Agent fetch error:', error);
}
```

### 2. Loading States
Provide visual feedback during API calls:

```typescript
const { data: agents, isLoading, error } = useAgents();

if (isLoading) return <Spinner />;
if (error) return <ErrorMessage error={error} />;
return <AgentList agents={agents} />;
```

### 3. Optimistic Updates
For better user experience, implement optimistic updates:

```typescript
const updateAgent = useMutation(updateAgentAPI, {
  onMutate: async (newAgent) => {
    await queryClient.cancelQueries(['agent', newAgent.id]);
    const previousAgent = queryClient.getQueryData(['agent', newAgent.id]);
    queryClient.setQueryData(['agent', newAgent.id], newAgent);
    return { previousAgent };
  },
  onError: (err, newAgent, context) => {
    queryClient.setQueryData(['agent', newAgent.id], context?.previousAgent);
  },
});
```

### 4. Real-time Updates
Use WebSocket connections for live data:

```typescript
useEffect(() => {
  const ws = new WebSocket(`${wsUrl}/websocket`);
  
  ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    if (data.type === 'agent_update') {
      queryClient.setQueryData(['agent', data.agent_id], data.agent);
    }
  };
  
  return () => ws.close();
}, []);
```

## Development Setup

### Environment Configuration
Create a `.env.local` file in your project root:

```bash
# AgentAPI Configuration
NEXT_PUBLIC_AGENTAPI_URL=http://localhost:8080/api/v1
AGENTAPI_API_KEY=your_development_api_key

# Optional: WebSocket URL
NEXT_PUBLIC_AGENTAPI_WS_URL=ws://localhost:8080/api/v1

# Development settings
NODE_ENV=development
```

### API Client Configuration
```typescript
// lib/agentapi.ts
export const agentAPI = {
  baseURL: process.env.NEXT_PUBLIC_AGENTAPI_URL || 'http://localhost:8080/api/v1',
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${process.env.AGENTAPI_API_KEY}`,
  },
};
```

## Troubleshooting

### Common Issues

1. **Connection Refused**: Ensure AgentAPI server is running
2. **Authentication Failed**: Verify API key is correct and not expired
3. **CORS Errors**: Configure CORS settings in AgentAPI server
4. **Rate Limiting**: Implement exponential backoff for retries

### Debug Mode
Enable debug logging to troubleshoot issues:

```typescript
const apiClient = new AgentAPIClient({
  baseURL: agentAPI.baseURL,
  debug: process.env.NODE_ENV === 'development',
});
```

## Support

- **Documentation**: [AgentAPI Repository](https://github.com/coder/agentapi)
- **OpenAPI Spec**: Available at `/api/docs` endpoint
- **Issues**: Report issues on the GitHub repository

---

*This documentation is generated based on the UI structure and common API patterns. Please refer to the [official OpenAPI specification](https://github.com/coder/agentapi/blob/main/openapi.json) for the most accurate and up-to-date API details.*