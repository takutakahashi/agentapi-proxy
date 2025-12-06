package session

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// StartSessionRequest represents the input for starting a new session
type StartSessionRequest struct {
	UserID      entities.UserID   `json:"user_id"`
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Message     string            `json:"message,omitempty"`
	AuthContext *AuthContext      `json:"-"` // Not part of JSON, set by auth middleware
}

// AuthContext contains authentication information
type AuthContext struct {
	UserRole        string
	TeamEnvFile     string
	AuthTeamEnvFile string
}

// StartSessionResponse represents the output of starting a session
type StartSessionResponse struct {
	SessionID string `json:"session_id"`
}

// RepositoryInfo contains repository information extracted from tags
type RepositoryInfo struct {
	FullName string
	CloneDir string
}

// EnvMergeConfig holds configuration for environment variable merging
type EnvMergeConfig struct {
	RoleEnvFiles    *map[string]string
	UserRole        string
	TeamEnvFile     string
	AuthTeamEnvFile string
	RequestEnv      map[string]string
}

// Validate validates the StartSessionRequest
func (req *StartSessionRequest) Validate() error {
	if req.UserID == "" {
		return ErrInvalidUserID
	}
	return nil
}
