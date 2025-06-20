# Session Persistence

This document describes the session persistence feature that allows agentapi-proxy to maintain session state across server restarts.

## Overview

By default, all session data is stored only in memory, which means all sessions are lost when the proxy server restarts. The session persistence feature provides an optional storage layer to maintain session state across restarts, improving reliability and user experience.

## Configuration

Session persistence is configured in the main configuration file under the `persistence` section:

```json
{
  "persistence": {
    "enabled": true,
    "backend": "file",
    "file_path": "./sessions.json",
    "sync_interval_seconds": 30,
    "encrypt_sensitive_data": true
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable or disable session persistence |
| `backend` | string | `"file"` | Storage backend type (`"file"`, `"memory"`) |
| `file_path` | string | `"./sessions.json"` | Path to the session storage file (file backend only) |
| `sync_interval_seconds` | integer | `30` | Interval for periodic sync to storage in seconds |
| `encrypt_sensitive_data` | boolean | `true` | Encrypt sensitive environment variables |

## Storage Backends

### Memory Storage
- **Type**: `"memory"`
- **Description**: In-memory storage (default behavior when persistence is disabled)
- **Use Case**: Development, testing, or when persistence is not needed

### File Storage
- **Type**: `"file"`
- **Description**: JSON file-based storage with optional encryption
- **Features**:
  - Atomic writes using temporary files
  - Periodic synchronization
  - Sensitive data encryption
  - Automatic directory creation

## Data Storage Format

### Session Data Structure

The following session data is persisted:

```json
{
  "sessions": [
    {
      "id": "uuid-123",
      "port": 9001,
      "started_at": "2024-01-01T12:00:00Z",
      "user_id": "alice",
      "status": "active",
      "environment": {
        "VAR1": "value1",
        "GITHUB_TOKEN": "ENC:encrypted_token_data"
      },
      "tags": {
        "environment": "production",
        "version": "1.0"
      },
      "process_id": 12345,
      "command": ["agentapi", "--port", "9001"],
      "working_dir": "/tmp"
    }
  ],
  "updated_at": "2024-01-01T12:30:00Z"
}
```

### Sensitive Data Encryption

When `encrypt_sensitive_data` is enabled, environment variables containing sensitive patterns are automatically encrypted:

- `TOKEN`
- `KEY` 
- `SECRET`
- `PASSWORD`
- `CREDENTIAL`

Encrypted values are prefixed with `"ENC:"` and are automatically decrypted when loaded.

## Session Recovery

### Startup Process

1. **Load Sessions**: On startup, the proxy loads all persisted sessions
2. **Validation**: Each session is validated for:
   - Process still running (if process_id exists)
   - Port availability
   - Session age (sessions older than 24 hours are cleaned up)
3. **Recovery**: Valid sessions are restored to memory with status `"recovered"`
4. **Cleanup**: Invalid or stale sessions are removed from storage

### Recovery Validation

The recovery process performs the following checks:

- **Process Validation**: Check if the original process is still running
- **Port Validation**: Ensure the port is available or still in use by the session
- **Age Validation**: Remove sessions older than 24 hours
- **Resource Validation**: Verify required resources are available

## Error Handling

### Storage Errors

- Storage initialization failures fall back to memory storage
- Individual operation failures are logged but don't crash the server
- Periodic sync failures are logged and retried on the next interval

### Recovery Errors

- Invalid sessions during recovery are cleaned up automatically
- Recovery failures are logged but don't prevent server startup
- Partial recovery is acceptable (some sessions recovered, others cleaned up)

## Security Considerations

### File Permissions

- Storage files are created with restrictive permissions (0700 for directories)
- Temporary files are cleaned up even if operations fail

### Encryption

- Uses AES encryption for sensitive environment variables
- Encryption key is derived from a fixed string (for simplicity)
- In production, consider using external key management systems

### Data Exposure

- Only essential session metadata is persisted
- Runtime objects like `Process` and `Cancel` are not stored
- Sensitive data is encrypted before storage

## Best Practices

### Configuration

1. **Enable for Production**: Always enable persistence in production environments
2. **Secure Storage Location**: Store session files in secure, backed-up locations
3. **Regular Cleanup**: Monitor storage file sizes and implement rotation if needed

### Monitoring

1. **Recovery Metrics**: Monitor session recovery success/failure rates
2. **Storage Health**: Monitor storage operation errors
3. **File Size**: Monitor storage file growth over time

### Backup and Recovery

1. **Backup Storage Files**: Include session storage files in backup procedures
2. **Test Recovery**: Regularly test session recovery procedures
3. **Disaster Recovery**: Have procedures for manual session cleanup if needed

## Troubleshooting

### Common Issues

1. **Storage File Corruption**
   - Check file permissions and disk space
   - Verify JSON format validity
   - Delete corrupted files to start fresh

2. **Recovery Failures**
   - Check logs for specific validation failures
   - Verify port availability
   - Check process permissions

3. **Performance Issues**
   - Adjust sync_interval_seconds for your workload
   - Monitor storage file sizes
   - Consider implementing file rotation

### Debugging

Enable verbose logging to see detailed session recovery information:

```bash
./agentapi-proxy server --verbose
```

Check the storage file contents:

```bash
cat ./sessions.json | jq .
```

## Future Enhancements

### Planned Features

1. **SQLite Backend**: Database storage for better performance and queries
2. **Session Metrics**: Detailed metrics for session lifecycle and recovery
3. **Configuration Validation**: Enhanced validation of persistence settings
4. **External Key Management**: Support for external encryption key sources

### Extension Points

The storage interface is designed to be extensible:

```go
type Storage interface {
    Save(session *SessionData) error
    Load(sessionID string) (*SessionData, error)
    LoadAll() ([]*SessionData, error)
    Delete(sessionID string) error
    Update(session *SessionData) error
    Close() error
}
```

New storage backends can be added by implementing this interface and registering them in the factory function.