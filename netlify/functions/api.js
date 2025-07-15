// Netlify Function to handle all API requests
const fs = require('fs').promises;
const path = require('path');

// Mock data definitions
const mockResponses = {
  '/start': {
    POST: {
      session_id: "550e8400-e29b-41d4-a716-446655440000",
      message: "Session created successfully",
      port: 9000,
      started_at: new Date().toISOString(),
      status: "active"
    }
  },
  '/search': {
    GET: {
      sessions: [
        {
          session_id: "550e8400-e29b-41d4-a716-446655440000",
          user_id: "user-123",
          status: "active",
          started_at: "2024-01-01T12:00:00Z",
          port: 9000,
          tags: {
            repository: "agentapi-proxy",
            branch: "main",
            env: "production"
          }
        },
        {
          session_id: "550e8400-e29b-41d4-a716-446655440001",
          user_id: "user-123",
          status: "active",
          started_at: "2024-01-01T12:05:00Z",
          port: 9001,
          tags: {
            repository: "another-repo",
            branch: "develop",
            env: "development"
          }
        }
      ],
      total: 2,
      filters_applied: {
        user_id: null,
        status: null,
        tags: {}
      }
    }
  },
};


// Handle session-specific endpoints
const handleSessionEndpoint = (path, method) => {
  const sessionMatch = path.match(/^\/sessions?\/([^\/]+)/);
  const messagesMatch = path.match(/^\/([^\/]+)\/messages$/);
  
  if (messagesMatch && method === 'GET') {
    const sessionId = messagesMatch[1];
    return {
      session_id: sessionId,
      messages: [
        {
          id: "msg-550e8400-e29b-41d4-a716-446655440000",
          session_id: sessionId,
          role: "user",
          content: "こんにちは、今日のタスクについて相談したいです。",
          timestamp: "2024-01-01T12:00:00Z",
          metadata: {
            type: "text",
            source: "web"
          }
        },
        {
          id: "msg-550e8400-e29b-41d4-a716-446655440001",
          session_id: sessionId,
          role: "assistant",
          content: "こんにちは！喜んでお手伝いします。どのようなタスクについてご相談でしょうか？",
          timestamp: "2024-01-01T12:00:30Z",
          metadata: {
            type: "text",
            model: "claude-3.5-sonnet"
          }
        },
        {
          id: "msg-550e8400-e29b-41d4-a716-446655440002",
          session_id: sessionId,
          role: "user",
          content: "プロジェクトのコードレビューをお願いします。",
          timestamp: "2024-01-01T12:01:00Z",
          metadata: {
            type: "text",
            source: "web"
          }
        },
        {
          id: "msg-550e8400-e29b-41d4-a716-446655440003",
          session_id: sessionId,
          role: "assistant",
          content: "承知しました。コードレビューを実施いたします。対象のファイルを共有していただけますか？",
          timestamp: "2024-01-01T12:01:15Z",
          metadata: {
            type: "text",
            model: "claude-3.5-sonnet",
            tools_used: ["code_analysis"]
          }
        },
        {
          id: "msg-550e8400-e29b-41d4-a716-446655440004",
          session_id: sessionId,
          role: "system",
          content: "Error: Invalid API key provided. Please check your authentication credentials.",
          timestamp: "2024-01-01T12:01:30Z",
          metadata: {
            type: "error",
            error_code: "INVALID_API_KEY"
          }
        }
      ],
      total: 5,
      page: 1,
      per_page: 50,
      has_more: false
    };
  }
  
  if (sessionMatch) {
    const sessionId = sessionMatch[1];
    return {
      session_id: sessionId,
      status: "active",
      agentapi_url: `http://localhost:9000`,
      message: "This endpoint would normally proxy to the actual agentapi server",
      note: "In a real implementation, this would forward requests to the agentapi server running on the specified port"
    };
  }
  return null;
};

exports.handler = async (event, context) => {
  const path = event.path.replace('/.netlify/functions/api', '');
  const method = event.httpMethod;

  // CORS headers
  const headers = {
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Headers': 'Content-Type, Authorization, X-API-Key',
    'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
    'Content-Type': 'application/json'
  };

  // Handle OPTIONS requests
  if (method === 'OPTIONS') {
    return {
      statusCode: 200,
      headers,
      body: ''
    };
  }

  // Log request for debugging
  console.log(`${method} ${path}`);

  // Check basic endpoints first
  let responseData = mockResponses[path]?.[method];

  // If not found, check dynamic endpoints
  if (!responseData) {
    responseData = handleSessionEndpoint(path, method);
  }

  // If still not found, return 404
  if (!responseData) {
    return {
      statusCode: 404,
      headers,
      body: JSON.stringify({
        error: "Not Found",
        code: "ENDPOINT_NOT_FOUND",
        message: `The endpoint ${path} was not found`,
        path: path,
        method: method
      })
    };
  }

  // Handle POST/PUT requests with body
  if ((method === 'POST' || method === 'PUT') && event.body) {
    try {
      const requestBody = JSON.parse(event.body);
      // For session creation, include user data
      if (path === '/start' && requestBody.user_id) {
        responseData.user_id = requestBody.user_id;
        responseData.environment = requestBody.environment || {};
        responseData.tags = requestBody.tags || {};
      }
    } catch (e) {
      // Ignore parse errors
    }
  }

  return {
    statusCode: method === 'POST' ? 201 : 200,
    headers,
    body: JSON.stringify(responseData, null, 2)
  };
};