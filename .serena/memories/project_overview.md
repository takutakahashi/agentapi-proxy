# agentapi-proxy Project Overview

## Purpose
A session-based proxy server for coder/agentapi that provides process provisioning and lifecycle management for multiple agentapi server instances running in Kubernetes.

## Tech Stack
- **Language**: Go 1.25.0
- **Framework**: Echo v4 (HTTP server)
- **Container Orchestration**: Kubernetes
- **Key Libraries**:
  - k8s.io/client-go: Kubernetes client
  - sigs.k8s.io/controller-runtime: Kubernetes controller runtime
  - github.com/spf13/cobra: CLI framework
  - github.com/spf13/viper: Configuration management
  - github.com/google/uuid: UUID generation

## Architecture
The proxy manages agentapi sessions as Kubernetes resources:
- Each session is backed by a Deployment, Service, and optionally a PVC
- Sessions are persisted in Kubernetes and can survive proxy restarts
- Session metadata is stored in Service labels and annotations
- KubernetesSessionManager handles session lifecycle

## Key Components
- **internal/domain/entities**: Domain entities (Session, User, etc.)
- **internal/infrastructure/services**: Kubernetes session management
- **internal/usecases**: Business logic
- **internal/interfaces/controllers**: HTTP API controllers
- **pkg/**: Reusable packages (config, logger, client, etc.)