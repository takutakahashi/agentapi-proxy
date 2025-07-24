# Build stage
FROM golang:1.23-alpine AS builder

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
FROM golang:1.23-alpine AS agentapi-builder

# Install git for cloning
RUN apk add --no-cache git

# Set the agentapi version
ARG AGENTAPI_VERSION=v0.2.1

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
RUN apt-get update && apt-get install -y ca-certificates curl bash git python3 gcc make procps tmux sudo && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y gh && \
    curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - && \
    apt-get install -y nodejs && \
    rm -rf /var/lib/apt/lists/*

# Install Lightpanda Browser
RUN curl -L -o /usr/local/bin/lightpanda https://github.com/lightpanda-io/browser/releases/download/nightly/lightpanda-x86_64-linux && \
    chmod +x /usr/local/bin/lightpanda

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
ENV PATH="/home/agentapi/.local/bin:/home/agentapi/.local/share/mise/shims:$PATH"

# Install claude code and Playwright MCP server via npm (Node.js is now installed directly)
RUN sudo npm install -g @anthropic-ai/claude-code @playwright/mcp@latest

# Setup Lightpanda Browser
ENV LIGHTPANDA_BIN=/usr/local/bin/lightpanda
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

# Set default CLAUDE_MD_PATH for Docker environment
ENV CLAUDE_MD_PATH=/tmp/config/CLAUDE.md

# Copy CLAUDE.md to temporary location for entrypoint script
COPY config/CLAUDE.md /tmp/config/CLAUDE.md

# Copy entrypoint script
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sudo chmod +x /usr/local/bin/entrypoint.sh

# Expose port
EXPOSE 8080

# Run the application with entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["mise", "exec", "--", "agentapi-proxy", "server"]
