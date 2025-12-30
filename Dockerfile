# Build stage
FROM golang:1.25-alpine AS builder

# Install git for Go modules
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN go build -o bin/agentapi-proxy main.go

# Build agentapi from source stage
FROM golang:1.25-alpine AS agentapi-builder

# Install git for cloning
RUN apk add --no-cache git

# Set the agentapi version
ARG AGENTAPI_VERSION=v0.11.6

# Clone and build agentapi from source
WORKDIR /agentapi-src
RUN set -ex && \
    echo "Building agentapi ${AGENTAPI_VERSION} from source for native architecture" && \
    git clone --depth 1 --branch ${AGENTAPI_VERSION} https://github.com/coder/agentapi.git . && \
    go mod download && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /agentapi . && \
    echo "Built agentapi binary info:" && \
    ls -la /agentapi

# Runtime stage
FROM debian:bookworm-slim

# Install ca-certificates, curl, and bash for mise installation, plus GitHub CLI, make, tmux, sudo, and Node.js
RUN apt-get update && apt-get install -y ca-certificates curl bash git python3 gcc make procps tmux sudo jq && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y gh && \
    curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - && \
    apt-get install -y nodejs && \
    rm -rf /var/lib/apt/lists/*

# Create a non-root user
RUN groupadd -r agentapi && useradd -r -g agentapi -d /home/agentapi -s /bin/bash agentapi && \
    mkdir -p /home/agentapi && \
    chown -R agentapi:agentapi /home/agentapi && \
    echo 'agentapi ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

# Set working directory
WORKDIR /home/agentapi/workdir

# Copy binary from builder stage (agentapi-proxy binary only)
COPY --from=builder /app/bin/agentapi-proxy /usr/local/bin/

# Copy agentapi binary from builder stage
COPY --from=agentapi-builder /agentapi /usr/local/bin/agentapi

# Copy github-mcp-server binary from official image
COPY --from=ghcr.io/github/github-mcp-server:v0.26.3 /server/github-mcp-server /usr/local/bin/

# Switch to non-root user
USER agentapi

# Configure global gitignore for .claude directory
RUN git config --global core.excludesfile ~/.gitignore_global && \
    echo ".claude/" > ~/.gitignore_global

# Set Go environment variables to use /home/agentapi directory
ENV GOPATH=/home/agentapi/go
ENV GOMODCACHE=/home/agentapi/go/pkg/mod
ENV GOCACHE=/home/agentapi/.cache/go-build

# Install mise
RUN curl https://mise.run | sh && \
    echo 'export PATH="/home/agentapi/.local/bin:/home/agentapi/.local/share/mise/shims:$PATH"' >> /home/agentapi/.bashrc

# Install claude code and move to /opt/claude for persistence across volume mounts
# The installer creates a symlink at ~/.local/bin/claude -> ~/.local/share/claude/versions/X.X.X
RUN curl -fsSL https://claude.ai/install.sh | bash && \
    sudo mkdir -p /opt/claude/bin && \
    sudo cp -rL /home/agentapi/.local/share/claude /opt/claude/share && \
    CLAUDE_VERSION=$(ls /opt/claude/share/versions/ | head -1) && \
    sudo ln -sf /opt/claude/share/versions/${CLAUDE_VERSION} /opt/claude/bin/claude && \
    sudo chmod +x /opt/claude/bin/claude

# Install Playwright MCP server via npm (Node.js is now installed directly)
RUN sudo npm install -g @playwright/mcp@latest

# Install uv for Python package management (enables uvx)
RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    echo 'export PATH="/home/agentapi/.cargo/bin:$PATH"' >> /home/agentapi/.bashrc

# Set combined PATH environment variable (including /opt/claude/bin for claude CLI)
ENV PATH="/opt/claude/bin:/home/agentapi/.cargo/bin:/home/agentapi/.local/bin:/home/agentapi/.local/share/mise/shims:$PATH"

# Set default CLAUDE_MD_PATH for Docker environment
ENV CLAUDE_MD_PATH=/tmp/config/CLAUDE.md

# Copy CLAUDE.md to temporary location for entrypoint script
COPY config/CLAUDE.md /tmp/config/CLAUDE.md
COPY config/managed-settings.json /etc/claude-code/managed-settings.json

# Copy entrypoint script
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sudo chmod +x /usr/local/bin/entrypoint.sh

# Expose port
EXPOSE 8080

# Run the application with entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["mise", "exec", "--", "agentapi-proxy", "server"]
