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

# Install ca-certificates, curl, and bash for mise installation, plus GitHub CLI and make
RUN apt-get update && apt-get install -y ca-certificates curl bash git python3 gcc make procps && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y gh && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1001 agentapi && \
    useradd -u 1001 -g agentapi -m -s /bin/bash agentapi

# Set working directory
WORKDIR /app

# Copy binary from builder stage (agentapi-proxy binary only)
COPY --from=builder /app/bin/agentapi-proxy /usr/local/bin/

# Copy agentapi binary from builder stage
COPY --from=agentapi-builder /agentapi /usr/local/bin/agentapi

# Change ownership to non-root user
RUN chown -R agentapi:agentapi /app

# Switch to non-root user
USER agentapi

# Install mise
RUN curl https://mise.run | sh
ENV PATH="/home/agentapi/.local/bin:$PATH"

# Install Node.js via mise
RUN mise install node@latest
RUN mise global node@latest

# Install claude code via npm
RUN mise exec -- npm install -g @anthropic-ai/claude-code

# Copy CLAUDE.md to user's home directory
COPY --chown=agentapi:agentapi config/CLAUDE.md /home/agentapi/.claude/CLAUDE.md

# Expose port
EXPOSE 8080

# Run the application
CMD ["mise", "exec", "--", "agentapi-proxy", "server"]
