package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/repositories

const (
	// ShareConfigMapName is the name of the ConfigMap storing session shares
	ShareConfigMapName = repositories.ShareConfigMapName
	// ShareConfigMapDataKey is the key in the ConfigMap data
	ShareConfigMapDataKey = repositories.ShareConfigMapDataKey
	// LabelShare is the label key for share resources
	LabelShare = repositories.LabelShare
)

// KubernetesShareRepository implements ShareRepository using Kubernetes ConfigMap
type KubernetesShareRepository = repositories.KubernetesShareRepository

// NewKubernetesShareRepository creates a new KubernetesShareRepository
var NewKubernetesShareRepository = repositories.NewKubernetesShareRepository
