package share

import (
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// CreateShareUseCase handles share creation
type CreateShareUseCase struct {
	shareRepo      repositories.ShareRepository
	sessionManager repositories.SessionManager
}

// NewCreateShareUseCase creates a new CreateShareUseCase
func NewCreateShareUseCase(
	shareRepo repositories.ShareRepository,
	sessionManager repositories.SessionManager,
) *CreateShareUseCase {
	return &CreateShareUseCase{
		shareRepo:      shareRepo,
		sessionManager: sessionManager,
	}
}

// Execute creates a new share for a session
func (uc *CreateShareUseCase) Execute(sessionID, createdBy string) (*entities.SessionShare, error) {
	// Check if session exists
	session := uc.sessionManager.GetSession(sessionID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Create share
	share := entities.NewSessionShare(sessionID, createdBy)
	if err := uc.shareRepo.Save(share); err != nil {
		return nil, fmt.Errorf("failed to save share: %w", err)
	}

	log.Printf("[SHARE] Created share for session %s by user %s", sessionID, createdBy)
	return share, nil
}

// GetShareUseCase handles share retrieval
type GetShareUseCase struct {
	shareRepo repositories.ShareRepository
}

// NewGetShareUseCase creates a new GetShareUseCase
func NewGetShareUseCase(shareRepo repositories.ShareRepository) *GetShareUseCase {
	return &GetShareUseCase{
		shareRepo: shareRepo,
	}
}

// ExecuteByToken gets a share by token
func (uc *GetShareUseCase) ExecuteByToken(token string) (*entities.SessionShare, error) {
	share, err := uc.shareRepo.FindByToken(token)
	if err != nil {
		return nil, err
	}

	if share.IsExpired() {
		return nil, fmt.Errorf("share has expired")
	}

	return share, nil
}

// ExecuteBySessionID gets a share by session ID
func (uc *GetShareUseCase) ExecuteBySessionID(sessionID string) (*entities.SessionShare, error) {
	return uc.shareRepo.FindBySessionID(sessionID)
}

// DeleteShareUseCase handles share deletion
type DeleteShareUseCase struct {
	shareRepo repositories.ShareRepository
}

// NewDeleteShareUseCase creates a new DeleteShareUseCase
func NewDeleteShareUseCase(shareRepo repositories.ShareRepository) *DeleteShareUseCase {
	return &DeleteShareUseCase{
		shareRepo: shareRepo,
	}
}

// ExecuteBySessionID deletes a share by session ID
func (uc *DeleteShareUseCase) ExecuteBySessionID(sessionID string) error {
	return uc.shareRepo.Delete(sessionID)
}

// ExecuteByToken deletes a share by token
func (uc *DeleteShareUseCase) ExecuteByToken(token string) error {
	return uc.shareRepo.DeleteByToken(token)
}

// CleanupExpiredSharesUseCase handles expired share cleanup
type CleanupExpiredSharesUseCase struct {
	shareRepo repositories.ShareRepository
}

// NewCleanupExpiredSharesUseCase creates a new CleanupExpiredSharesUseCase
func NewCleanupExpiredSharesUseCase(shareRepo repositories.ShareRepository) *CleanupExpiredSharesUseCase {
	return &CleanupExpiredSharesUseCase{
		shareRepo: shareRepo,
	}
}

// Execute cleans up expired shares
func (uc *CleanupExpiredSharesUseCase) Execute() (int, error) {
	return uc.shareRepo.CleanupExpired()
}
