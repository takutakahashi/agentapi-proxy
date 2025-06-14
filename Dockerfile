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
RUN go build -o bin/agentapi-proxy ./cmd/agentapi-proxy

# Runtime stage
FROM alpine:latest

# Install ca-certificates, curl, and bash for mise installation
RUN apk --no-cache add ca-certificates curl bash git

# Install mise
RUN curl https://mise.run | sh
ENV PATH="/root/.local/bin:$PATH"

# Install Node.js via mise
RUN mise install node@latest
RUN mise global node@latest

# Install claude code via npm
RUN eval "$(mise activate bash)" && npm install -g @anthropic-ai/claude-code

# Create non-root user
RUN addgroup -g 1001 -S agentapi && \
    adduser -u 1001 -S agentapi -G agentapi

# Set working directory
WORKDIR /app

# Copy binary from builder stage (agentapi-proxy binary only)
COPY --from=builder /app/bin/agentapi-proxy .

# Change ownership to non-root user
RUN chown -R agentapi:agentapi /app

# Switch to non-root user
USER agentapi

# Expose port
EXPOSE 8080

# Run the application
CMD ["./agentapi-proxy", "server"]