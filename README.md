# agentapi-proxy

Proxy and Process Provisioner for coder/agentapi

A configurable HTTP proxy server that routes requests based on URL patterns to different backend services. Designed specifically for proxying agentapi requests to appropriate backend services.

## Features

- **Flexible Routing**: Route requests based on URL patterns like `/api/{org}/{repo}`
- **Configurable Backends**: Map different routes to different backend services
- **Default Fallback**: Configure a default backend for unmatched routes
- **Header Forwarding**: Automatically forwards `X-Forwarded-Host` and `X-Forwarded-Proto` headers
- **Comprehensive Logging**: Optional verbose logging for debugging
- **Health Checks**: Built-in support for health check endpoints
- **Concurrent Request Handling**: Efficient handling of multiple simultaneous requests

## Usage

### Command Line Options

```bash
./agentapi-proxy [options]

Options:
  -port string
        Port to listen on (default "8080")
  -config string
        Configuration file path (default "config.json")
  -verbose
        Enable verbose logging
```

### Configuration

The proxy uses a JSON configuration file to define routing rules:

```json
{
  "default_backend": "http://localhost:3000",
  "routes": {
    "/api/{org}/{repo}": "http://agentapi-backend:8080",
    "/api/{org}/{repo}/issues": "http://issues-service:9000",
    "/api/{org}/{repo}/pulls": "http://pr-service:9001",
    "/health": "http://health-check:8080"
  }
}
```

#### Configuration Fields

- `default_backend` (optional): Backend URL to use for routes that don't match any pattern
- `routes`: Map of URL patterns to backend URLs

#### URL Patterns

- Use `{variable}` syntax for path variables (e.g., `{org}`, `{repo}`)
- Patterns are matched using gorilla/mux router
- More specific patterns take precedence over general ones

## Examples

### Basic Usage

1. Create a configuration file:
```bash
cp config.json.example config.json
# Edit config.json to match your backend services
```

2. Run the proxy:
```bash
go run .
```

3. The proxy will start on port 8080 and route requests according to your configuration.

### Example Requests

With the example configuration:

```bash
# Routes to agentapi-backend:8080
curl http://localhost:8080/api/myorg/myrepo

# Routes to issues-service:9000  
curl http://localhost:8080/api/myorg/myrepo/issues

# Routes to health-check:8080
curl http://localhost:8080/health

# Routes to default_backend (if configured)
curl http://localhost:8080/some/other/path
```

## Development

### Building

```bash
go build -o agentapi-proxy .
```

### Testing

Run all tests:
```bash
go test -v ./...
```

Run specific test suites:
```bash
# Unit tests
go test -v -run TestProxy
go test -v -run TestConfig

# Integration tests  
go test -v -run TestIntegration
```

### Running Tests with Coverage

```bash
go test -v -cover ./...
```

## Docker Usage

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o agentapi-proxy .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/agentapi-proxy .
COPY config.json .
EXPOSE 8080
CMD ["./agentapi-proxy"]
```

## Architecture

The proxy consists of several key components:

- **Router**: Uses gorilla/mux for URL pattern matching and routing
- **Reverse Proxy**: Built on Go's `httputil.ReverseProxy` for efficient request forwarding
- **Configuration Manager**: Handles loading and parsing of JSON configuration files
- **Error Handling**: Proper error responses and logging for debugging

## License

See LICENSE file for details.