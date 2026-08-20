package personal_api_key

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// GetOrCreatePersonalAPIKeyUseCase handles getting or creating a personal API key for a user
type GetOrCreatePersonalAPIKeyUseCase struct {
	apiKeyRepo repositories.PersonalAPIKeyRepository
}

// NewGetOrCreatePersonalAPIKeyUseCase creates a new GetOrCreatePersonalAPIKeyUseCase
func NewGetOrCreatePersonalAPIKeyUseCase(
	apiKeyRepo repositories.PersonalAPIKeyRepository,
) *GetOrCreatePersonalAPIKeyUseCase {
	return &GetOrCreatePersonalAPIKeyUseCase{
		apiKeyRepo: apiKeyRepo,
	}
}

// Execute gets or creates a personal API key for the specified user
func (uc *GetOrCreatePersonalAPIKeyUseCase) Execute(ctx context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error) {
	// Try to find existing personal API key
	apiKey, err := uc.apiKeyRepo.FindByUserID(ctx, userID)
	if err == nil && apiKey != nil {
		// API key already exists
		return apiKey, nil
	}

	// API key doesn't exist, create new one
	generatedKey, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	apiKey = entities.NewPersonalAPIKey(userID, generatedKey)

	// Save to repository
	if err := uc.apiKeyRepo.Save(ctx, apiKey); err != nil {
		return nil, fmt.Errorf("failed to save personal API key for user %s: %w", userID, err)
	}

	return apiKey, nil
}

// generateAPIKey generates a random API key
func generateAPIKey() (string, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", err
	}
	return "ap_" + hex.EncodeToString(keyBytes), nil
}
