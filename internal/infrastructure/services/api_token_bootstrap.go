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
//
// Any per-token load failure is propagated so the caller can prevent serving
// traffic rather than silently running with a partially loaded auth map.
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
		if token == nil {
			return fmt.Errorf("failed to load api token: token is nil")
		}
		if err := authService.LoadAPIToken(ctx, token); err != nil {
			return fmt.Errorf("failed to load api token %s: %w", token.ID(), err)
		}
		loaded++
	}
	log.Printf("[BOOTSTRAP] loaded %d named api token(s)", loaded)
	return nil
}
