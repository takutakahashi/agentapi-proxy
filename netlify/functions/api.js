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
  '/start-with-profile': {
    POST: {
      session_id: "550e8400-e29b-41d4-a716-446655440001",
      message: "Session created successfully with profile",
      profile_id: "profile-uuid-here",
      port: 9001,
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
  '/profiles': {
    GET: {
      profiles: [
        {
          id: "profile-550e8400-e29b-41d4-a716-446655440000",
          user_id: "user-123",
          name: "開発環境プロファイル",
          description: "開発作業用の設定",
          environment: {
            NODE_ENV: "development",
            DEBUG: "true"
          },
          repository_history: [],
          system_prompt: "開発者アシスタント",
          message_templates: [],
          created_at: "2024-01-01T12:00:00Z",
          updated_at: "2024-01-01T12:00:00Z"
        }
      ],
      total: 1
    },
    POST: {
      profile: {
        id: "profile-550e8400-e29b-41d4-a716-446655440000",
        user_id: "user-123",
        name: "開発環境プロファイル",
        description: "開発作業用の設定",
        environment: {
          NODE_ENV: "development",
          DEBUG: "true",
          API_BASE_URL: "https://dev-api.example.com"
        },
        repository_history: [],
        system_prompt: "あなたは開発者のアシスタントです。コードレビューやデバッグを支援してください。",
        message_templates: [],
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      }
    }
  }
};

// Handle profile-specific endpoints
const handleProfileEndpoint = (path, method) => {
  const profileIdMatch = path.match(/^\/profiles\/([^\/]+)$/);
  const repoMatch = path.match(/^\/profiles\/([^\/]+)\/repositories$/);
  const templateMatch = path.match(/^\/profiles\/([^\/]+)\/templates$/);

  if (profileIdMatch) {
    const profileId = profileIdMatch[1];
    switch (method) {
      case 'GET':
        return {
          profile: {
            id: profileId,
            user_id: "user-123",
            name: "開発環境プロファイル",
            description: "開発作業用の設定",
            environment: {
              NODE_ENV: "development",
              DEBUG: "true",
              API_BASE_URL: "https://dev-api.example.com"
            },
            repository_history: [
              {
                id: "repo-550e8400-e29b-41d4-a716-446655440000",
                url: "https://github.com/example/project",
                name: "example-project",
                branch: "main",
                last_commit: "abc123def456",
                accessed_at: "2024-01-01T12:00:00Z"
              }
            ],
            system_prompt: "開発者アシスタント",
            message_templates: [],
            created_at: "2024-01-01T12:00:00Z",
            updated_at: "2024-01-01T12:00:00Z",
            last_used_at: "2024-01-01T13:00:00Z"
          }
        };
      case 'PUT':
        return {
          profile: {
            id: profileId,
            user_id: "user-123",
            name: "更新された開発環境プロファイル",
            description: "開発作業用の設定（更新済み）",
            environment: {
              NODE_ENV: "development",
              DEBUG: "true",
              NEW_VAR: "new_value"
            },
            repository_history: [],
            system_prompt: "更新されたシステムプロンプト",
            message_templates: [],
            created_at: "2024-01-01T12:00:00Z",
            updated_at: new Date().toISOString()
          }
        };
      case 'DELETE':
        return {
          message: "Profile deleted successfully",
          profile_id: profileId,
          deleted_at: new Date().toISOString()
        };
      default:
        return null;
    }
  }

  if (repoMatch) {
    return {
      message: "Repository added to profile successfully",
      repository: {
        id: "repo-550e8400-e29b-41d4-a716-446655440002",
        url: "https://github.com/example/new-project",
        name: "new-project",
        branch: "develop",
        last_commit: "def456abc789",
        added_at: new Date().toISOString()
      }
    };
  }

  if (templateMatch) {
    return {
      message: "Template added to profile successfully",
      template: {
        id: "template-550e8400-e29b-41d4-a716-446655440003",
        name: "バグ報告テンプレート",
        content: "## バグ報告\\n\\n**発生環境**: {{environment}}",
        variables: ["environment"],
        category: "bug-report",
        created_at: new Date().toISOString()
      }
    };
  }

  return null;
};

// Handle session-specific endpoints
const handleSessionEndpoint = (path, method) => {
  const sessionMatch = path.match(/^\/sessions?\/([^\/]+)/);
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
    responseData = handleProfileEndpoint(path, method) || handleSessionEndpoint(path, method);
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