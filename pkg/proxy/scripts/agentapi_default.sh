#!/bin/bash

# AgentAPI startup script with GitHub integration
# This script is executed when the github_repo parameter is present

# Get port from command line argument
PORT="${1:-8080}"

agentapi-agent helpers setup-claude-code
exec agentapi server --port "$PORT" $AGENTAPI_ARGS -- claude $CLAUDE_ARGS
