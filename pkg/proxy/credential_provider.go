package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// ClaudeCredentials represents Claude authentication credentials
type ClaudeCredentials = services.ClaudeCredentials

// CredentialProvider is an interface for loading Claude credentials from various sources
type CredentialProvider = services.CredentialProvider
