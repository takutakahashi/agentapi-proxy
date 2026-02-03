# Code Style and Conventions

## Go Conventions
- Follow standard Go formatting (enforced by `gofmt`)
- Use meaningful variable names
- Add comments to exported functions and types
- Group related constants and types together

## Project Structure
- Clean Architecture pattern with domain/infrastructure/usecase/interface layers
- Domain entities in `internal/domain/entities`
- Infrastructure implementations in `internal/infrastructure`
- Use cases in `internal/usecases`
- Controllers in `internal/interfaces/controllers`

## Logging
- Use structured logging via `pkg/logger`
- Prefix log messages with component name (e.g., `[K8S_SESSION]`)
- Log at appropriate levels (info, warning, error)

## Error Handling
- Wrap errors with context using `fmt.Errorf`
- Return errors rather than logging and continuing when appropriate
- Log warnings for non-critical errors

## Testing
- Place tests alongside source files (*_test.go)
- Use table-driven tests where appropriate
- Use testify/assert for assertions
- Mock Kubernetes clients for infrastructure tests

## Kubernetes Resources
- Use consistent naming: `agentapi-session-{id}` for deployments
- Use labels for queryable metadata
- Use annotations for non-queryable metadata
- Clean up resources on deletion