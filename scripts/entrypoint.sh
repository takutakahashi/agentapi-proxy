#!/bin/bash
set -e

# Set default CLAUDE_MD_PATH for Docker environment if not already set
export CLAUDE_MD_PATH="${CLAUDE_MD_PATH:-/tmp/config/CLAUDE.md}"

# Ensure CLAUDE_CONFIG_DIR exists (may be overridden externally)
CLAUDE_CONFIG_DIR="${CLAUDE_CONFIG_DIR:-/opt/claude-config}"
mkdir -p "${CLAUDE_CONFIG_DIR}"

# Copy CLAUDE.md if it doesn't exist or if it's older than the source
if [ ! -f "${CLAUDE_CONFIG_DIR}/CLAUDE.md" ] || [ /tmp/config/CLAUDE.md -nt "${CLAUDE_CONFIG_DIR}/CLAUDE.md" ]; then
    echo "Copying CLAUDE.md to ${CLAUDE_CONFIG_DIR}..."
    cp /tmp/config/CLAUDE.md "${CLAUDE_CONFIG_DIR}/CLAUDE.md"
    echo "CLAUDE.md copied successfully"
fi


# Fix permissions for persistent volume directories only if needed
if [ -d "$HOME/.agentapi-proxy" ]; then
    # Check if the directory ownership needs to be changed
    current_owner=$(stat -c "%u:%g" "$HOME/.agentapi-proxy" 2>/dev/null || echo "0:0")
    expected_owner="$(id -u):$(id -g)"
    
    if [ "$current_owner" != "$expected_owner" ]; then
        echo "Fixing permissions for $HOME/.agentapi-proxy directory..."
        # Only change ownership of the main directory, not recursively
        sudo chown $(id -u):$(id -g) "$HOME/.agentapi-proxy" || chown $(id -u):$(id -g) "$HOME/.agentapi-proxy" 2>/dev/null || true
        chmod 755 "$HOME/.agentapi-proxy"
        echo "Directory permissions fixed for $HOME/.agentapi-proxy"
    fi
    
    # Create myclaudes directory if it doesn't exist
    mkdir -p "$HOME/.agentapi-proxy/myclaudes"
    
    # Set proper permissions for myclaudes directory only
    chmod 755 "$HOME/.agentapi-proxy/myclaudes"
fi

# Fix permissions for workdir only if needed
if [ -d "$HOME/workdir" ]; then
    # Check if the directory ownership needs to be changed
    current_owner=$(stat -c "%u:%g" "$HOME/workdir" 2>/dev/null || echo "0:0")
    expected_owner="$(id -u):$(id -g)"
    
    if [ "$current_owner" != "$expected_owner" ]; then
        echo "Fixing permissions for $HOME/workdir directory..."
        # Only change ownership of the main directory, not recursively
        sudo chown $(id -u):$(id -g) "$HOME/workdir" || chown $(id -u):$(id -g) "$HOME/workdir" 2>/dev/null || true
        chmod 755 "$HOME/workdir"
        echo "Directory permissions fixed for $HOME/workdir"
    fi
fi

# Execute the original command
exec "$@"
