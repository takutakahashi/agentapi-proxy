#!/bin/bash

# End-to-End Test Script for agentapi-proxy
# This script tests the agentapi-proxy against a mock agentapi server

set -e

# Configuration
PROXY_PORT=8080
MOCK_PORT=9001
TEST_TIMEOUT=30
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MOCK_SCRIPT="$PROJECT_ROOT/scripts/start-mock-agentapi.sh"
PROXY_BINARY="$PROJECT_ROOT/bin/agentapi-proxy"
CONFIG_FILE="$PROJECT_ROOT/test/e2e-config.json"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log() {
    echo -e "${BLUE}[E2E]${NC} $*"
}

success() {
    echo -e "${GREEN}[E2E]${NC} ✅ $*"
}

warning() {
    echo -e "${YELLOW}[E2E]${NC} ⚠️  $*"
}

error() {
    echo -e "${RED}[E2E]${NC} ❌ $*"
}

# Cleanup function
cleanup() {
    log "🧹 Cleaning up test environment..."
    
    # Kill background processes
    if [ -n "$MOCK_PID" ]; then
        log "Stopping mock server (PID: $MOCK_PID)"
        kill $MOCK_PID 2>/dev/null || true
        wait $MOCK_PID 2>/dev/null || true
    fi
    
    if [ -n "$PROXY_PID" ]; then
        log "Stopping proxy server (PID: $PROXY_PID)"
        kill $PROXY_PID 2>/dev/null || true
        wait $PROXY_PID 2>/dev/null || true
    fi
    
    # Clean up temporary files
    [ -f "$CONFIG_FILE" ] && rm -f "$CONFIG_FILE"
    
    log "Cleanup completed"
}

# Set up signal handlers
trap cleanup EXIT INT TERM

# Wait for service to be ready
wait_for_service() {
    local port=$1
    local service_name=$2
    local max_attempts=30
    local attempt=1
    
    log "⏳ Waiting for $service_name on port $port..."
    
    while [ $attempt -le $max_attempts ]; do
        if curl -s -o /dev/null "http://localhost:$port/health" 2>/dev/null; then
            success "$service_name is ready on port $port"
            return 0
        fi
        
        if [ $attempt -eq $max_attempts ]; then
            error "$service_name failed to start on port $port after $max_attempts attempts"
            return 1
        fi
        
        sleep 1
        attempt=$((attempt + 1))
    done
}

# Test HTTP endpoint
test_endpoint() {
    local method=$1
    local path=$2
    local expected_status=$3
    local description=$4
    local data=$5
    
    log "🧪 Testing: $description"
    
    local curl_args=("-s" "-w" "%{http_code}" "-o" "/tmp/e2e_response")
    
    if [ "$method" = "POST" ] && [ -n "$data" ]; then
        curl_args+=("-X" "POST" "-H" "Content-Type: application/json" "-d" "$data")
    else
        curl_args+=("-X" "$method")
    fi
    
    local status_code
    status_code=$(curl "${curl_args[@]}" "http://localhost:$PROXY_PORT$path")
    
    if [ "$status_code" = "$expected_status" ]; then
        success "$description (Status: $status_code)"
        return 0
    else
        error "$description failed - Expected: $expected_status, Got: $status_code"
        if [ -f "/tmp/e2e_response" ]; then
            log "Response body:"
            cat /tmp/e2e_response
        fi
        return 1
    fi
}

# Main test execution
main() {
    log "🚀 Starting E2E Tests for agentapi-proxy"
    log "📊 Test Configuration:"
    log "   - Proxy Port: $PROXY_PORT"
    log "   - Mock Port: $MOCK_PORT"
    log "   - Timeout: ${TEST_TIMEOUT}s"
    
    # Check dependencies
    log "🔍 Checking dependencies..."
    
    if ! command -v curl &> /dev/null; then
        error "curl is required for e2e tests"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        error "Go is required to compile the mock server"
        exit 1
    fi
    
    # Build proxy if it doesn't exist
    if [ ! -f "$PROXY_BINARY" ]; then
        log "🔨 Building agentapi-proxy..."
        cd "$PROJECT_ROOT"
        make build
    fi
    
    # Create test configuration
    log "📝 Creating test configuration..."
    cat > "$CONFIG_FILE" << EOF
{
  "default_backend": "http://localhost:$MOCK_PORT",
  "routes": {
    "/api/{org}/{repo}": "http://localhost:$MOCK_PORT",
    "/health": "http://localhost:$MOCK_PORT"
  }
}
EOF
    
    # Start mock server
    log "🎭 Starting mock agentapi server on port $MOCK_PORT..."
    $MOCK_SCRIPT -p $MOCK_PORT -v > /tmp/mock_server.log 2>&1 &
    MOCK_PID=$!
    
    # Wait for mock server to be ready
    if ! wait_for_service $MOCK_PORT "Mock AgentAPI Server"; then
        error "Failed to start mock server"
        cat /tmp/mock_server.log
        exit 1
    fi
    
    # Start proxy server
    log "🔀 Starting agentapi-proxy on port $PROXY_PORT..."
    cd "$PROJECT_ROOT"
    $PROXY_BINARY server --port $PROXY_PORT --config "$CONFIG_FILE" --verbose > /tmp/proxy_server.log 2>&1 &
    PROXY_PID=$!
    
    # Wait for proxy to be ready
    if ! wait_for_service $PROXY_PORT "AgentAPI Proxy"; then
        error "Failed to start proxy server"
        cat /tmp/proxy_server.log
        exit 1
    fi
    
    log "🧪 Running E2E Tests..."
    
    # Test basic routing through proxy to mock server
    local failed_tests=0
    
    # Test 1: Health check
    test_endpoint "GET" "/health" "200" "Health check through proxy" || ((failed_tests++))
    
    # Test 2: AgentAPI status endpoint
    test_endpoint "GET" "/status" "200" "AgentAPI status endpoint" || ((failed_tests++))
    
    # Test 3: AgentAPI messages endpoint
    test_endpoint "GET" "/messages" "200" "AgentAPI messages endpoint" || ((failed_tests++))
    
    # Test 4: Send message to AgentAPI
    test_endpoint "POST" "/message" "200" "Send message to AgentAPI" '{"content": "Hello from e2e test", "type": "user"}' || ((failed_tests++))
    
    # Test 5: API route with parameters
    test_endpoint "GET" "/api/testorg/testrepo" "200" "API route with parameters" || ((failed_tests++))
    
    # Wait a bit for agent simulation to complete
    log "⏳ Waiting for agent simulation to complete..."
    sleep 3
    
    # Test 6: Check messages after agent response
    test_endpoint "GET" "/messages" "200" "Messages after agent response" || ((failed_tests++))
    
    # Test 7: Test SSE endpoint (just check if it's accessible)
    log "🧪 Testing: SSE events endpoint"
    if curl -s -m 2 "http://localhost:$PROXY_PORT/events" | head -n 1 | grep -q "event:"; then
        success "SSE events endpoint accessible"
    else
        error "SSE events endpoint test failed"
        ((failed_tests++))
    fi
    
    # Summary
    log "📊 Test Summary:"
    if [ $failed_tests -eq 0 ]; then
        success "All tests passed! 🎉"
        log "✨ The mock server is working correctly with the proxy"
        log "🎯 You can now use the mock server for your e2e testing:"
        log "   - Start mock: $MOCK_SCRIPT -p <port>"
        log "   - Configure proxy to route to mock server"
        log "   - Test your application against the proxy"
        exit 0
    else
        error "$failed_tests test(s) failed"
        log "📋 Check the logs for more details:"
        log "   - Mock server log: /tmp/mock_server.log"
        log "   - Proxy server log: /tmp/proxy_server.log"
        exit 1
    fi
}

# Show help
show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

End-to-End test script for agentapi-proxy with mock agentapi server.

Options:
  --proxy-port PORT    Port for proxy server (default: 8080)
  --mock-port PORT     Port for mock server (default: 9001)
  --timeout SECONDS    Test timeout in seconds (default: 30)
  -h, --help          Show this help message

Examples:
  $0                          # Run tests with default settings
  $0 --proxy-port 3000        # Use port 3000 for proxy
  $0 --mock-port 4000         # Use port 4000 for mock server

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --proxy-port)
            PROXY_PORT="$2"
            shift 2
            ;;
        --mock-port)
            MOCK_PORT="$2"
            shift 2
            ;;
        --timeout)
            TEST_TIMEOUT="$2"
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Run main function
main "$@"