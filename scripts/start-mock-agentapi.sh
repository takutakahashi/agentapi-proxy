#!/bin/bash

# Mock AgentAPI Server startup script for e2e testing
# This script compiles and starts a mock agentapi server that implements
# the agentapi OpenAPI specification for testing purposes

set -e

# Default values
PORT="${1:-8080}"
VERBOSE=""
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_PATH="$SCRIPT_DIR/mock-agentapi-server"
SOURCE_PATH="$SCRIPT_DIR/mock-agentapi-server.go"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE="-v"
            shift
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS] [PORT]"
            echo ""
            echo "Start a mock agentapi server for e2e testing"
            echo ""
            echo "Options:"
            echo "  -p, --port PORT    Port to listen on (default: 8080)"
            echo "  -v, --verbose      Enable verbose logging"
            echo "  -h, --help         Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                 # Start on port 8080"
            echo "  $0 9000            # Start on port 9000"
            echo "  $0 -p 9000 -v      # Start on port 9000 with verbose logging"
            exit 0
            ;;
        *)
            # Assume it's a port number if it's numeric
            if [[ "$1" =~ ^[0-9]+$ ]]; then
                PORT="$1"
            else
                echo "Unknown option: $1"
                exit 1
            fi
            shift
            ;;
    esac
done

echo "🚀 Starting Mock AgentAPI Server for E2E Testing"
echo "📋 Port: $PORT"
echo "🔧 Verbose: $([ "$VERBOSE" == "-v" ] && echo "enabled" || echo "disabled")"

# Check if Go is available
if ! command -v go &> /dev/null; then
    echo "❌ Error: Go is not installed or not in PATH"
    echo "   Please install Go to run the mock server"
    exit 1
fi

# Check if source file exists
if [ ! -f "$SOURCE_PATH" ]; then
    echo "❌ Error: Mock server source not found at $SOURCE_PATH"
    exit 1
fi

echo "🔨 Compiling mock server..."

# Compile the Go program
if ! go build -o "$BINARY_PATH" "$SOURCE_PATH"; then
    echo "❌ Failed to compile mock server"
    exit 1
fi

echo "✅ Mock server compiled successfully"

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up..."
    if [ -f "$BINARY_PATH" ]; then
        rm -f "$BINARY_PATH"
        echo "🗑️  Removed compiled binary"
    fi
    exit 0
}

# Set up signal handlers for cleanup
trap cleanup EXIT INT TERM

echo "🎯 Starting mock agentapi server..."
echo "📡 Server will be available at http://localhost:$PORT"
echo "🛑 Press Ctrl+C to stop the server"
echo ""

# Start the server
exec "$BINARY_PATH" "$PORT" $VERBOSE