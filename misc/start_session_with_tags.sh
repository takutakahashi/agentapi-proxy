#!/bin/bash
set -e  # Exit on any command failure

# Start agentapi-proxy session with tags
# Usage: ./start_session_with_tags.sh [host:port] [user_id] [repository] [branch] [env]

HOST=${1:-"localhost:8081"}
USER_ID=${2:-"user123"}
REPOSITORY=${3:-"takutakahashi/agentapi-ui"}

echo "Starting session for user: $USER_ID on host: $HOST"
echo "Repository: $REPOSITORY"

if ! curl -f -X POST "http://${HOST}/start" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"${USER_ID}\",
    \"tags\": {
      \"repository\": \"${REPOSITORY}\"
    }
  }"; then
    echo "ERROR: Failed to start session" >&2
    echo "Check if agentapi-proxy server is running at $HOST" >&2
    echo "Verify network connectivity and server status" >&2
    exit 1
fi

echo "Session started successfully"
