# AWS IAM Authentication

agentapi-proxy supports authentication using AWS IAM credentials (Access Key ID and Secret Access Key) via HTTP Basic Authentication.

## Overview

AWS IAM authentication allows users to authenticate using their AWS Access Key ID. The system:

1. Accepts AWS Access Key ID as the Basic Auth username
2. Accepts AWS Secret Access Key as the Basic Auth password (not used for verification)
3. Uses **proxy's IAM permissions** to lookup the user by Access Key ID
4. Verifies the IAM user/role exists and has the required tag
5. Retrieves IAM tags to determine team membership
6. Maps teams to roles and permissions

**Note**: The proxy verifies users using its own IAM permissions, not the client's credentials. This provides centralized access control.

## Authentication Flow

```
Client                    agentapi-proxy                    AWS IAM
   |                            |                              |
   | Basic Auth                 |                              |
   | (AccessKeyID:Secret)       |                              |
   |--------------------------->|                              |
   |                            |                              |
   |                            | GetAccessKeyLastUsed         |
   |                            | (find user by AccessKeyID)   |
   |                            |----------------------------->|
   |                            |                              |
   |                            | GetUser + ListUserTags       |
   |                            | (verify user & check tags)   |
   |                            |----------------------------->|
   |                            |                              |
   |                            | <-- User info + tags         |
   |                            |<-----------------------------|
   |                            |                              |
   |                            | Check required tag           |
   |                            | Map teams -> permissions     |
   |                            |                              |
   | <-- Auth success           |                              |
   |<---------------------------|                              |
```

## Configuration

### Basic Configuration

```json
{
  "auth": {
    "enabled": true,
    "aws": {
      "enabled": true,
      "region": "ap-northeast-1",
      "account_id": "123456789012",
      "team_tag_key": "Team",
      "required_tag_key": "agentapi-proxy",
      "required_tag_value": "enabled",
      "cache_ttl": "1h",
      "user_mapping": {
        "default_role": "guest",
        "default_permissions": ["read"],
        "team_role_mapping": {
          "platform": {
            "role": "admin",
            "permissions": ["*"],
            "env_file": "/etc/agentapi/envs/platform.env"
          },
          "backend": {
            "role": "developer",
            "permissions": ["read", "write", "execute"]
          }
        }
      }
    }
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable AWS authentication |
| `region` | string | `ap-northeast-1` | AWS region for IAM API calls |
| `account_id` | string | (optional) | Restrict to specific AWS account ID |
| `team_tag_key` | string | `Team` | IAM tag key used to identify team membership |
| `required_tag_key` | string | (optional) | Tag key that must exist on the user (e.g., "agentapi-proxy") |
| `required_tag_value` | string | (optional) | Expected value for the required tag (e.g., "enabled") |
| `cache_ttl` | string | `1h` | Duration to cache user information |
| `user_mapping.default_role` | string | `guest` | Default role for users without team mappings |
| `user_mapping.default_permissions` | []string | `[]` | Default permissions for all users |
| `user_mapping.team_role_mapping` | map | `{}` | Team to role/permission mappings |

### Environment Variables

```bash
export AGENTAPI_AUTH_ENABLED=true
export AGENTAPI_AUTH_AWS_ENABLED=true
export AGENTAPI_AUTH_AWS_REGION=ap-northeast-1
export AGENTAPI_AUTH_AWS_ACCOUNT_ID=123456789012
export AGENTAPI_AUTH_AWS_TEAM_TAG_KEY=Team
export AGENTAPI_AUTH_AWS_REQUIRED_TAG_KEY=agentapi-proxy
export AGENTAPI_AUTH_AWS_REQUIRED_TAG_VALUE=enabled
export AGENTAPI_AUTH_AWS_CACHE_TTL=1h
```

## Client Usage

### curl

```bash
curl -u "AKIAIOSFODNN7EXAMPLE:anyvalue" \
  https://api.example.com/sessions
```

**Note**: The secret access key is accepted but not verified. Authentication is done by looking up the access key ID using the proxy's IAM permissions.

### Go

```go
import (
    "net/http"
    "os"
)

func callAPI(endpoint string) (*http.Response, error) {
    req, _ := http.NewRequest("GET", endpoint, nil)
    req.SetBasicAuth(
        os.Getenv("AWS_ACCESS_KEY_ID"),
        os.Getenv("AWS_SECRET_ACCESS_KEY"), // Sent but not verified
    )
    return http.DefaultClient.Do(req)
}
```

### Python

```python
import requests
import os

response = requests.get(
    "https://api.example.com/sessions",
    auth=(
        os.environ["AWS_ACCESS_KEY_ID"],
        os.environ["AWS_SECRET_ACCESS_KEY"]  # Sent but not verified
    )
)
```

## IAM Tag Configuration

### Required Tag for Access Control

To enable access control, set `required_tag_key` and optionally `required_tag_value`. Only users with this tag can authenticate:

```bash
# Allow users with tag "agentapi-proxy=enabled"
aws iam tag-user --user-name johndoe \
  --tags Key=agentapi-proxy,Value=enabled
```

### Team Tags

Users are mapped to teams based on IAM tags:

```bash
# Single team
aws iam tag-user --user-name johndoe --tags Key=Team,Value=backend

# Multiple teams (comma-separated)
aws iam tag-user --user-name janedoe --tags Key=Team,Value="platform,backend"
```

### Complete Tag Setup Example

```bash
# Enable access and assign to backend team
aws iam tag-user --user-name johndoe \
  --tags \
    Key=agentapi-proxy,Value=enabled \
    Key=Team,Value=backend
```

## Required IAM Permissions

### For agentapi-proxy Service

The service running agentapi-proxy needs the following IAM permissions:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "iam:GetAccessKeyLastUsed",
                "iam:GetUser",
                "iam:ListUserTags"
            ],
            "Resource": "arn:aws:iam::*:user/*"
        }
    ]
}
```

### For Client Users

Client users only need their Access Key ID. They do not need any specific IAM permissions for authentication.

## Security Considerations

1. **Proxy-Side Verification**: The proxy verifies users using its own IAM permissions, not the client's secret key
2. **Required Tag**: Use `required_tag_key` to restrict access to authorized users only
3. **Account Restriction**: Configure `account_id` to restrict access to your AWS account
4. **Tag Management**: Control who can modify IAM tags to prevent unauthorized access
5. **HTTPS Required**: Always use HTTPS to protect the Access Key ID in transit
6. **Credential Rotation**: Regularly rotate AWS access keys

## Role Priority

When a user belongs to multiple teams, the highest priority role is applied:

1. `admin` (priority: 4)
2. `developer` (priority: 3)
3. `member` (priority: 2)
4. `user` (priority: 1)
5. `guest` (priority: 0)

All permissions from matching teams are merged.

## Caching

User information is cached to reduce IAM API calls. The cache key is derived from the Access Key ID hash. Configure `cache_ttl` to control cache duration.

## Troubleshooting

### Authentication Failed: Access Key Not Found

1. Verify the Access Key ID is correct and exists
2. Check that the proxy has `iam:GetAccessKeyLastUsed` permission
3. Verify the access key belongs to an IAM user (not root account)

### Authentication Failed: Required Tag Missing

1. Verify the user has the required tag set
2. Check tag key and value match the configuration exactly (case-sensitive)
3. Use `aws iam list-user-tags --user-name USERNAME` to verify tags

### Authentication Failed: Account Not Allowed

1. Verify the user belongs to the configured `account_id`
2. Check the ARN in proxy logs to confirm the account ID
