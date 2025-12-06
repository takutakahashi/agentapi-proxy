package services

import (
	"context"
	"errors"
)

var (
	ErrRepositoryInitFailed = errors.New("repository initialization failed")
	ErrInvalidRepository    = errors.New("invalid repository")
)

// RepositoryInfo contains repository information
type RepositoryInfo struct {
	FullName string
	CloneDir string
}

// RepositoryService handles repository operations
type RepositoryService interface {
	// ExtractRepositoryInfo extracts repository information from session tags
	ExtractRepositoryInfo(sessionID string, tags map[string]string) *RepositoryInfo

	// InitializeRepository clones and sets up a GitHub repository
	InitializeRepository(ctx context.Context, repoInfo *RepositoryInfo, userID string) error

	// ValidateRepository validates repository information
	ValidateRepository(repoInfo *RepositoryInfo) error
}
