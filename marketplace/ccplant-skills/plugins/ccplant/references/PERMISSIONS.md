# Permissions and Role-Based Access Control (RBAC)

## Overview

agentapi-proxy implements a role-based access control (RBAC) system with granular permissions for different operations.

## Roles

### admin

Full system access with all permissions.

**Permissions:** `["*"]` (wildcard - grants all permissions)

**Capabilities:**
- Create, list, delete, and access all sessions (including other users' sessions)
- Manage settings and notifications
- Access all API endpoints
- View and manage all resources system-wide

**Example Configuration:**
```json
{
  "key": "ap_admin_key_123456789abcdef",
  "user_id": "admin",
  "role": "admin",
  "permissions": ["*"]
}
```

### user

Standard user with permissions for normal development workflows.

**Permissions:**
```json
[
  "session:create",
  "session:list",
  "session:delete",
  "session:access",
  "session:read"
]
```

**Capabilities:**
- Create new sessions
- List and search own sessions
- Delete own sessions
- Access own session instances
- Read user info and settings
- Manage personal notifications

**Restrictions:**
- Cannot access other users' sessions
- Cannot view other users' data

**Example Configuration:**
```json
{
  "key": "ap_user_alice_987654321fedcba",
  "user_id": "alice",
  "role": "user",
  "permissions": [
    "session:create",
    "session:list",
    "session:delete",
    "session:access",
    "session:read"
  ]
}
```

### readonly

Limited access for viewing session information only.

**Permissions:** `["session:list"]`

**Capabilities:**
- List and search own sessions
- View session metadata

**Restrictions:**
- Cannot create sessions
- Cannot delete sessions
- Cannot access session instances
- Cannot modify any data

**Example Configuration:**
```json
{
  "key": "ap_readonly_charlie_aabbccddeeff",
  "user_id": "charlie",
  "role": "readonly",
  "permissions": ["session:list"]
}
```

## Permission Types

### session:create

Create new agentapi sessions.

**Required for:**
- `POST /start` - Create session
- `PUT /settings/:name` - Update settings
- `DELETE /settings/:name` - Delete settings

**Example:**
```bash
curl -X POST https://api.example.com/start \
  -H "X-API-Key: ap_user_alice_987654321fedcba" \
  -d '{"environment": {...}}'
```

### session:list

List and search sessions.

**Required for:**
- `GET /search` - Search sessions

**Access Control:**
- Non-admin users see only their own sessions
- Admin users see all sessions

**Example:**
```bash
curl -H "X-API-Key: ap_user_alice_987654321fedcba" \
  https://api.example.com/search
```

### session:delete

Delete sessions.

**Required for:**
- `DELETE /sessions/:sessionId` - Delete session

**Access Control:**
- Non-admin users can only delete their own sessions
- Admin users can delete any session

**Example:**
```bash
curl -X DELETE https://api.example.com/sessions/SESSION_ID \
  -H "X-API-Key: ap_user_alice_987654321fedcba"
```

### session:access

Access and interact with session instances.

**Required for:**
- `ANY /:sessionId/*` - Proxy to session
- `POST /sessions/:sessionId/share` - Create share token
- `GET /sessions/:sessionId/share` - Get share token
- `DELETE /sessions/:sessionId/share` - Revoke share token

**Access Control:**
- Non-admin users can only access their own sessions
- Admin users can access any session

**Example:**
```bash
curl -X POST https://api.example.com/SESSION_ID/message \
  -H "X-API-Key: ap_user_alice_987654321fedcba" \
  -d '{"content": "Hello", "type": "user"}'
```

### session:read

Read user information, settings, and notifications.

**Required for:**
- `GET /user/info` - Get user info
- `GET /settings/:name` - Get setting
- `GET /users/me/api-key` - Get personal API key
- `POST /users/me/api-key` - Create personal API key
- `POST /notification/subscribe` - Subscribe to notifications
- `GET /notification/subscribe` - Get subscription info
- `DELETE /notification/subscribe` - Unsubscribe

**Example:**
```bash
curl -H "X-API-Key: ap_user_alice_987654321fedcba" \
  https://api.example.com/user/info
```

### * (Wildcard)

All permissions. Used for admin roles.

**Grants access to:** All endpoints and operations.

## Session Ownership

Session ownership is enforced for non-admin users:

### Owner Validation

For session-specific operations, the system checks:

1. **Admin users**: Can access any session
2. **Non-admin users**: Can only access sessions where `session.user_id == authenticated_user.user_id`

### Affected Endpoints

Ownership is checked for:
- `DELETE /sessions/:sessionId`
- `ANY /:sessionId/*`
- `POST /sessions/:sessionId/share`
- `GET /sessions/:sessionId/share`
- `DELETE /sessions/:sessionId/share`

### Shared Sessions

Shared sessions (accessed via `/s/:shareToken/*`) are read-only and do not require authentication or ownership.

## Permission Enforcement

### Middleware Flow

```
Request → Authentication → Permission Check → Ownership Check → Handler
```

1. **Authentication**: Validate API key, extract user context
2. **Permission Check**: Verify user has required permission for endpoint
3. **Ownership Check**: For session operations, verify user owns the session (unless admin)
4. **Handler**: Execute the requested operation

### Endpoint Permissions Matrix

| Endpoint | Required Permission | Ownership Check |
|----------|---------------------|-----------------|
| `POST /start` | `session:create` | N/A |
| `GET /search` | `session:list` | Filter results |
| `DELETE /sessions/:id` | `session:delete` | Yes |
| `ANY /:sessionId/*` | `session:access` | Yes |
| `POST /sessions/:id/share` | `session:access` | Yes |
| `GET /sessions/:id/share` | `session:access` | Yes |
| `DELETE /sessions/:id/share` | `session:access` | Yes |
| `ANY /s/:shareToken/*` | None | No |
| `GET /user/info` | `session:read` | N/A |
| `GET /settings/:name` | `session:read` | N/A |
| `PUT /settings/:name` | `session:create` | N/A |
| `DELETE /settings/:name` | `session:create` | N/A |
| `GET /users/me/api-key` | `session:read` | N/A |
| `POST /users/me/api-key` | `session:read` | N/A |
| `POST /notification/subscribe` | `session:read` | N/A |
| `GET /notification/subscribe` | `session:read` | N/A |
| `DELETE /notification/subscribe` | `session:read` | N/A |
| `GET /health` | None | No |
| `GET /auth/status` | Authenticated | No |

## Custom Roles

You can define custom roles with specific permission combinations:

```json
{
  "key": "ap_custom_developer_xyz",
  "user_id": "developer",
  "role": "developer",
  "permissions": [
    "session:create",
    "session:list",
    "session:access"
  ]
}
```

This example creates a "developer" role that can create and access sessions but not delete them.

## Best Practices

1. **Principle of Least Privilege**: Grant only the permissions needed for the task
2. **Use predefined roles**: Stick to admin/user/readonly unless you need custom permissions
3. **Separate concerns**: Use different API keys for different purposes
4. **Review permissions regularly**: Audit API keys and their permissions periodically
5. **Rotate high-privilege keys**: Especially admin keys, rotate frequently
6. **Monitor access logs**: Track usage of high-privilege operations

## Error Responses

### 403 Insufficient Permissions

Returned when the API key lacks required permissions:

```json
{
  "error": "Insufficient permissions"
}
```

**Resolution:** Update the API key configuration to include the required permission.

### 403 Access Denied

Returned when a non-admin user attempts to access another user's session:

```json
{
  "error": "Access denied"
}
```

**Resolution:** Ensure you're accessing your own sessions, or use an admin API key.

## Permission Validation

The permission validation logic:

```go
// Wildcard permission grants all access
if hasPermission(user, "*") {
    return true
}

// Check for specific permission
if hasPermission(user, requiredPermission) {
    return true
}

// Check session ownership for non-admin users
if isSessionEndpoint && !isAdmin(user) {
    if session.UserID != user.ID {
        return false // Access denied
    }
}

return false
```

## Implementation Reference

For implementation details, see the agentapi-proxy source code:

- `/pkg/auth/middleware.go` - Authentication and authorization middleware
- `/pkg/auth/authz_context.go` - Authorization context handling
- `/internal/infrastructure/services/simple_auth_service.go` - API key validation
