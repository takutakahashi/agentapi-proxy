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

# Set up test environment
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

echo "Building agentapi-proxy..."
make build

echo "Running e2e tests..."
cd test/e2e

# Run the e2e tests with verbose output
go test -v -timeout=60s ./... -tags=e2e

echo "E2E tests completed successfully!"