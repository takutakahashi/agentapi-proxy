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

# Fix permissions for persistent volume directory
if [ -d "$HOME/.agentapi-proxy" ]; then
    echo "Fixing permissions for $HOME/.agentapi-proxy..."
    # Change ownership to current user
    sudo chown -R $(id -u):$(id -g) "$HOME/.agentapi-proxy" || chown -R $(id -u):$(id -g) "$HOME/.agentapi-proxy" 2>/dev/null || true
    
    # Create myclaudes directory if it doesn't exist
    mkdir -p "$HOME/.agentapi-proxy/myclaudes"
    
    # Set proper permissions for myclaudes directory
    chmod 755 "$HOME/.agentapi-proxy/myclaudes"
    
    echo "Permissions fixed successfully"
fi

# Execute the original command
exec "$@"