package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// BootstrapPersonalAPIKeys loads existing personal API keys from Kubernetes into auth service
func BootstrapPersonalAPIKeys(
	ctx context.Context,
	authService *SimpleAuthService,
	personalAPIKeyRepo repositories.PersonalAPIKeyRepository,
) error {
	log.Println("Starting personal API key bootstrap...")

	// List all personal API keys from Kubernetes
	personalAPIKeys, err := personalAPIKeyRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list personal API keys: %w", err)
	}

	log.Printf("Found %d personal API key(s)", len(personalAPIKeys))

	loadedCount := 0

	for _, apiKey := range personalAPIKeys {
		userID := apiKey.UserID()

		// Load personal API key into auth service memory
		if err := authService.LoadPersonalAPIKey(ctx, apiKey); err != nil {
			log.Printf("Warning: failed to load personal API key for user %s: %v", userID, err)
			continue
		}
		log.Printf("Loaded personal API key for user: %s", userID)
		loadedCount++
	}

	log.Printf("Personal API key bootstrap completed: %d loaded", loadedCount)
	return nil
}
