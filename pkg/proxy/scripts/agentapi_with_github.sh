#!/bin/bash

# AgentAPI startup script with GitHub integration
# This script is executed when repository tag is present

# Get port from command line argument
PORT="${1:-8080}"

# Additional GitHub-specific environment setup
export AGENTAPI_GITHUB_INTEGRATION=true

# Execute init git repo helper if GITHUB_REPO_URL is set and is a valid GitHub URL
if [[ -n "$GITHUB_REPO_URL" ]]; then
    echo "Repository URL found: $GITHUB_REPO_URL"
    # Check if it's a valid GitHub URL
    if [[ "$GITHUB_REPO_URL" =~ ^https://github\.com/ ]] || [[ "$GITHUB_REPO_URL" =~ ^git@github\.com: ]] || [[ "$GITHUB_REPO_URL" =~ ^http://github\.com/ ]]; then
        echo "Valid GitHub URL detected. Executing init git repo helper..."
        agentapi-proxy helpers init-github-repository
        if [[ $? -eq 0 ]]; then
            echo "Init git repo helper completed successfully."
        else
            echo "Init git repo helper failed."
            exit 1
        fi
    else
        echo "Repository URL is not a valid GitHub URL. Skipping init git repo helper."
    fi
fi

# Start agentapi server with GitHub integration enabled
exec agentapi server --port "$PORT"