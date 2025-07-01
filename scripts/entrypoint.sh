#!/bin/bash
set -e

# Create .claude directory if it doesn't exist
mkdir -p /home/agentapi/.claude

# Copy CLAUDE.md if it doesn't exist or if it's older than the source
if [ ! -f /home/agentapi/.claude/CLAUDE.md ] || [ /tmp/config/CLAUDE.md -nt /home/agentapi/.claude/CLAUDE.md ]; then
    echo "Copying CLAUDE.md to .claude directory..."
    cp /tmp/config/CLAUDE.md /home/agentapi/.claude/CLAUDE.md
    echo "CLAUDE.md copied successfully"
fi

# Setup Playwright MCP server
echo "Setting up Playwright MCP server..."
# Add Playwright MCP using claude mcp add command
# Using --scope user to make it available across all projects
claude mcp add playwright npx -- @playwright/mcp@latest --scope user || {
    echo "Warning: Failed to add Playwright MCP server. This is normal if it's already installed."
}

# Execute the original command
exec "$@"