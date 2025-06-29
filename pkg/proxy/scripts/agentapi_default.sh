#!/bin/bash

# AgentAPI startup script with GitHub integration
# This script is executed when the github_repo parameter is present

# Get parameters from command line arguments and templates
PORT="${1:-8080}"
GITHUB_REPO_FULLNAME="{{.RepoFullName}}"
GITHUB_CLONE_DIR="{{.CloneDir}}"
USER_ID="{{.UserID}}"
ENABLE_MULTIPLE_USERS="{{.EnableMultipleUsers}}"
USER_HOME_DIR="{{.UserHomeDir}}"

# GitHub environment variables embedded from template
export GITHUB_TOKEN="{{.GitHubToken}}"
export GITHUB_APP_ID="{{.GitHubAppID}}"
export GITHUB_INSTALLATION_ID="{{.GitHubInstallationID}}"
export GITHUB_APP_PEM_PATH="{{.GitHubAppPEMPath}}"
export GITHUB_API="{{.GitHubAPI}}"
export GITHUB_PERSONAL_ACCESS_TOKEN="{{.GitHubPersonalAccessToken}}"

# Set user-specific HOME directory if multiple users is enabled
if [[ "$ENABLE_MULTIPLE_USERS" == "true" && -n "$USER_HOME_DIR" ]]; then
    echo "Setting HOME to user-specific directory: $USER_HOME_DIR"
    export HOME="$USER_HOME_DIR"
    
    # Ensure the user home directory exists
    if [[ ! -d "$USER_HOME_DIR" ]]; then
        echo "Creating user home directory: $USER_HOME_DIR"
        mkdir -p "$USER_HOME_DIR"
    fi
fi

# Set up GitHub repository if parameters are provided
if [[ -n "$GITHUB_REPO_FULLNAME" && -n "$GITHUB_CLONE_DIR" ]]; then
    if ! agentapi-proxy helpers init-github-repository --ignore-missing-config --repo-fullname "$GITHUB_REPO_FULLNAME" --clone-dir "$GITHUB_CLONE_DIR"; then
        echo "Failed to initialize GitHub repository" >&2
        exit 1
    fi
    echo "Changing directory to $GITHUB_CLONE_DIR"
    cd "$GITHUB_CLONE_DIR"
else
    echo "GitHub parameters not provided, skipping repository setup"
    # Create session directory even when no repository is cloned
    if [[ -n "$GITHUB_CLONE_DIR" ]]; then
        echo "Creating session directory: $GITHUB_CLONE_DIR"
        mkdir -p "$GITHUB_CLONE_DIR"
        echo "Changing directory to $GITHUB_CLONE_DIR"
        cd "$GITHUB_CLONE_DIR"
    fi
fi

CLAUDE_DIR=. MCP_CONFIGS='{{.MCPConfigs}}' agentapi-proxy helpers setup-claude-code
exec agentapi server --port "$PORT" {{.AgentAPIArgs}} -- claude {{.ClaudeArgs}}
