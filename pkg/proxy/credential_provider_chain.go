package proxy

import (
	"fmt"
	"log"
)

// ChainCredentialProvider tries multiple providers in order until one succeeds
type ChainCredentialProvider struct {
	providers []CredentialProvider
}

// NewChainCredentialProvider creates a new ChainCredentialProvider
func NewChainCredentialProvider(providers ...CredentialProvider) *ChainCredentialProvider {
	return &ChainCredentialProvider{providers: providers}
}

// Name returns the provider name
func (p *ChainCredentialProvider) Name() string {
	return "chain"
}

// Load attempts to load credentials from each provider in order
// Returns the first successful result
// Returns nil, nil if all providers return nil
func (p *ChainCredentialProvider) Load() (*ClaudeCredentials, error) {
	var lastErr error

	for _, provider := range p.providers {
		creds, err := provider.Load()
		if err != nil {
			log.Printf("[CREDENTIAL_PROVIDER] Provider %s failed: %v", provider.Name(), err)
			lastErr = err
			continue
		}
		if creds != nil {
			log.Printf("[CREDENTIAL_PROVIDER] Loaded credentials from provider: %s", provider.Name())
			return creds, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}

	return nil, nil
}

// DefaultCredentialProvider returns the default credential provider chain
// Order: Environment variables (highest priority) -> File
func DefaultCredentialProvider() CredentialProvider {
	return NewChainCredentialProvider(
		NewEnvCredentialProvider(),
		NewFileCredentialProvider(),
	)
}
