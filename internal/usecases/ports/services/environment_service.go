package services

import (
	"context"
	"errors"
)

var (
	ErrEnvironmentMergeFailed   = errors.New("environment merge failed")
	ErrInvalidEnvironmentConfig = errors.New("invalid environment config")
)

// EnvMergeConfig holds configuration for environment variable merging
type EnvMergeConfig struct {
	RoleEnvFiles    *map[string]string
	UserRole        string
	TeamEnvFile     string
	AuthTeamEnvFile string
	RequestEnv      map[string]string
}

// EnvironmentService handles environment variable management
type EnvironmentService interface {
	// MergeEnvironmentVariables merges environment variables from multiple sources
	// Priority order (highest to lowest): RequestEnv, AuthTeamEnvFile, TeamEnvFile, RoleEnvFiles
	MergeEnvironmentVariables(ctx context.Context, config *EnvMergeConfig) (map[string]string, error)

	// ExtractTeamEnvFile extracts team environment file from tags
	ExtractTeamEnvFile(tags map[string]string) string
}
