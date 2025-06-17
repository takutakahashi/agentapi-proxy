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

# Download agentapi binary stage
FROM alpine:latest AS agentapi-downloader

# Install curl
RUN apk add --no-cache curl

# Set the agentapi version
ARG AGENTAPI_VERSION=v0.2.1

# Set target platform arguments
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Download the appropriate agentapi binary
RUN set -ex && \
    if [ "${TARGETOS}" = "windows" ]; then \
        BINARY_NAME="agentapi-${TARGETOS}-${TARGETARCH}.exe"; \
    else \
        BINARY_NAME="agentapi-${TARGETOS}-${TARGETARCH}"; \
    fi && \
    DOWNLOAD_URL="https://github.com/coder/agentapi/releases/download/${AGENTAPI_VERSION}/${BINARY_NAME}" && \
    echo "Downloading agentapi from: ${DOWNLOAD_URL}" && \
    curl -fsSL "${DOWNLOAD_URL}" -o /agentapi && \
    chmod +x /agentapi

# Runtime stage
FROM ubuntu

# Install ca-certificates, curl, and bash for mise installation
RUN apt update && apt install -y ca-certificates curl bash git python3 gcc

# Create non-root user
RUN groupadd -g 1001 agentapi && \
    useradd -u 1001 -g agentapi -m -s /bin/bash agentapi

# Set working directory
WORKDIR /app

# Copy binary from builder stage (agentapi-proxy binary only)
COPY --from=builder /app/bin/agentapi-proxy /usr/local/bin/

# Copy agentapi binary from downloader stage
COPY --from=agentapi-downloader /agentapi /usr/local/bin/agentapi

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


# Expose port
EXPOSE 8080

# Run the application
CMD ["mise", "exec", "--", "agentapi-proxy", "server"]
