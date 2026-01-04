package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// KubernetesSubscriptionSecretSyncer is an alias to the internal implementation
type KubernetesSubscriptionSecretSyncer = services.KubernetesSubscriptionSecretSyncer

// NewKubernetesSubscriptionSecretSyncer creates a new KubernetesSubscriptionSecretSyncer
var NewKubernetesSubscriptionSecretSyncer = services.NewKubernetesSubscriptionSecretSyncer
