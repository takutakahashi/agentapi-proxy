// Package api_token contains the use cases that implement the multi API token
// CRUD endpoints (list, create, get, delete). Authorization is enforced inside
// each use case so the HTTP controller stays thin.
package api_token

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
)

// maxIDRetries is the number of times Create retries with a freshly generated
// token ID when the repository reports a collision. With 128 bits of entropy
// this should never happen in practice, but the loop keeps creation
// collision-safe.
const maxIDRetries = 5

// AuthService provides the auth-side operations the use cases need: loading a
// newly created token so it authenticates immediately, and revoking a deleted
// token's secret from the in-memory auth map.
type AuthService interface {
	LoadAPIToken(ctx context.Context, token *entities.APIToken) error
	RevokeAPIToken(secret string)
}

// --- Create ---

// CreateAPITokenInput is the request to create a new token.
type CreateAPITokenInput struct {
	// Caller is the authenticated user creating the token.
	Caller *entities.User
	// Name is the human-readable token name (optional but recommended).
	Name string
	// Scope is "user" or "team".
	Scope string
	// TeamID is required when Scope == "team".
	TeamID string
	// Permissions is the requested permission set; each must be granted by
	// the caller (no broader than caller).
	Permissions []entities.Permission
	// ExpiresAt is the optional expiration time.
	ExpiresAt *time.Time
}

// CreateAPITokenOutput is the result of a successful creation. It includes the
// plaintext secret, which is returned exactly once to the caller.
type CreateAPITokenOutput struct {
	Token *entities.APIToken
}

// CreateAPITokenUseCase creates new named API tokens.
type CreateAPITokenUseCase struct {
	repo        repositories.APITokenRepository
	authService AuthService
}

// NewCreateAPITokenUseCase constructs a CreateAPITokenUseCase.
func NewCreateAPITokenUseCase(repo repositories.APITokenRepository, authService AuthService) *CreateAPITokenUseCase {
	return &CreateAPITokenUseCase{repo: repo, authService: authService}
}

// Execute creates the token.
func (uc *CreateAPITokenUseCase) Execute(ctx context.Context, in *CreateAPITokenInput) (*CreateAPITokenOutput, error) {
	if in == nil || in.Caller == nil {
		return nil, errors.New("caller is required")
	}

	scope, err := resolveScope(in.Caller, in.Scope, in.TeamID)
	if err != nil {
		return nil, err
	}
	teamID := ""
	if scope == string(entities.APITokenScopeTeam) {
		teamID = in.TeamID
	}

	// Authorization.
	if err := uc.authorizeCreate(in.Caller, scope, teamID); err != nil {
		return nil, err
	}

	// Permissions must be a subset of the caller's permissions.
	if err := validatePermissionsSubset(in.Caller, in.Permissions); err != nil {
		return nil, err
	}
	if len(in.Permissions) == 0 {
		return nil, errors.New("permissions cannot be empty")
	}

	// Identity the token authenticates as.
	var userID entities.UserID
	if scope == string(entities.APITokenScopeTeam) {
		userID = serviceAccountUserID(teamID)
	} else {
		userID = in.Caller.ID()
	}

	createdBy := in.Caller.ID()

	// Generate ID + secret with collision-safe retry.
	var token *entities.APIToken
	for attempt := 0; attempt < maxIDRetries; attempt++ {
		id, err := apitoken.GenerateTokenID()
		if err != nil {
			return nil, fmt.Errorf("failed to generate token id: %w", err)
		}
		secret, err := apitoken.GenerateSecret()
		if err != nil {
			return nil, fmt.Errorf("failed to generate token secret: %w", err)
		}
		token = entities.NewAPIToken(
			id, secret, apitoken.DisplayPrefix(secret), in.Name,
			entities.APITokenScope(scope), userID, teamID,
			in.Permissions, in.ExpiresAt, createdBy,
		)
		if err := uc.repo.Create(ctx, token); err != nil {
			if errors.Is(err, entities.ErrAPITokenAlreadyExists) {
				continue // extremely unlikely; retry with a new ID
			}
			return nil, fmt.Errorf("failed to persist api token: %w", err)
		}
		break
	}
	if token == nil {
		return nil, errors.New("failed to generate a unique token id after retries")
	}

	// Load into auth service for immediate authentication.
	if uc.authService != nil {
		if err := uc.authService.LoadAPIToken(ctx, token); err != nil {
			// Best-effort: the token is persisted; it will be loaded on the
			// next bootstrap. Log via the returned error.
			return &CreateAPITokenOutput{Token: token}, fmt.Errorf("token persisted but failed to load into auth service: %w", err)
		}
	}

	return &CreateAPITokenOutput{Token: token}, nil
}

// authorizeCreate enforces scope-based creation authorization.
func (uc *CreateAPITokenUseCase) authorizeCreate(caller *entities.User, scope, teamID string) error {
	if scope == string(entities.APITokenScopeUser) {
		// Any authenticated (non-service-account) user may create personal
		// tokens. Service accounts are team-scoped and cannot own personal
		// tokens.
		if caller.UserType() == entities.UserTypeServiceAccount {
			return errors.New("service accounts cannot create personal api tokens")
		}
		return nil
	}
	// Team scope: caller must be a member of the team (admins qualify).
	if teamID == "" {
		return errors.New("team_id is required for team-scoped tokens")
	}
	if !canAccessTeam(caller, teamID) {
		return errors.New("caller is not a member of the team")
	}
	return nil
}

// --- List ---

// ListAPITokenInput is the request to list tokens.
type ListAPITokenInput struct {
	Caller *entities.User
	Scope  string // "" | "user" | "team"
	TeamID string
}

// ListAPITokenOutput is the list result (tokens without secrets are produced
// by the controller; the entities here still carry secrets and must not be
// serialized directly).
type ListAPITokenOutput struct {
	Tokens []*entities.APIToken
}

// ListAPITokenUseCase lists API tokens the caller is allowed to see.
type ListAPITokenUseCase struct {
	repo repositories.APITokenRepository
}

// NewListAPITokenUseCase constructs a ListAPITokenUseCase.
func NewListAPITokenUseCase(repo repositories.APITokenRepository) *ListAPITokenUseCase {
	return &ListAPITokenUseCase{repo: repo}
}

// Execute lists the tokens.
func (uc *ListAPITokenUseCase) Execute(ctx context.Context, in *ListAPITokenInput) (*ListAPITokenOutput, error) {
	if in == nil || in.Caller == nil {
		return nil, errors.New("caller is required")
	}

	scope, err := resolveScope(in.Caller, in.Scope, in.TeamID)
	if err != nil {
		return nil, err
	}

	switch scope {
	case string(entities.APITokenScopeUser):
		tokens, err := uc.repo.ListByOwner(ctx, in.Caller.ID())
		if err != nil {
			return nil, fmt.Errorf("failed to list personal tokens: %w", err)
		}
		return &ListAPITokenOutput{Tokens: tokens}, nil
	case string(entities.APITokenScopeTeam):
		if in.TeamID == "" {
			return nil, errors.New("team_id is required for team-scoped tokens")
		}
		if !canAccessTeam(in.Caller, in.TeamID) {
			return nil, errors.New("caller is not a member of the team")
		}
		tokens, err := uc.repo.ListByTeam(ctx, in.TeamID)
		if err != nil {
			return nil, fmt.Errorf("failed to list team tokens: %w", err)
		}
		return &ListAPITokenOutput{Tokens: tokens}, nil
	default:
		// No scope filter: return own personal tokens plus tokens for every
		// team the caller belongs to.
		personal, err := uc.repo.ListByOwner(ctx, in.Caller.ID())
		if err != nil {
			return nil, fmt.Errorf("failed to list personal tokens: %w", err)
		}
		out := append([]*entities.APIToken{}, personal...)
		for _, teamID := range callerTeamIDs(in.Caller) {
			teamTokens, err := uc.repo.ListByTeam(ctx, teamID)
			if err != nil {
				return nil, fmt.Errorf("failed to list team tokens for %s: %w", teamID, err)
			}
			out = append(out, teamTokens...)
		}
		return &ListAPITokenOutput{Tokens: out}, nil
	}
}

// --- Get ---

// GetAPITokenInput is the request to get a single token's metadata.
type GetAPITokenInput struct {
	Caller  *entities.User
	TokenID string
}

// GetAPITokenOutput is the get result.
type GetAPITokenOutput struct {
	Token *entities.APIToken
}

// GetAPITokenUseCase fetches a single token's metadata.
type GetAPITokenUseCase struct {
	repo repositories.APITokenRepository
}

// NewGetAPITokenUseCase constructs a GetAPITokenUseCase.
func NewGetAPITokenUseCase(repo repositories.APITokenRepository) *GetAPITokenUseCase {
	return &GetAPITokenUseCase{repo: repo}
}

// Execute fetches the token. It returns entities.ErrAPITokenNotFound both when
// the token does not exist and when the caller is not authorized to see it, so
// the HTTP layer can return a uniform 404 without leaking cross-scope
// existence.
func (uc *GetAPITokenUseCase) Execute(ctx context.Context, in *GetAPITokenInput) (*GetAPITokenOutput, error) {
	if in == nil || in.Caller == nil {
		return nil, errors.New("caller is required")
	}
	if in.TokenID == "" {
		return nil, errors.New("token_id is required")
	}
	token, err := uc.repo.GetByID(ctx, in.TokenID)
	if err != nil {
		if errors.Is(err, entities.ErrAPITokenNotFound) {
			return nil, entities.ErrAPITokenNotFound
		}
		return nil, fmt.Errorf("failed to get api token: %w", err)
	}
	if !canAccessToken(in.Caller, token) {
		// Do not leak existence across scopes.
		return nil, entities.ErrAPITokenNotFound
	}
	return &GetAPITokenOutput{Token: token}, nil
}

// --- Delete ---

// DeleteAPITokenInput is the request to delete a token.
type DeleteAPITokenInput struct {
	Caller  *entities.User
	TokenID string
}

// DeleteAPITokenUseCase deletes tokens with creator-or-admin authorization for
// team tokens and owner-only authorization for personal tokens. It is
// idempotent: deleting a non-existent or not-authorized token returns nil so
// the HTTP layer can answer 204 without leaking cross-scope existence.
type DeleteAPITokenUseCase struct {
	repo        repositories.APITokenRepository
	authService AuthService
}

// NewDeleteAPITokenUseCase constructs a DeleteAPITokenUseCase.
func NewDeleteAPITokenUseCase(repo repositories.APITokenRepository, authService AuthService) *DeleteAPITokenUseCase {
	return &DeleteAPITokenUseCase{repo: repo, authService: authService}
}

// Execute deletes the token.
func (uc *DeleteAPITokenUseCase) Execute(ctx context.Context, in *DeleteAPITokenInput) error {
	if in == nil || in.Caller == nil {
		return errors.New("caller is required")
	}
	if in.TokenID == "" {
		return errors.New("token_id is required")
	}
	token, err := uc.repo.GetByID(ctx, in.TokenID)
	if err != nil {
		if errors.Is(err, entities.ErrAPITokenNotFound) {
			// Idempotent: a nonexistent token is "deleted" from the caller's
			// perspective. We do not leak whether it existed in another scope.
			return nil
		}
		return fmt.Errorf("failed to get api token: %w", err)
	}

	if !canDeleteToken(in.Caller, token) {
		// Not authorized to delete. Return nil (idempotent, no leak) without
		// actually deleting or revoking.
		return nil
	}

	if err := uc.repo.Delete(ctx, in.TokenID); err != nil {
		return fmt.Errorf("failed to delete api token: %w", err)
	}
	// Immediate revocation: remove the secret from the in-memory auth map so
	// the token stops authenticating right away, before any cache TTL.
	if uc.authService != nil {
		uc.authService.RevokeAPIToken(token.Secret())
	}
	return nil
}

// --- helpers ---

// resolveScope normalizes the requested scope for the caller. Service
// accounts are forced to team scope (their own team); for everyone else the
// requested scope is validated.
func resolveScope(caller *entities.User, scope, teamID string) (string, error) {
	if caller.UserType() == entities.UserTypeServiceAccount {
		if caller.TeamID() == "" {
			return "", errors.New("service account has no team")
		}
		return string(entities.APITokenScopeTeam), nil
	}
	switch entities.APITokenScope(scope) {
	case "", entities.APITokenScopeUser:
		return string(entities.APITokenScopeUser), nil
	case entities.APITokenScopeTeam:
		return string(entities.APITokenScopeTeam), nil
	default:
		return "", fmt.Errorf("invalid scope %q: must be 'user' or 'team'", scope)
	}
}

// validatePermissionsSubset ensures every requested permission is granted by
// the caller, so a token can never be broader than its creator.
func validatePermissionsSubset(caller *entities.User, requested []entities.Permission) error {
	for _, p := range requested {
		if !caller.HasPermission(p) {
			return fmt.Errorf("requested permission %q is not granted to the caller", p)
		}
	}
	return nil
}

// canAccessTeam reports whether the caller may access a team's resources:
// admins qualify, as do team members (GitHub teams) and team service accounts.
func canAccessTeam(caller *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if caller.IsAdmin() {
		return true
	}
	return caller.IsMemberOfTeam(teamID)
}

// canAccessToken reports whether the caller may read a token's metadata.
func canAccessToken(caller *entities.User, token *entities.APIToken) bool {
	switch token.Scope() {
	case entities.APITokenScopeUser:
		// Strictly owner-only; admin has no special bypass for personal tokens.
		return caller.ID() == token.UserID()
	case entities.APITokenScopeTeam:
		return canAccessTeam(caller, token.TeamID())
	default:
		return false
	}
}

// canDeleteToken reports whether the caller may delete a token.
//   - Personal tokens: only the owner.
//   - Team tokens: the creator or an admin.
func canDeleteToken(caller *entities.User, token *entities.APIToken) bool {
	switch token.Scope() {
	case entities.APITokenScopeUser:
		return caller.ID() == token.UserID()
	case entities.APITokenScopeTeam:
		if caller.IsAdmin() {
			return true
		}
		return caller.ID() == token.CreatedBy()
	default:
		return false
	}
}

// serviceAccountUserID returns the canonical service-account user id for a
// team, matching the format used by SimpleAuthService.CreateServiceAccountForTeam.
func serviceAccountUserID(teamID string) entities.UserID {
	return entities.UserID(fmt.Sprintf("sa-%s", strings.ReplaceAll(teamID, "/", "-")))
}

// callerTeamIDs returns the team ids the caller belongs to. For service
// accounts it is the single team; for GitHub users it is derived from their
// team memberships.
func callerTeamIDs(caller *entities.User) []string {
	if caller.UserType() == entities.UserTypeServiceAccount {
		if caller.TeamID() != "" {
			return []string{caller.TeamID()}
		}
		return nil
	}
	if gh := caller.GitHubInfo(); gh != nil {
		teams := gh.Teams()
		out := make([]string, 0, len(teams))
		for _, t := range teams {
			out = append(out, t.Organization+"/"+t.TeamSlug)
		}
		return out
	}
	return nil
}
