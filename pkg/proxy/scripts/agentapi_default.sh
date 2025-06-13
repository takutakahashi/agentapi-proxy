#!/bin/bash

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
    agentapi-proxy helpers init-github-repository --ignore-missing-config --repo-fullname "$GITHUB_REPO_FULLNAME" --clone-dir "$GITHUB_CLONE_DIR"
    echo "Changing directory to $GITHUB_CLONE_DIR"
    cd "$GITHUB_CLONE_DIR"
else
    echo "GitHub parameters not provided, skipping repository setup"
fi

CLAUDE_DIR=. agentapi-proxy helpers setup-claude-code
exec agentapi server --port "$PORT" {{.AgentAPIArgs}} -- claude {{.ClaudeArgs}}
