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
	// Save stores a profile
	Save(ctx context.Context, profile *Profile) error

	// Load retrieves a profile by user ID
	Load(ctx context.Context, userID string) (*Profile, error)

	// Update updates an existing profile
	Update(ctx context.Context, userID string, update *ProfileUpdate) error

	// Delete removes a profile
	Delete(ctx context.Context, userID string) error

	// Exists checks if a profile exists
	Exists(ctx context.Context, userID string) (bool, error)

	// List returns all profile IDs (optional, may return empty slice if not supported)
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
