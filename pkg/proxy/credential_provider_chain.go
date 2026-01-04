package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// ChainCredentialProvider tries multiple providers in order until one succeeds
type ChainCredentialProvider = services.ChainCredentialProvider

// NewChainCredentialProvider creates a new ChainCredentialProvider
var NewChainCredentialProvider = services.NewChainCredentialProvider

// DefaultCredentialProvider returns the default credential provider chain
var DefaultCredentialProvider = services.DefaultCredentialProvider
