package services

import (
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// EncryptionServiceRegistry manages multiple EncryptionService implementations
// and selects the appropriate one based on encryption metadata
type EncryptionServiceRegistry struct {
	// Primary service used for encryption
	primary services.EncryptionService

	// Map of registered services by algorithm
	// Key: algorithm (e.g., "noop", "aes-256-gcm", "aws-kms")
	servicesByAlgorithm map[string]services.EncryptionService

	// Map of registered services by (algorithm, keyID)
	// Key: "algorithm:keyID"
	servicesByAlgorithmAndKey map[string]services.EncryptionService
}

// NewEncryptionServiceRegistry creates a new registry
func NewEncryptionServiceRegistry(primary services.EncryptionService) *EncryptionServiceRegistry {
	registry := &EncryptionServiceRegistry{
		primary:                   primary,
		servicesByAlgorithm:       make(map[string]services.EncryptionService),
		servicesByAlgorithmAndKey: make(map[string]services.EncryptionService),
	}

	// Register the primary service
	if primary != nil {
		registry.Register(primary)
	}

	return registry
}

// Register adds an EncryptionService to the registry
func (r *EncryptionServiceRegistry) Register(service services.EncryptionService) {
	if service == nil {
		return
	}

	algorithm := service.Algorithm()
	keyID := service.KeyID()

	// Register by algorithm only
	if _, exists := r.servicesByAlgorithm[algorithm]; !exists {
		r.servicesByAlgorithm[algorithm] = service
		log.Printf("[ENCRYPTION_REGISTRY] Registered service for algorithm: %s", algorithm)
	}

	// Register by algorithm and keyID
	key := fmt.Sprintf("%s:%s", algorithm, keyID)
	r.servicesByAlgorithmAndKey[key] = service
	log.Printf("[ENCRYPTION_REGISTRY] Registered service for %s (keyID: %s)", algorithm, keyID)
}

// GetForEncryption returns the primary service used for encrypting new values
func (r *EncryptionServiceRegistry) GetForEncryption() services.EncryptionService {
	return r.primary
}

// GetForDecryption returns the appropriate service for decrypting based on metadata
// Falls back to primary if no matching service is found
func (r *EncryptionServiceRegistry) GetForDecryption(metadata services.EncryptionMetadata) services.EncryptionService {
	// Try exact match by algorithm and keyID
	key := fmt.Sprintf("%s:%s", metadata.Algorithm, metadata.KeyID)
	if service, exists := r.servicesByAlgorithmAndKey[key]; exists {
		return service
	}

	// Try match by algorithm only
	if service, exists := r.servicesByAlgorithm[metadata.Algorithm]; exists {
		log.Printf("[ENCRYPTION_REGISTRY] Using algorithm-only match for %s (keyID: %s)",
			metadata.Algorithm, metadata.KeyID)
		return service
	}

	// Fallback to primary
	log.Printf("[ENCRYPTION_REGISTRY] No matching service found for %s (keyID: %s), using primary",
		metadata.Algorithm, metadata.KeyID)
	return r.primary
}

// SetPrimary sets the primary encryption service
func (r *EncryptionServiceRegistry) SetPrimary(service services.EncryptionService) {
	r.primary = service
	if service != nil {
		r.Register(service)
	}
}
