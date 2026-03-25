#!/bin/bash
set -e

# Set default CLAUDE_MD_PATH for Docker environment if not already set
export CLAUDE_MD_PATH="${CLAUDE_MD_PATH:-/tmp/config/CLAUDE.md}"

# Create .claude directory if it doesn't exist
mkdir -p /home/agentapi/.claude

# Copy CLAUDE.md if it doesn't exist or if it's older than the source
if [ ! -f /home/agentapi/.claude/CLAUDE.md ] || [ /tmp/config/CLAUDE.md -nt /home/agentapi/.claude/CLAUDE.md ]; then
    echo "Copying CLAUDE.md to .claude directory..."
    cp /tmp/config/CLAUDE.md /home/agentapi/.claude/CLAUDE.md
    echo "CLAUDE.md copied successfully"
fi


# Set up ccplant plugin from pre-baked marketplace in /opt/claude-marketplace.
# This runs every startup so that the plugin is always configured even when
# ~/.claude is a fresh volume mount.
MARKETPLACE_DIR="/opt/claude-marketplace/takutakahashi-plugins"
PLUGIN_DIR="${MARKETPLACE_DIR}/plugins/ccplant"
if [ -d "${MARKETPLACE_DIR}" ]; then
    echo "Setting up ccplant plugin from pre-baked marketplace..."

    mkdir -p /home/agentapi/.claude/plugins

    # --- known_marketplaces.json ---
    KNOWN_MARKETPLACES="/home/agentapi/.claude/plugins/known_marketplaces.json"
    if [ ! -f "${KNOWN_MARKETPLACES}" ]; then
        echo '{}' > "${KNOWN_MARKETPLACES}"
    fi
    if ! jq -e '.["takutakahashi-plugins"]' "${KNOWN_MARKETPLACES}" > /dev/null 2>&1; then
        tmp=$(mktemp)
        jq --arg path "${MARKETPLACE_DIR}" --arg ts "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" \
            '. + {"takutakahashi-plugins": {"source": {"source": "directory", "path": $path}, "installLocation": $path, "lastUpdated": $ts}}' \
            "${KNOWN_MARKETPLACES}" > "${tmp}" && mv "${tmp}" "${KNOWN_MARKETPLACES}"
        echo "Registered takutakahashi-plugins marketplace"
    fi

    # --- installed_plugins.json ---
    INSTALLED_PLUGINS="/home/agentapi/.claude/plugins/installed_plugins.json"
    if [ ! -f "${INSTALLED_PLUGINS}" ]; then
        echo '{"version": 2, "plugins": {}}' > "${INSTALLED_PLUGINS}"
    fi
    if ! jq -e '.plugins["ccplant@takutakahashi-plugins"]' "${INSTALLED_PLUGINS}" > /dev/null 2>&1; then
        tmp=$(mktemp)
        jq --arg path "${PLUGIN_DIR}" --arg ts "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" \
            '.plugins["ccplant@takutakahashi-plugins"] = [{"scope": "user", "installPath": $path, "version": "2.0.0", "installedAt": $ts, "lastUpdated": $ts, "gitCommitSha": "prebaked"}]' \
            "${INSTALLED_PLUGINS}" > "${tmp}" && mv "${tmp}" "${INSTALLED_PLUGINS}"
        echo "Registered ccplant plugin"
    fi

    # --- settings.json: enable plugin ---
    SETTINGS="/home/agentapi/.claude/settings.json"
    if [ ! -f "${SETTINGS}" ]; then
        echo '{}' > "${SETTINGS}"
    fi
    if ! jq -e '.enabledPlugins["ccplant@takutakahashi-plugins"] // false' "${SETTINGS}" | grep -q true; then
        tmp=$(mktemp)
        jq '.enabledPlugins = (.enabledPlugins // {}) | .enabledPlugins["ccplant@takutakahashi-plugins"] = true' \
            "${SETTINGS}" > "${tmp}" && mv "${tmp}" "${SETTINGS}"
        echo "Enabled ccplant@takutakahashi-plugins in settings"
    fi

    echo "ccplant plugin setup complete"
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
