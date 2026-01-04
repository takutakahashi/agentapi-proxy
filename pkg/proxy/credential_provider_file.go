package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// FileCredentialProvider loads credentials from user-specific credential files
type FileCredentialProvider = services.FileCredentialProvider

// NewFileCredentialProvider creates a new FileCredentialProvider with default path
var NewFileCredentialProvider = services.NewFileCredentialProvider

// NewFileCredentialProviderWithPath creates a new FileCredentialProvider with custom path
var NewFileCredentialProviderWithPath = services.NewFileCredentialProviderWithPath
