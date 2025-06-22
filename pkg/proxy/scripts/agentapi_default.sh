#!/bin/bash
set -e  # Exit on any command failure

# AgentAPI startup script with GitHub integration
# This script is executed when the github_repo parameter is present

# Get parameters from command line arguments and templates
PORT="${1:-8080}"
GITHUB_REPO_FULLNAME="{{.RepoFullName}}"
GITHUB_CLONE_DIR="{{.CloneDir}}"

# GitHub environment variables embedded from template
export GITHUB_TOKEN="{{.GitHubToken}}"
export GITHUB_APP_ID="{{.GitHubAppID}}"
export GITHUB_INSTALLATION_ID="{{.GitHubInstallationID}}"
export GITHUB_APP_PEM_PATH="{{.GitHubAppPEMPath}}"
export GITHUB_API="{{.GitHubAPI}}"
export GITHUB_PERSONAL_ACCESS_TOKEN="{{.GitHubPersonalAccessToken}}"

# Set up GitHub repository if parameters are provided
if [[ -n "$GITHUB_REPO_FULLNAME" && -n "$GITHUB_CLONE_DIR" ]]; then
    echo "Initializing GitHub repository: $GITHUB_REPO_FULLNAME"
    if ! agentapi-proxy helpers init-github-repository --ignore-missing-config --repo-fullname "$GITHUB_REPO_FULLNAME" --clone-dir "$GITHUB_CLONE_DIR"; then
        echo "ERROR: Failed to initialize GitHub repository '$GITHUB_REPO_FULLNAME'" >&2
        echo "Check GitHub credentials and repository access permissions" >&2
        exit 1
    fi
    
    echo "Changing directory to $GITHUB_CLONE_DIR"
    if ! cd "$GITHUB_CLONE_DIR"; then
        echo "ERROR: Failed to change directory to '$GITHUB_CLONE_DIR'" >&2
        echo "Directory may not exist or permission denied" >&2
        exit 1
    fi
else
    echo "GitHub parameters not provided, skipping repository setup"
fi

echo "Setting up Claude Code environment"
if ! CLAUDE_DIR=. agentapi-proxy helpers setup-claude-code; then
    echo "ERROR: Failed to setup Claude Code environment" >&2
    echo "Check Claude Code installation and configuration" >&2
    exit 1
fi

echo "Starting agentapi server on port $PORT"
exec agentapi server --port "$PORT" {{.AgentAPIArgs}} -- claude {{.ClaudeArgs}}
