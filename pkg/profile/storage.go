package profile

import (
	"context"
	"errors"
)

// Common errors
var (
	ErrProfileNotFound = errors.New("profile not found")
	ErrInvalidProfile  = errors.New("invalid profile data")
)

// Storage defines the interface for profile persistence
type Storage interface {
	// Save stores user profiles
	Save(ctx context.Context, userProfiles *UserProfiles) error

	// Load retrieves user profiles by user ID
	Load(ctx context.Context, userID string) (*UserProfiles, error)

	// Update updates existing user profiles
	Update(ctx context.Context, userID string, update *UserProfilesUpdate) error

	// Delete removes user profiles
	Delete(ctx context.Context, userID string) error

	// Exists checks if user profiles exist
	Exists(ctx context.Context, userID string) (bool, error)

	// List returns all user IDs (optional, may return empty slice if not supported)
	List(ctx context.Context) ([]string, error)
}

// StorageType represents the type of storage backend
type StorageType string

const (
	StorageTypeFilesystem StorageType = "filesystem"
	StorageTypeS3         StorageType = "s3"
)

// Config represents configuration for profile storage
type Config struct {
	Type StorageType `yaml:"type" json:"type"`

	// Filesystem-specific config
	BasePath string `yaml:"base_path,omitempty" json:"base_path,omitempty"`

	// S3-specific config
	S3Bucket   string `yaml:"s3_bucket,omitempty" json:"s3_bucket,omitempty"`
	S3Region   string `yaml:"s3_region,omitempty" json:"s3_region,omitempty"`
	S3Endpoint string `yaml:"s3_endpoint,omitempty" json:"s3_endpoint,omitempty"`
	S3Prefix   string `yaml:"s3_prefix,omitempty" json:"s3_prefix,omitempty"`
}
