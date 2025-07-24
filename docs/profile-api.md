# Profile API Documentation

## Overview

The Profile API allows users to manage their persistent profiles with customizable settings and preferences. Profiles support both filesystem and S3 storage backends.

## Configuration

Add profile configuration to your `config.yaml`:

```yaml
profile:
  type: filesystem  # or "s3"
  # Filesystem configuration
  base_path: ~/.agentapi-proxy/profiles
  
  # S3 configuration (when type: s3)
  s3_bucket: my-profiles-bucket
  s3_region: us-east-1
  s3_endpoint: ""  # Optional, for S3-compatible services
  s3_prefix: profiles  # Optional prefix for keys
```

## Environment Variables

- `AGENTAPI_PROFILE_TYPE` - Storage type (filesystem or s3)
- `AGENTAPI_PROFILE_BASE_PATH` - Base path for filesystem storage
- `AGENTAPI_PROFILE_S3_BUCKET` - S3 bucket name
- `AGENTAPI_PROFILE_S3_REGION` - S3 region
- `AGENTAPI_PROFILE_S3_ENDPOINT` - S3 endpoint (optional)
- `AGENTAPI_PROFILE_S3_PREFIX` - S3 key prefix (optional)

## Authentication & Permissions

All profile endpoints require authentication. The following permissions are used:
- `profile:read` - Read profile data
- `profile:write` - Create/update profile data  
- `profile:delete` - Delete profiles
- `profile:admin` - Administrative access to all profiles

## Profile Data Structure

```json
{
  "user_id": "user123",
  "username": "johndoe",
  "email": "john@example.com", 
  "display_name": "John Doe",
  "preferences": {
    "theme": "dark",
    "language": "en",
    "notifications": true
  },
  "settings": {
    "timeout": 30,
    "max_sessions": 5
  },
  "metadata": {
    "organization": "acme-corp",
    "role": "developer"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T14:20:00Z",
  "last_login_at": "2024-01-15T14:20:00Z"
}
```

## API Endpoints

### User Profile Management

#### Get Current User Profile

```http
GET /profile
Authorization: Bearer <token>
```

**Response:**
```json
{
  "user_id": "user123",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "preferences": {},
  "settings": {},
  "metadata": {},
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T14:20:00Z",
  "last_login_at": "2024-01-15T14:20:00Z"
}
```

**Status Codes:**
- `200` - Profile retrieved successfully
- `401` - Authentication required
- `404` - Profile not found

#### Create Profile

```http
POST /profile
Authorization: Bearer <token>
Content-Type: application/json

{
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe"
}
```

**Response:**
```json
{
  "user_id": "user123",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "preferences": {},
  "settings": {},
  "metadata": {},
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:00Z"
}
```

**Status Codes:**
- `201` - Profile created successfully
- `400` - Invalid request body
- `401` - Authentication required
- `409` - Profile already exists

#### Update Profile

```http
PUT /profile
Authorization: Bearer <token>
Content-Type: application/json

{
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "preferences": {
    "theme": "dark",
    "language": "en"
  },
  "settings": {
    "timeout": 30
  },
  "metadata": {
    "role": "developer"
  }
}
```

**Response:**
```json
{
  "user_id": "user123",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "preferences": {
    "theme": "dark",
    "language": "en"
  },
  "settings": {
    "timeout": 30
  },
  "metadata": {
    "role": "developer"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T14:20:00Z"
}
```

**Status Codes:**
- `200` - Profile updated successfully
- `400` - Invalid request body
- `401` - Authentication required
- `404` - Profile not found

#### Delete Profile

```http
DELETE /profile
Authorization: Bearer <token>
```

**Status Codes:**
- `204` - Profile deleted successfully
- `401` - Authentication required
- `404` - Profile not found

### Preference Management

#### Set Preference

```http
POST /profile/preference
Authorization: Bearer <token>
Content-Type: application/json

{
  "key": "theme",
  "value": "dark"
}
```

**Response:**
```json
{
  "message": "Preference set successfully",
  "key": "theme",
  "value": "dark"
}
```

**Status Codes:**
- `200` - Preference set successfully
- `400` - Invalid request body or missing key
- `401` - Authentication required
- `404` - Profile not found

#### Get Preference

```http
GET /profile/preference/{key}
Authorization: Bearer <token>
```

**Response:**
```json
{
  "key": "theme",
  "value": "dark"
}
```

**Status Codes:**
- `200` - Preference retrieved successfully
- `401` - Authentication required
- `404` - Profile or preference not found

### Setting Management

#### Set Setting

```http
POST /profile/setting
Authorization: Bearer <token>
Content-Type: application/json

{
  "key": "timeout",
  "value": 30
}
```

**Response:**
```json
{
  "message": "Setting set successfully",
  "key": "timeout",
  "value": 30
}
```

**Status Codes:**
- `200` - Setting set successfully
- `400` - Invalid request body or missing key
- `401` - Authentication required
- `404` - Profile not found

#### Get Setting

```http
GET /profile/setting/{key}
Authorization: Bearer <token>
```

**Response:**
```json
{
  "key": "timeout",
  "value": 30
}
```

**Status Codes:**
- `200` - Setting retrieved successfully
- `401` - Authentication required
- `404` - Profile or setting not found

### Administrative Endpoints

#### List All Profiles (Admin Only)

```http
GET /profiles
Authorization: Bearer <admin-token>
```

**Response:**
```json
{
  "profiles": ["user123", "user456", "user789"],
  "count": 3
}
```

**Status Codes:**
- `200` - Profiles listed successfully
- `401` - Authentication required
- `403` - Insufficient permissions

#### Get User Profile (Admin Only)

```http
GET /profiles/{userID}
Authorization: Bearer <admin-token>
```

**Response:**
```json
{
  "user_id": "user123",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "preferences": {},
  "settings": {},
  "metadata": {},
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T14:20:00Z",
  "last_login_at": "2024-01-15T14:20:00Z"
}
```

**Status Codes:**
- `200` - Profile retrieved successfully
- `401` - Authentication required
- `403` - Insufficient permissions
- `404` - Profile not found

#### Delete User Profile (Admin Only)

```http
DELETE /profiles/{userID}
Authorization: Bearer <admin-token>
```

**Status Codes:**
- `204` - Profile deleted successfully
- `401` - Authentication required
- `403` - Insufficient permissions
- `404` - Profile not found

## Storage Options

### Filesystem Storage

Profiles are stored as JSON files in user-specific directories:
- Path: `{base_path}/{userID}/profile.json`
- Default base path: `~/.agentapi-proxy/profiles`
- Uses atomic writes for data safety

### S3 Storage

Profiles are stored as JSON objects in S3:
- Key format: `{prefix}/{userID}/profile.json`
- Supports S3-compatible services via custom endpoint
- Automatic encryption at rest (when S3 bucket configured)

## Error Handling

All endpoints return consistent error responses:

```json
{
  "message": "Error description",
  "status": 400
}
```

Common error responses:
- `400 Bad Request` - Invalid request format or missing required fields
- `401 Unauthorized` - Authentication required or invalid credentials
- `403 Forbidden` - Insufficient permissions for the requested operation
- `404 Not Found` - Profile or resource not found
- `409 Conflict` - Profile already exists (on creation)
- `500 Internal Server Error` - Server-side error

## Examples

### Complete Profile Management Flow

1. **Create a profile:**
```bash
curl -X POST /profile \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"username": "johndoe", "email": "john@example.com", "display_name": "John Doe"}'
```

2. **Set preferences:**
```bash
curl -X POST /profile/preference \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"key": "theme", "value": "dark"}'
```

3. **Update profile:**
```bash
curl -X PUT /profile \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"display_name": "John Smith", "preferences": {"theme": "light"}}'
```

4. **Get profile:**
```bash
curl -X GET /profile \
  -H "Authorization: Bearer <token>"
```

### Configuration Examples

#### Filesystem Configuration
```yaml
profile:
  type: filesystem
  base_path: /app/data/profiles
```

#### S3 Configuration  
```yaml
profile:
  type: s3
  s3_bucket: agentapi-profiles
  s3_region: us-west-2
  s3_prefix: prod/profiles
```

#### MinIO Configuration
```yaml
profile:
  type: s3
  s3_bucket: profiles
  s3_region: us-east-1
  s3_endpoint: http://minio:9000
  s3_prefix: profiles
```