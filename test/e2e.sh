#!/bin/bash

set -e

echo "Starting e2e tests for Claude Code integration..."

# Check if required tools are available
if ! command -v claude &> /dev/null; then
    echo "Error: claude command not found. Please install Claude Code CLI."
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "Error: go command not found. Please install Go."
    exit 1
fi

# Debug: Show current directory and file listing
echo "Current working directory: $(pwd)"
echo "Contents of current directory:"
ls -la
echo "Contents of bin directory:"
ls -la bin/ || echo "bin directory not found"

# Check if binary exists in possible locations
BINARY_PATH=""
for path in "./bin/agentapi-proxy" "bin/agentapi-proxy" "../bin/agentapi-proxy"; do
    if [ -f "$path" ]; then
        echo "Found binary at: $path"
        BINARY_PATH="$path"
        break
    fi
done

if [ -z "$BINARY_PATH" ]; then
    echo "Error: agentapi-proxy binary not found in any of: ./bin/agentapi-proxy, bin/agentapi-proxy, ../bin/agentapi-proxy"
    echo "Please run 'make build' first."
    exit 1
fi

# Export binary path for tests to use
export AGENTAPI_PROXY_BINARY="$BINARY_PATH"

echo "Running e2e tests..."

# Run the e2e tests with verbose output
go test -v -timeout=${GO_TEST_TIMEOUT:-60s} ./test/... -tags=e2e

echo "E2E tests completed successfully!"