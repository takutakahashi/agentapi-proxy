package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

const (
	// Environment variable names for Claude credentials
	EnvClaudeAccessToken  = services.EnvClaudeAccessToken
	EnvClaudeRefreshToken = services.EnvClaudeRefreshToken
	EnvClaudeExpiresAt    = services.EnvClaudeExpiresAt
)

// EnvCredentialProvider loads credentials from environment variables
type EnvCredentialProvider = services.EnvCredentialProvider

// NewEnvCredentialProvider creates a new EnvCredentialProvider
var NewEnvCredentialProvider = services.NewEnvCredentialProvider
