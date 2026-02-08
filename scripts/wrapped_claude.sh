#!/bin/bash
# wrapped_claude.sh - Claude CLI wrapper with execution logging

set -euo pipefail

# Configuration
CLAUDE_BIN="/opt/claude/bin/claude"
LOG_DIR="${HOME}/.claude/command_history"
LOG_FILE="${LOG_DIR}/history.jsonl"
MAX_LOG_SIZE=$((10 * 1024 * 1024))  # 10MB
MAX_LOG_FILES=5

# Ensure log directory exists
mkdir -p "${LOG_DIR}"

# Capture execution metadata
START_TIME=$(date +%s%3N)
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
CWD=$(pwd)
USER=$(whoami)
SESSION_ID="${SESSION_ID:-}"

# Execute claude and capture exit code
"${CLAUDE_BIN}" "$@"
EXIT_CODE=$?

# Calculate duration
END_TIME=$(date +%s%3N)
DURATION=$((END_TIME - START_TIME))

# Build JSON log entry (escape args properly)
ARGS_JSON=$(printf '%s\n' "$@" | jq -R . | jq -s .)

LOG_ENTRY=$(jq -n \
  --arg timestamp "${TIMESTAMP}" \
  --arg command "claude" \
  --argjson args "${ARGS_JSON}" \
  --arg cwd "${CWD}" \
  --argjson exit_code "${EXIT_CODE}" \
  --argjson duration_ms "${DURATION}" \
  --arg user "${USER}" \
  --arg session_id "${SESSION_ID}" \
  '{
    timestamp: $timestamp,
    command: $command,
    args: $args,
    cwd: $cwd,
    exit_code: $exit_code,
    duration_ms: $duration_ms,
    user: $user,
    session_id: $session_id
  }')

# Append to log file
echo "${LOG_ENTRY}" >> "${LOG_FILE}" 2>/dev/null || true

# Rotate logs if necessary
if [ -f "${LOG_FILE}" ] && [ $(stat -f%z "${LOG_FILE}" 2>/dev/null || stat -c%s "${LOG_FILE}" 2>/dev/null || echo 0) -gt ${MAX_LOG_SIZE} ]; then
  for i in $(seq $((MAX_LOG_FILES - 1)) -1 1); do
    [ -f "${LOG_FILE}.$i" ] && mv "${LOG_FILE}.$i" "${LOG_FILE}.$((i + 1))"
  done
  mv "${LOG_FILE}" "${LOG_FILE}.1"
  touch "${LOG_FILE}"
fi

# Exit with claude's exit code
exit ${EXIT_CODE}
