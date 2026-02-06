package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// AuthServiceForBootstrap defines the interface for auth service methods needed by bootstrap
type AuthServiceForBootstrap interface {
	CreateServiceAccountForTeam(ctx context.Context, teamID string, teamConfigRepo repositories.TeamConfigRepository) error
	LoadServiceAccountFromTeamConfig(ctx context.Context, teamConfig interface{}) error
}

// BootstrapServiceAccounts loads existing service accounts from Kubernetes and creates missing ones
func BootstrapServiceAccounts(
	ctx context.Context,
	authService *SimpleAuthService,
	teamConfigRepo repositories.TeamConfigRepository,
) error {
	log.Println("Starting service account bootstrap...")

	// List all team configs from Kubernetes
	teamConfigs, err := teamConfigRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list team configs: %w", err)
	}

	log.Printf("Found %d team config(s)", len(teamConfigs))

	createdCount := 0
	loadedCount := 0

	for _, teamConfig := range teamConfigs {
		teamID := teamConfig.TeamID()

		// Check if service account exists in the team config
		if teamConfig.ServiceAccount() != nil {
			// Load existing service account into memory
			if err := authService.LoadServiceAccountFromTeamConfig(ctx, teamConfig); err != nil {
				log.Printf("Warning: failed to load service account for team %s: %v", teamID, err)
				continue
			}
			log.Printf("Loaded service account for team: %s", teamID)
			loadedCount++
		} else {
			// Create new service account for this team
			_, _, err := authService.CreateServiceAccountForTeam(ctx, teamID, teamConfigRepo)
			if err != nil {
				log.Printf("Warning: failed to create service account for team %s: %v", teamID, err)
				continue
			}
			log.Printf("Created service account for team: %s", teamID)
			createdCount++
		}
	}

	log.Printf("Service account bootstrap completed: %d loaded, %d created", loadedCount, createdCount)
	return nil
}
