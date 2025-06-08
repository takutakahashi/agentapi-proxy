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

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S agentapi && \
    adduser -u 1001 -S agentapi -G agentapi

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/bin/agentapi-proxy .

# Copy config example (optional)
COPY config.json.example ./config.json.example

# Change ownership to non-root user
RUN chown -R agentapi:agentapi /app

# Switch to non-root user
USER agentapi

# Expose port
EXPOSE 8080

# Run the application
CMD ["./agentapi-proxy", "server"]