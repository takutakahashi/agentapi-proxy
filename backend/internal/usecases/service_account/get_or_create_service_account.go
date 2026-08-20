package service_account

import (
	"context"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// AuthServiceForServiceAccount defines the interface for auth service methods needed by this use case
type AuthServiceForServiceAccount interface {
	CreateServiceAccountForTeam(ctx context.Context, teamID string, teamConfigRepo repositories.TeamConfigRepository) (*entities.User, *entities.ServiceAccount, error)
	LoadServiceAccountFromTeamConfig(ctx context.Context, teamConfig *entities.TeamConfig) error
}

// GetOrCreateServiceAccountUseCase handles getting or creating a service account for a team
type GetOrCreateServiceAccountUseCase struct {
	teamConfigRepo repositories.TeamConfigRepository
	authService    AuthServiceForServiceAccount
}

// NewGetOrCreateServiceAccountUseCase creates a new GetOrCreateServiceAccountUseCase
func NewGetOrCreateServiceAccountUseCase(
	teamConfigRepo repositories.TeamConfigRepository,
	authService AuthServiceForServiceAccount,
) *GetOrCreateServiceAccountUseCase {
	return &GetOrCreateServiceAccountUseCase{
		teamConfigRepo: teamConfigRepo,
		authService:    authService,
	}
}

// EnsureServiceAccount ensures a service account exists for the specified team.
// It implements the services.ServiceAccountEnsurer interface.
func (uc *GetOrCreateServiceAccountUseCase) EnsureServiceAccount(ctx context.Context, teamID string) error {
	_, err := uc.Execute(ctx, teamID)
	return err
}

// Execute gets or creates a service account for the specified team
func (uc *GetOrCreateServiceAccountUseCase) Execute(ctx context.Context, teamID string) (*entities.ServiceAccount, error) {
	// Try to find existing team config
	teamConfig, err := uc.teamConfigRepo.FindByTeamID(ctx, teamID)
	if err == nil && teamConfig != nil && teamConfig.ServiceAccount() != nil {
		// Service account already exists
		return teamConfig.ServiceAccount(), nil
	}

	// Service account doesn't exist, create new one
	_, serviceAccount, err := uc.authService.CreateServiceAccountForTeam(ctx, teamID, uc.teamConfigRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create service account for team %s: %w", teamID, err)
	}

	return serviceAccount, nil
}
