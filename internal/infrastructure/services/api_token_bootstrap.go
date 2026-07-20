package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// BootstrapAPITokens loads all named API tokens from the repository into the
// in-memory auth service so they authenticate immediately after startup. It
// is run after MigrateAPITokens so freshly migrated tokens are also loaded
// (loading them twice is harmless).
func BootstrapAPITokens(
	ctx context.Context,
	authService *SimpleAuthService,
	tokenRepo repositories.APITokenRepository,
) error {
	if authService == nil || tokenRepo == nil {
		return nil
	}
	tokens, err := tokenRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list api tokens: %w", err)
	}
	loaded := 0
	for _, token := range tokens {
		if err := authService.LoadAPIToken(ctx, token); err != nil {
			log.Printf("[BOOTSTRAP] warning: failed to load api token %s: %v", token.ID(), err)
			continue
		}
		loaded++
	}
	log.Printf("[BOOTSTRAP] loaded %d named api token(s)", loaded)
	return nil
}
