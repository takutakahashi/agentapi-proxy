#!/bin/bash

# Start agentapi-proxy session with tags
# Usage: ./start_session_with_tags.sh [host:port] [user_id] [repository] [branch] [env]

HOST=${1:-"localhost:8080"}
USER_ID=${2:-"user123"}
REPOSITORY=${3:-"agentapi-proxy"}
BRANCH=${4:-"main"}
ENV=${5:-"production"}

curl -X POST "http://${HOST}/start" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"${USER_ID}\",
    \"tags\": {
      \"repository\": \"${REPOSITORY}\",
      \"branch\": \"${BRANCH}\",
      \"env\": \"${ENV}\"
    }
  }"