package session

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// CreateSessionUseCase handles session creation
type CreateSessionUseCase struct {
	sessionManager repositories.SessionManager
	shareRepo      repositories.ShareRepository
}

// NewCreateSessionUseCase creates a new CreateSessionUseCase
func NewCreateSessionUseCase(
	sessionManager repositories.SessionManager,
	shareRepo repositories.ShareRepository,
) *CreateSessionUseCase {
	return &CreateSessionUseCase{
		sessionManager: sessionManager,
		shareRepo:      shareRepo,
	}
}

// Execute creates a new session
func (uc *CreateSessionUseCase) Execute(ctx context.Context, sessionID string, req *entities.RunServerRequest) (entities.Session, error) {
	return uc.sessionManager.CreateSession(ctx, sessionID, req, nil)
}

// ListSessionsUseCase handles session listing
type ListSessionsUseCase struct {
	sessionManager repositories.SessionManager
}

// NewListSessionsUseCase creates a new ListSessionsUseCase
func NewListSessionsUseCase(sessionManager repositories.SessionManager) *ListSessionsUseCase {
	return &ListSessionsUseCase{
		sessionManager: sessionManager,
	}
}

// Execute lists sessions matching the filter
func (uc *ListSessionsUseCase) Execute(filter entities.SessionFilter) []entities.Session {
	return uc.sessionManager.ListSessions(filter)
}

// GetSessionUseCase handles getting a single session
type GetSessionUseCase struct {
	sessionManager repositories.SessionManager
}

// NewGetSessionUseCase creates a new GetSessionUseCase
func NewGetSessionUseCase(sessionManager repositories.SessionManager) *GetSessionUseCase {
	return &GetSessionUseCase{
		sessionManager: sessionManager,
	}
}

// Execute gets a session by ID
func (uc *GetSessionUseCase) Execute(sessionID string) entities.Session {
	return uc.sessionManager.GetSession(sessionID)
}

// DeleteSessionUseCase handles session deletion
type DeleteSessionUseCase struct {
	sessionManager repositories.SessionManager
	shareRepo      repositories.ShareRepository
}

// NewDeleteSessionUseCase creates a new DeleteSessionUseCase
func NewDeleteSessionUseCase(
	sessionManager repositories.SessionManager,
	shareRepo repositories.ShareRepository,
) *DeleteSessionUseCase {
	return &DeleteSessionUseCase{
		sessionManager: sessionManager,
		shareRepo:      shareRepo,
	}
}

// Execute deletes a session by ID
func (uc *DeleteSessionUseCase) Execute(sessionID string) error {
	// Delete associated share link if exists (ignore errors as share may not exist)
	if uc.shareRepo != nil {
		if err := uc.shareRepo.Delete(sessionID); err != nil {
			log.Printf("[SESSION] Warning: failed to delete share for session %s: %v", sessionID, err)
		}
	}

	return uc.sessionManager.DeleteSession(sessionID)
}

// ValidateTeamAccessUseCase validates team access for session operations
type ValidateTeamAccessUseCase struct{}

// NewValidateTeamAccessUseCase creates a new ValidateTeamAccessUseCase
func NewValidateTeamAccessUseCase() *ValidateTeamAccessUseCase {
	return &ValidateTeamAccessUseCase{}
}

// ValidateTeamScope validates that user can create team-scoped sessions
func (uc *ValidateTeamAccessUseCase) ValidateTeamScope(
	scope entities.ResourceScope,
	teamID string,
	userTeams []string,
	isAuthenticated bool,
) error {
	if scope != entities.ScopeTeam {
		return nil // Not team scope, no validation needed
	}

	if teamID == "" {
		return fmt.Errorf("team_id is required when scope is 'team'")
	}

	if !isAuthenticated {
		return fmt.Errorf("authentication required for team-scoped sessions")
	}

	// Check if user is member of the team
	for _, t := range userTeams {
		if t == teamID {
			return nil
		}
	}

	return fmt.Errorf("you are not a member of this team")
}
