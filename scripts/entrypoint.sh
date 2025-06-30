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

# Setup Claude Code configuration with MCP servers
if [ -f /tmp/config/claude_code_config.json ]; then
    echo "Setting up Claude Code MCP configuration..."
    # Ensure claude-code config directory exists
    mkdir -p /home/agentapi/.config/claude-code
    cp /tmp/config/claude_code_config.json /home/agentapi/.config/claude-code/config.json
    echo "Claude Code MCP configuration copied successfully"
fi

# Execute the original command
exec "$@"