# syntax=docker/dockerfile:1.7

# Build stage
FROM golang:1.25-alpine AS builder

# Install git for Go modules
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .


# Build the application with optimizations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/agentapi-proxy main.go

# Download agentapi release binary instead of rebuilding it from source.
FROM node:22.19.0-bookworm-slim AS node-runtime

FROM alpine:3.22 AS agentapi-downloader

ARG TARGETOS=linux
ARG TARGETARCH
ARG AGENTAPI_VERSION=v0.6.0

RUN apk add --no-cache ca-certificates curl && \
    set -ex && \
    if [ -z "$TARGETARCH" ]; then \
      case "$(apk --print-arch)" in \
        x86_64) TARGETARCH="amd64" ;; \
        aarch64) TARGETARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(apk --print-arch)" && exit 1 ;; \
      esac; \
    fi && \
    case "$TARGETARCH" in \
      amd64|arm64) ;; \
      *) echo "Unsupported architecture: $TARGETARCH" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/coder/agentapi/releases/download/${AGENTAPI_VERSION}/agentapi-${TARGETOS}-${TARGETARCH}" \
      -o /agentapi && \
    chmod +x /agentapi && \
    echo "Downloaded agentapi binary info:" && \
    ls -la /agentapi

# Runtime stage
FROM ubuntu:24.04

# Copy the official Node.js runtime, including npm and npx.
COPY --from=node-runtime /usr/local/ /usr/local/

# Install essential OS packages and GitHub CLI.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends ca-certificates curl bash git make sudo jq procps tzdata iptables && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y gh

# Create a non-root user
RUN groupadd -r agentapi && useradd -r -g agentapi -d /home/agentapi -s /bin/bash agentapi && \
    mkdir -p /home/agentapi && \
    chown -R agentapi:agentapi /home/agentapi && \
    echo 'agentapi ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

# Set working directory
WORKDIR /home/agentapi/workdir

# Copy agentapi binary from builder stage
COPY --from=agentapi-downloader /agentapi /usr/local/bin/agentapi

# Copy github-mcp-server binary from official image
COPY --from=ghcr.io/github/github-mcp-server:v0.26.3 /server/github-mcp-server /usr/local/bin/

# Copy docker CLI binary and compose plugin (no daemon)
COPY --from=docker:27-cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker:27-cli /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose
RUN sudo ln -sf /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/bin/docker-compose

# Download acp-posts binary for Slack integration subprocess
ARG ACP_POSTS_VERSION=v0.1.0
RUN ARCH=$(dpkg --print-architecture) && \
    case "$ARCH" in \
      amd64) ACP_POSTS_ARCH="linux-amd64" ;; \
      arm64) ACP_POSTS_ARCH="linux-arm64" ;; \
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/takutakahashi/acp-posts/releases/download/${ACP_POSTS_VERSION}/acp-posts-${ACP_POSTS_ARCH}" \
      -o /usr/local/bin/acp-posts && \
    chmod +x /usr/local/bin/acp-posts

# Download otelcol-contrib binary for in-process OpenTelemetry Collector support.
# Used when OtelCollectorInProcess=true (e.g. when stock inventory is enabled) so
# that otelcol starts after user context is known instead of at Pod creation time.
ARG OTELCOL_VERSION=0.143.1
RUN ARCH=$(dpkg --print-architecture) && \
    case "$ARCH" in \
      amd64) OTELCOL_ARCH="linux_amd64" ;; \
      arm64) OTELCOL_ARCH="linux_arm64" ;; \
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v${OTELCOL_VERSION}/otelcol-contrib_${OTELCOL_VERSION}_${OTELCOL_ARCH}.tar.gz" \
      -o /tmp/otelcol.tar.gz && \
    tar -xzf /tmp/otelcol.tar.gz -C /tmp otelcol-contrib && \
    mv /tmp/otelcol-contrib /usr/local/bin/otelcol && \
    chmod +x /usr/local/bin/otelcol && \
    rm /tmp/otelcol.tar.gz

# Pre-create /opt/webhook so that the agent-provisioner (running as agentapi)
# can write the webhook payload file for stock sessions.  In non-stock sessions
# this directory is provided by the Kubernetes Secret volume mount, but stock
# sessions have no such mount at pod creation time.
RUN mkdir -p /opt/webhook && chown agentapi:agentapi /opt/webhook

# Pre-create /opt/acp-posts so that the acp-server bridge (running as agentapi)
# can write the conversation history file for acp-posts Slack integration.
RUN mkdir -p /opt/acp-posts && chown agentapi:agentapi /opt/acp-posts

# Pre-create /etc/codex so agent-provisioner can write managed requirements.toml
# for Codex hooks at provision time.
RUN mkdir -p /etc/codex && chown agentapi:agentapi /etc/codex

# Switch to non-root user
USER agentapi

# Configure global gitignore for .claude, .codex directories and mise.toml
COPY config/gitignore_global /home/agentapi/.gitignore_global
RUN git config --global core.excludesfile ~/.gitignore_global

# Set Go environment variables to use /home/agentapi directory
ENV GOPATH=/home/agentapi/go
ENV GOMODCACHE=/home/agentapi/go/pkg/mod
ENV GOCACHE=/home/agentapi/.cache/go-build

# Install mise
RUN curl https://mise.run | sh && \
    /home/agentapi/.local/bin/mise --version && \
    echo 'export PATH="/home/agentapi/.local/bin:/home/agentapi/.local/share/mise/shims:$PATH"' >> /home/agentapi/.bashrc

# Install claude code and move to /opt/claude for persistence across volume mounts
# The installer creates a symlink at ~/.local/bin/claude -> ~/.local/share/claude/versions/X.X.X
# We copy with -L to follow the symlink and get the actual binary, then clean up
# Then create a symlink at ~/.local/bin/claude -> /opt/claude/bin/claude for volume mount compatibility
RUN curl -fsSL https://claude.ai/install.sh | bash -s 2.1.12 && \
    sudo mkdir -p /opt/claude/bin && \
    sudo cp -L /home/agentapi/.local/bin/claude /opt/claude/bin/claude && \
    sudo chown agentapi:agentapi /opt/claude/bin/claude && \
    sudo chmod +x /opt/claude/bin/claude && \
    rm -rf /home/agentapi/.local/share/claude/versions /home/agentapi/.local/bin/claude 2>/dev/null || true && \
    mkdir -p /home/agentapi/.local/bin && \
    ln -sf /opt/claude/bin/claude /home/agentapi/.local/bin/claude

# Install uv for Python package management (enables uvx) and clean cache
RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    echo 'export PATH="/home/agentapi/.cargo/bin:$PATH"' >> /home/agentapi/.bashrc && \
    rm -rf /home/agentapi/.cache/uv 2>/dev/null || true

# Keep npm global packages in the non-root user home directory.
ENV NPM_CONFIG_PREFIX=/home/agentapi/.npm-global

# Set combined PATH environment variable (including /opt/claude/bin for claude CLI and /opt/cursor/bin for Cursor CLI)
ENV PATH="/opt/claude/bin:/opt/cursor/bin:/home/agentapi/.npm-global/bin:/home/agentapi/.cargo/bin:/home/agentapi/.local/bin:/home/agentapi/.local/share/mise/shims:$PATH"

# Install codex CLI with npm. Its node shebang resolves to the real Node.js executable.
RUN npm install --global @openai/codex

# Install Pi, pi-acp, and Pi extensions for pi-ollama sessions.
# pi-acp starts `pi --mode rpc`, and pi-ollama-cloud connects directly to
# https://ollama.com/v1 using OLLAMA_API_KEY/OLLAMA_API_KEYS. pi-mcp-adapter
# lets Pi read MCP servers from ~/.config/mcp/mcp.json.
RUN npm install --global @earendil-works/pi-coding-agent && \
    npm install --global pi-acp@latest && \
    mkdir -p /home/agentapi/.pi/agent/npm && \
    printf '{"private":true,"dependencies":{}}\n' > /home/agentapi/.pi/agent/npm/package.json && \
    mkdir -p /tmp/pi-npm-shim && \
    printf '%s\n' \
      '#!/bin/sh' \
      'set -e' \
      'prefix=""' \
      'packages=""' \
      'while [ "$#" -gt 0 ]; do' \
      '  case "$1" in' \
      '    install) shift ;;' \
      '    --prefix) prefix="$2"; shift 2 ;;' \
      '    --legacy-peer-deps) shift ;;' \
      '    *) packages="$packages $1"; shift ;;' \
      '  esac' \
      'done' \
      'if [ -z "$prefix" ]; then prefix="$PWD"; fi' \
      'mkdir -p "$prefix"' \
      'test -f "$prefix/package.json" || printf "%s\n" "{\"private\":true,\"dependencies\":{}}" > "$prefix/package.json"' \
      'exec /usr/local/bin/npm install --prefix "$prefix" --legacy-peer-deps $packages' \
      > /tmp/pi-npm-shim/npm && \
    chmod +x /tmp/pi-npm-shim/npm && \
    PATH="/tmp/pi-npm-shim:$PATH" pi install npm:pi-ollama-cloud && \
    PATH="/tmp/pi-npm-shim:$PATH" pi install npm:pi-mcp-adapter && \
    rm -rf /tmp/pi-npm-shim

# Install Cursor Agent CLI and place stable wrappers in /opt/cursor/bin.
# Official install docs: https://cursor.com/docs/cli/installation
RUN curl https://cursor.com/install -fsS | bash && \
    sudo mkdir -p /opt/cursor/bin /opt/cursor/agent && \
    CURSOR_AGENT_TARGET="$(readlink -f /home/agentapi/.local/bin/agent)" && \
    sudo cp -a "$(dirname "$CURSOR_AGENT_TARGET")/." /opt/cursor/agent/ && \
    sudo ln -sf /opt/cursor/agent/cursor-agent /opt/cursor/bin/agent && \
    sudo ln -sf /opt/cursor/agent/cursor-agent /opt/cursor/bin/cursor-agent && \
    sudo chown -R agentapi:agentapi /opt/cursor && \
    sudo chmod +x /opt/cursor/agent/cursor-agent && \
    rm -rf /home/agentapi/.local/share/cursor-agent /home/agentapi/.local/bin/agent /home/agentapi/.local/bin/cursor-agent 2>/dev/null || true && \
    ln -sf /opt/cursor/bin/agent /home/agentapi/.local/bin/agent && \
    ln -sf /opt/cursor/bin/cursor-agent /home/agentapi/.local/bin/cursor-agent

# Set default CLAUDE_MD_PATH for Docker environment
ENV CLAUDE_MD_PATH=/tmp/config/CLAUDE.md

# Use the installed Claude Code CLI directly for the SDK and ACP adapter.
ENV CLAUDE_CODE_EXECUTABLE_PATH=/opt/claude/bin/claude
ENV CLAUDE_CODE_EXECUTABLE=/opt/claude/bin/claude

# Copy the frequently changing proxy binary after the expensive runtime toolchain
# setup so ordinary app changes do not invalidate those cached layers.
COPY --from=builder /app/bin/agentapi-proxy /usr/local/bin/

# Copy CLAUDE.md to temporary location for entrypoint script
COPY config/CLAUDE.md /tmp/config/CLAUDE.md
COPY config/CLAUDE.md /etc/claude-code/CLAUDE.md
COPY config/managed-settings.json /etc/claude-code/managed-settings.json

# Copy Codex configuration files for entrypoint script
# AGENTS.md is the Codex equivalent of CLAUDE.md (user-level instructions → ~/.codex/instructions.md)
# codex-config.toml is the Codex equivalent of claude.json (bypasses interactive prompts)
COPY config/AGENTS.md /tmp/config/AGENTS.md
COPY config/codex-config.toml /tmp/config/codex-config.toml

# Copy entrypoint script
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sudo chmod +x /usr/local/bin/entrypoint.sh

# Copy wrapped_claude script
COPY --chmod=755 scripts/wrapped_claude.sh /usr/local/bin/wrapped_claude

# Expose port
EXPOSE 8080

# Run the application with entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["mise", "exec", "--", "agentapi-proxy", "server"]
