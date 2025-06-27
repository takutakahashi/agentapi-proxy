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

# Check if binary exists
if [ ! -f "./bin/agentapi-proxy" ]; then
    echo "Error: agentapi-proxy binary not found. Please run 'make build' first."
    exit 1
fi

echo "Running e2e tests..."

# Run the e2e tests with verbose output
go test -v -timeout=60s ./test/... -tags=e2e

echo "E2E tests completed successfully!"