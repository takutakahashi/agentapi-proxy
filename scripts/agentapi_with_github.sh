#!/bin/bash

# AgentAPI startup script with GitHub integration
# This script is executed when the github_repo parameter is present

# Get port from command line argument
PORT="${1:-8080}"

# Additional GitHub-specific environment setup
export AGENTAPI_GITHUB_INTEGRATION=true

# Start agentapi server with GitHub integration enabled
exec agentapi server --port "$PORT"