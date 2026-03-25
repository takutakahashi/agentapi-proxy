#!/bin/bash

# agentapi-proxy API Request Helper
# Usage: ./agentapi_request.sh <endpoint> [method] [data]

set -e

# Configuration from environment variables
API_BASE_URL="${AGENTAPI_PROXY_URL:-http://localhost:8080}"
API_KEY="${AGENTAPI_PROXY_API_KEY}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print usage
usage() {
    echo "Usage: $0 <endpoint> [method] [data]"
    echo ""
    echo "Examples:"
    echo "  $0 /start POST '{\"environment\":{\"GITHUB_TOKEN\":\"ghp_...\"}}'"
    echo "  $0 /search GET"
    echo "  $0 /sessions/SESSION_ID DELETE"
    echo ""
    echo "Environment variables:"
    echo "  AGENTAPI_PROXY_URL - API base URL (default: http://localhost:8080)"
    echo "  AGENTAPI_PROXY_API_KEY - API key for authentication (required)"
    exit 1
}

# Check arguments
if [ $# -lt 1 ]; then
    usage
fi

ENDPOINT="$1"
METHOD="${2:-GET}"
DATA="${3:-}"

# Check if API key is set
if [ -z "$API_KEY" ]; then
    echo -e "${RED}Error: AGENTAPI_PROXY_API_KEY environment variable is not set${NC}"
    echo "Set it with: export AGENTAPI_PROXY_API_KEY=your_api_key"
    exit 1
fi

# Build curl command
CURL_CMD="curl -s -w '\n\nHTTP Status: %{http_code}\n'"
CURL_CMD="$CURL_CMD -X $METHOD"
CURL_CMD="$CURL_CMD -H 'X-API-Key: $API_KEY'"

if [ -n "$DATA" ]; then
    CURL_CMD="$CURL_CMD -H 'Content-Type: application/json'"
    CURL_CMD="$CURL_CMD -d '$DATA'"
fi

URL="$API_BASE_URL$ENDPOINT"
CURL_CMD="$CURL_CMD '$URL'"

# Print request details
echo -e "${YELLOW}Request:${NC}"
echo "  Method: $METHOD"
echo "  URL: $URL"
if [ -n "$DATA" ]; then
    echo "  Data: $DATA"
fi
echo ""

# Execute request
echo -e "${YELLOW}Response:${NC}"
eval $CURL_CMD

echo ""
