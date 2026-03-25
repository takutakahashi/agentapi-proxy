# Authentication Guide

## Overview

agentapi-proxy supports multiple authentication methods:

1. **Static API Keys** - Pre-configured keys with role-based permissions
2. **GitHub Token Authentication** - Using GitHub personal access tokens
3. **GitHub OAuth Flow** - Browser-based OAuth2 authentication
4. **AWS IAM Authentication** - Using AWS Access Key IDs
5. **Hybrid Mode** - Combination of multiple methods

## Static API Keys (Most Common)

### Authentication Headers

agentapi-proxy accepts API keys through multiple header formats:

#### Option 1: Custom Header (Recommended)

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://api.example.com/endpoint
```

The header name defaults to `X-API-Key` but can be configured via `auth.static.header_name`.

#### Option 2: Authorization Bearer

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://api.example.com/endpoint
```

#### Option 3: Authorization Token

```bash
curl -H "Authorization: token YOUR_API_KEY" https://api.example.com/endpoint
```

#### Option 4: Raw Authorization

```bash
curl -H "Authorization: YOUR_API_KEY" https://api.example.com/endpoint
```

### API Key Format

API keys in agentapi-proxy are defined in a JSON configuration file:

```json
{
  "api_keys": [
    {
      "key": "ap_admin_key_123456789abcdef",
      "user_id": "admin",
      "role": "admin",
      "permissions": ["*"],
      "created_at": "2024-06-14T00:00:00Z",
      "expires_at": "2025-06-14T00:00:00Z"
    },
    {
      "key": "ap_user_alice_987654321fedcba",
      "user_id": "alice",
      "role": "user",
      "permissions": [
        "session:create",
        "session:list",
        "session:delete",
        "session:access"
      ],
      "created_at": "2024-06-14T00:00:00Z",
      "expires_at": "2025-06-14T00:00:00Z"
    }
  ]
}
```

### Configuration

Static API key authentication is configured in `config.json`:

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

Or load from an external file:

```json
{
  "auth": {
    "enabled": true,
    "static": {
      "enabled": true,
      "header_name": "X-API-Key",
      "api_keys_file": "/path/to/api_keys.json"
    }
  }
}
```

## GitHub Personal Access Token

Authenticate using GitHub personal access tokens directly:

```json
{
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "token": {
        "enabled": true
      }
    }
  }
}
```

Usage:

```bash
curl -H "Authorization: Bearer ghp_your_github_token" \
  https://api.example.com/start
```

## GitHub OAuth Flow

Full OAuth2 flow for web applications:

```json
{
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "oauth": {
        "client_id": "${GITHUB_CLIENT_ID}",
        "client_secret": "${GITHUB_CLIENT_SECRET}",
        "redirect_url": "https://your-domain.com/oauth/callback",
        "scope": "read:user read:org"
      }
    }
  }
}
```

OAuth endpoints:
- `POST /oauth/authorize` - Start authorization
- `GET /oauth/callback` - Handle callback
- `POST /oauth/logout` - Logout
- `POST /oauth/refresh` - Refresh token

## AWS IAM Authentication

Authenticate using AWS Access Key IDs:

```json
{
  "auth": {
    "enabled": true,
    "aws": {
      "enabled": true
    }
  }
}
```

Usage with Basic auth:

```bash
curl -u "AKIAIOSFODNN7EXAMPLE:anyvalue" \
  https://api.example.com/sessions
```

## Hybrid Authentication

Combine multiple authentication methods:

```json
{
  "auth": {
    "enabled": true,
    "static": {
      "enabled": true,
      "header_name": "X-API-Key"
    },
    "github": {
      "enabled": true,
      "token": {
        "enabled": true
      },
      "oauth": {
        "client_id": "${GITHUB_CLIENT_ID}",
        "client_secret": "${GITHUB_CLIENT_SECRET}"
      }
    },
    "aws": {
      "enabled": true
    }
  }
}
```

## Authentication Flow

The authentication middleware processes requests in this order:

1. **Extract credentials** from headers:
   - Check custom header (e.g., `X-API-Key`)
   - Check `Authorization` header (Bearer/token/raw)
   - Check Basic auth (for AWS)

2. **Validate credentials**:
   - Static API key validation
   - GitHub token validation
   - AWS IAM validation

3. **Check expiration**:
   - Verify API key hasn't expired (RFC3339 format)

4. **Retrieve user context**:
   - Get user ID and permissions
   - Set authorization context

5. **Enforce permissions**:
   - Check required permissions for endpoint
   - Verify session ownership (for non-admin users)

## Key Validation

API keys are validated with these checks:

1. **Existence**: Key must exist in configuration
2. **Expiration**: Current time must be before `expires_at`
3. **User**: Associated user must be valid
4. **Permissions**: User must have required permissions for the endpoint

## Personal API Keys

Users can generate personal API keys through the API:

```bash
# Get or create personal API key
curl -H "Authorization: Bearer ghp_github_token" \
  https://api.example.com/users/me/api-key

# Rotate personal API key
curl -X POST -H "Authorization: Bearer ghp_github_token" \
  https://api.example.com/users/me/api-key
```

Personal keys:
- Valid for 24 hours by default
- Automatically associated with the user
- Can be rotated on demand

## Best Practices

1. **Use X-API-Key header** for simplicity and clarity
2. **Set appropriate expiration dates** to limit exposure
3. **Rotate keys regularly** for security
4. **Use minimal permissions** - grant only what's needed
5. **Store keys securely** - never commit to version control
6. **Use environment variables** for sensitive configuration

## Troubleshooting

### 401 Invalid API key

- Verify the API key is correct
- Check the key exists in configuration
- Ensure you're using the correct header name

### 401 API key expired

- Check the `expires_at` field in configuration
- Generate a new key or update expiration date

### 403 Insufficient permissions

- Verify the key has required permissions for the endpoint
- Check the `permissions` array in the API key configuration

### 403 Access denied

- Ensure you're accessing your own sessions (non-admin users)
- Verify you're the owner of the session you're trying to access
