package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
)

// migrationSourcePersonal and migrationSourceTeam identify the legacy stores a
// token was migrated from. They are recorded as annotations on the new Secret.
const (
	migrationSourcePersonal = "personal-api-key"
	migrationSourceTeam     = "team-config"
)

// defaultMigrationPermissions are the permissions assigned to migrated legacy
// personal API keys. They mirror the behavior of the legacy
// SimpleAuthService.LoadPersonalAPIKey, which granted session CRUD.
var defaultMigrationPermissions = []entities.Permission{
	entities.PermissionSessionCreate,
	entities.PermissionSessionRead,
	entities.PermissionSessionUpdate,
	entities.PermissionSessionDelete,
}

// APITokenAnnotator can apply provenance annotations to a migrated token.
// The concrete Kubernetes repository implements it; other implementations
// may no-op. Using an interface keeps the services package from depending on
// the concrete repositories package.
type APITokenAnnotator interface {
	ApplyMigrationAnnotations(ctx context.Context, tokenID, source, sourceID string) error
}

// MigrateAPITokens idempotently migrates legacy API key material into the new
// APITokenRepository without changing any plaintext token values, and without
// deleting the legacy data. It is safe to run repeatedly and on a partially
// migrated cluster.
//
// Migration sources:
//  1. Legacy personal API key Secrets (label agentapi.proxy/personal-api-key=true)
//     → personal APIToken with deterministic id tok_migrate-<userID>.
//  2. Legacy TeamConfig service-account keys → team APIToken with
//     deterministic id tok_migrate-<sanitized-teamID>.
//
// For each legacy item the migration uses a deterministic token id. If a new
// token with that id already exists with the SAME secret, the migration is a
// no-op for that item (already migrated). If it exists with a DIFFERENT secret
// the migration fails safely: it logs a warning, leaves both the legacy and
// new records untouched, and continues with the next item. The application
// continues authenticating legacy token strings after migration because the
// migrated token's secret is the legacy plaintext value, which is loaded into
// the in-memory auth map.
func MigrateAPITokens(
	ctx context.Context,
	authService *SimpleAuthService,
	tokenRepo repositories.APITokenRepository,
	personalAPIKeyRepo repositories.PersonalAPIKeyRepository,
	teamConfigRepo repositories.TeamConfigRepository,
	annotator APITokenAnnotator,
) error {
	log.Println("[MIGRATE] starting API token migration...")
	personalCount, err := migratePersonalKeys(ctx, authService, tokenRepo, personalAPIKeyRepo, annotator)
	if err != nil {
		return fmt.Errorf("personal api key migration failed: %w", err)
	}
	teamCount, err := migrateTeamServiceAccounts(ctx, authService, tokenRepo, teamConfigRepo, annotator)
	if err != nil {
		return fmt.Errorf("team service account migration failed: %w", err)
	}
	log.Printf("[MIGRATE] api token migration completed: %d personal, %d team migrated", personalCount, teamCount)
	return nil
}

func migratePersonalKeys(
	ctx context.Context,
	authService *SimpleAuthService,
	tokenRepo repositories.APITokenRepository,
	personalAPIKeyRepo repositories.PersonalAPIKeyRepository,
	annotator APITokenAnnotator,
) (int, error) {
	if personalAPIKeyRepo == nil || tokenRepo == nil {
		return 0, nil
	}
	legacy, err := personalAPIKeyRepo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list legacy personal api keys: %w", err)
	}
	count := 0
	for _, pk := range legacy {
		userID := pk.UserID()
		secret := pk.APIKey()
		if userID == "" || secret == "" {
			log.Printf("[MIGRATE] skipping legacy personal key with empty user/secret")
			continue
		}
		tokenID := apitoken.MigrationTokenID("personal-" + string(userID))
		token := buildMigrationPersonalToken(tokenID, secret, userID, pk.CreatedAt(), pk.UpdatedAt())

		created, err := idempotentCreate(ctx, tokenRepo, token)
		if err != nil {
			log.Printf("[MIGRATE] warning: failed to migrate personal key for user %s: %v", userID, err)
			continue
		}
		if created {
			count++
		}
		if annotator != nil {
			if err := annotator.ApplyMigrationAnnotations(ctx, tokenID, migrationSourcePersonal, string(userID)); err != nil {
				log.Printf("[MIGRATE] warning: failed to annotate migrated personal token %s: %v", tokenID, err)
			}
		}
		if authService != nil {
			if err := authService.LoadAPIToken(ctx, token); err != nil {
				log.Printf("[MIGRATE] warning: failed to load migrated personal token for user %s: %v", userID, err)
			}
		}
	}
	return count, nil
}

func migrateTeamServiceAccounts(
	ctx context.Context,
	authService *SimpleAuthService,
	tokenRepo repositories.APITokenRepository,
	teamConfigRepo repositories.TeamConfigRepository,
	annotator APITokenAnnotator,
) (int, error) {
	if teamConfigRepo == nil || tokenRepo == nil {
		return 0, nil
	}
	teamConfigs, err := teamConfigRepo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list legacy team configs: %w", err)
	}
	count := 0
	for _, tc := range teamConfigs {
		sa := tc.ServiceAccount()
		if sa == nil {
			continue
		}
		teamID := tc.TeamID()
		secret := sa.APIKey()
		if teamID == "" || secret == "" {
			log.Printf("[MIGRATE] skipping legacy team config %s with empty team/secret", teamID)
			continue
		}
		tokenID := apitoken.MigrationTokenID("team-" + teamID)
		perms := sa.Permissions()
		if len(perms) == 0 {
			perms = defaultMigrationPermissions
		}
		token := buildMigrationTeamToken(tokenID, secret, teamID, sa.UserID(), perms, sa.CreatedAt(), sa.UpdatedAt())

		created, err := idempotentCreate(ctx, tokenRepo, token)
		if err != nil {
			log.Printf("[MIGRATE] warning: failed to migrate team service account for team %s: %v", teamID, err)
			continue
		}
		if created {
			count++
		}
		if annotator != nil {
			if err := annotator.ApplyMigrationAnnotations(ctx, tokenID, migrationSourceTeam, teamID); err != nil {
				log.Printf("[MIGRATE] warning: failed to annotate migrated team token %s: %v", tokenID, err)
			}
		}
		if authService != nil {
			if err := authService.LoadAPIToken(ctx, token); err != nil {
				log.Printf("[MIGRATE] warning: failed to load migrated team token for team %s: %v", teamID, err)
			}
		}
	}
	return count, nil
}

func buildMigrationPersonalToken(id, secret string, userID entities.UserID, createdAt, updatedAt time.Time) *entities.APIToken {
	return entities.RestoreAPIToken(
		id, secret, apitoken.DisplayPrefix(secret), "migrated",
		entities.APITokenScopeUser, userID, "",
		defaultMigrationPermissions, nil, userID,
		createdAt, updatedAt,
	)
}

func buildMigrationTeamToken(id, secret, teamID string, userID entities.UserID, perms []entities.Permission, createdAt, updatedAt time.Time) *entities.APIToken {
	return entities.RestoreAPIToken(
		id, secret, apitoken.DisplayPrefix(secret), "migrated",
		entities.APITokenScopeTeam, userID, teamID,
		perms, nil, userID,
		createdAt, updatedAt,
	)
}

// idempotentCreate creates a token, treating AlreadyExists as already
// migrated only when the stored secret matches. On a secret mismatch it fails
// safely (returns an error) without overwriting either record.
func idempotentCreate(ctx context.Context, repo repositories.APITokenRepository, token *entities.APIToken) (bool, error) {
	err := repo.Create(ctx, token)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, entities.ErrAPITokenAlreadyExists) {
		return false, err
	}
	existing, getErr := repo.GetByID(ctx, token.ID())
	if getErr != nil {
		return false, fmt.Errorf("existing migrated token could not be read: %w", getErr)
	}
	if existing.Secret() != token.Secret() {
		return false, fmt.Errorf("migration id %s already in use with a different secret; refusing to overwrite", token.ID())
	}
	return false, nil // already migrated
}
