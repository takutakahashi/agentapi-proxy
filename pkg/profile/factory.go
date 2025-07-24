package profile

import (
	"fmt"
)

// NewStorage creates a new profile storage instance based on the configuration
func NewStorage(cfg *Config) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("profile storage config is nil")
	}

	switch cfg.Type {
	case StorageTypeFilesystem, "":
		// Default to filesystem if no type specified
		return NewFilesystemStorage(cfg.BasePath)

	case StorageTypeS3:
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("S3 bucket is required for S3 storage")
		}
		return NewS3Storage(cfg.S3Bucket, cfg.S3Region, cfg.S3Endpoint, cfg.S3Prefix)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
