package storage

import (
	"fmt"
)

// NewStorage creates a storage instance based on the configuration
func NewStorage(config *StorageConfig) (Storage, error) {
	switch config.Type {
	case "memory", "":
		return NewMemoryStorage(), nil

	case "file":
		if config.FilePath == "" {
			config.FilePath = "./sessions.json"
		}
		if config.SyncInterval == 0 {
			config.SyncInterval = 30 // Default to 30 seconds
		}
		return NewFileStorage(config.FilePath, config.SyncInterval, config.EncryptSecrets)

	case "sqlite":
		// TODO: Implement SQLite storage
		return nil, fmt.Errorf("SQLite storage not yet implemented")

	default:
		return nil, fmt.Errorf("unknown storage type: %s", config.Type)
	}
}
