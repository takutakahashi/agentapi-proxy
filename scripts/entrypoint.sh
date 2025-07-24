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

# Setup Claude Code hooks configuration
echo "Setting up Claude Code hooks configuration..."
cat > /home/agentapi/.claude/settings.json << 'EOF'
{
  "workspaceFolders": [],
  "recentWorkspaces": [],
  "settings": {},
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "curl -s -X POST http://localhost:8080/notifications/webhook -H 'Content-Type: application/json' -d '{\"session_id\":\"claude-code-session\",\"user_id\":\"anonymous\",\"event_type\":\"session_completed\",\"timestamp\":\"'$(date -Iseconds)'\",\"data\":{\"title\":\"Claude Code 完了\",\"body\":\"セッションが正常に完了しました。お疲れ様でした！\",\"type\":\"session_completed\"}}' || echo 'Claude Code セッションが完了しました。お疲れ様でした！'"
          }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "curl -s -X POST http://localhost:8080/notifications/webhook -H 'Content-Type: application/json' -d '{\"session_id\":\"claude-code-session\",\"user_id\":\"anonymous\",\"event_type\":\"permission_request\",\"timestamp\":\"'$(date -Iseconds)'\",\"data\":{\"title\":\"Claude Code 通知\",\"body\":\"ツールの使用許可が必要です。確認をお願いします。\",\"type\":\"permission_request\"}}' || echo 'Claude Code から通知: ツールの使用許可が必要です。'"
          }
        ]
      }
    ]
  }
}
EOF
echo "Claude Code hooks configuration setup completed"

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
