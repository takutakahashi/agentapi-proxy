package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// KubernetesSessionManager is an alias to the internal implementation
type KubernetesSessionManager = services.KubernetesSessionManager

// NewKubernetesSessionManager creates a new KubernetesSessionManager
var NewKubernetesSessionManager = services.NewKubernetesSessionManager

// NewKubernetesSessionManagerWithClient creates a new KubernetesSessionManager with a custom client
var NewKubernetesSessionManagerWithClient = services.NewKubernetesSessionManagerWithClient
