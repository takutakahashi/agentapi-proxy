# Suggested Commands

## Development Workflow

### Code Formatting
```bash
make gofmt
```
Formats Go code with `go fmt ./...`

### Linting
```bash
make lint
```
Runs golangci-lint with 5-minute timeout

### Testing
```bash
make test
```
Runs all tests with race detector

### Building
```bash
make build
```
Builds the binary to `bin/agentapi-proxy`

### Full CI Pipeline
```bash
make ci
```
Runs lint, test, and build in sequence

### End-to-End Tests
```bash
make e2e
```
Runs end-to-end integration tests

## Before Committing
**Always run these commands before committing:**
```bash
make lint
make test
```

## Docker Operations
```bash
# Build Docker image
make docker-build

# Push Docker image
make docker-push
```

## Running the Server
```bash
# Using built binary
./bin/agentapi-proxy server

# With configuration
./bin/agentapi-proxy server --config config.json --port 8080 --verbose
```