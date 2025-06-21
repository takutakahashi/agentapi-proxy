package config

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Enabled    bool     `json:"enabled" mapstructure:"enabled"`
	APIKeys    []APIKey `json:"api_keys" mapstructure:"api_keys"`
	KeysFile   string   `json:"keys_file" mapstructure:"keys_file"`
	HeaderName string   `json:"header_name" mapstructure:"header_name"`
}

// APIKey represents an API key configuration
type APIKey struct {
	Key         string   `json:"key" mapstructure:"key"`
	UserID      string   `json:"user_id" mapstructure:"user_id"`
	Role        string   `json:"role" mapstructure:"role"`
	Permissions []string `json:"permissions" mapstructure:"permissions"`
	CreatedAt   string   `json:"created_at" mapstructure:"created_at"`
	ExpiresAt   string   `json:"expires_at,omitempty" mapstructure:"expires_at"`
}

// PersistenceConfig represents session persistence configuration
type PersistenceConfig struct {
	Enabled                bool `json:"enabled" mapstructure:"enabled"`
	Backend                string `json:"backend" mapstructure:"backend"` // "file", "sqlite", "postgres"
	FilePath               string `json:"file_path" mapstructure:"file_path"`
	SyncInterval           int    `json:"sync_interval_seconds" mapstructure:"sync_interval_seconds"`
	EncryptSecrets         bool   `json:"encrypt_sensitive_data" mapstructure:"encrypt_sensitive_data"`
	SessionRecoveryMaxAge  int    `json:"session_recovery_max_age_hours" mapstructure:"session_recovery_max_age_hours"` // Max age in hours for session recovery
}

// Config represents the proxy configuration
type Config struct {
	// StartPort is the starting port for agentapi servers
	StartPort int `json:"start_port" mapstructure:"start_port"`
	// Auth represents authentication configuration
	Auth AuthConfig `json:"auth" mapstructure:"auth"`
	// Persistence represents session persistence configuration
	Persistence PersistenceConfig `json:"persistence" mapstructure:"persistence"`
	// DisableHeartbeat disables heartbeat checking (default: false)
	DisableHeartbeat bool `json:"disable_heartbeat" mapstructure:"disable_heartbeat"`
	// DisableZombieCleanup disables automatic zombie session cleanup (default: false)
	DisableZombieCleanup bool `json:"disable_zombie_cleanup" mapstructure:"disable_zombie_cleanup"`
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close config file: %v", err)
		}
	}()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	// Set default values if not specified
	if config.StartPort == 0 {
		config.StartPort = 9000
	}

	// Set default auth configuration
	if config.Auth.HeaderName == "" {
		config.Auth.HeaderName = "X-API-Key"
	}

	// Load API keys from external file if specified
	if config.Auth.Enabled && config.Auth.KeysFile != "" {
		if err := config.loadAPIKeysFromFile(); err != nil {
			log.Printf("Warning: Failed to load API keys from %s: %v", config.Auth.KeysFile, err)
		}
	}

	return &config, nil
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		StartPort: 9000,
		Auth: AuthConfig{
			Enabled:    false,
			HeaderName: "X-API-Key",
			APIKeys:    []APIKey{},
		},
		Persistence: PersistenceConfig{
			Enabled:               false,
			Backend:               "file",
			FilePath:              "./sessions.json",
			SyncInterval:          30,
			EncryptSecrets:        true,
			SessionRecoveryMaxAge: 24, // Default 24 hours
		},
		DisableHeartbeat:     false,
		DisableZombieCleanup: false,
	}
}

// loadAPIKeysFromFile loads API keys from an external JSON file
func (c *Config) loadAPIKeysFromFile() error {
	file, err := os.Open(c.Auth.KeysFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close API keys file: %v", err)
		}
	}()

	var keysData struct {
		APIKeys []APIKey `json:"api_keys"`
	}

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&keysData); err != nil {
		return err
	}

	c.Auth.APIKeys = keysData.APIKeys
	return nil
}

// ValidateAPIKey validates an API key and returns user information
func (c *Config) ValidateAPIKey(key string) (*APIKey, bool) {
	if !c.Auth.Enabled {
		return nil, false
	}

	for _, apiKey := range c.Auth.APIKeys {
		if apiKey.Key == key {
			// Check if key is expired
			if apiKey.ExpiresAt != "" {
				expiryTime, err := time.Parse(time.RFC3339, apiKey.ExpiresAt)
				if err != nil {
					log.Printf("Invalid expiry time format for API key: %v", err)
					continue
				}
				if time.Now().After(expiryTime) {
					maskedExpiredKey := key
					if len(key) > 8 {
						maskedExpiredKey = key[:8] + "***"
					} else if len(key) > 0 {
						maskedExpiredKey = key[:1] + "***"
					}
					log.Printf("API key expired for user %s (key: %s)", apiKey.UserID, maskedExpiredKey)
					continue
				}
			}
			return &apiKey, true
		}
	}
	// Log invalid API key attempt with masked key for security
	maskedKey := key
	if len(key) > 8 {
		maskedKey = key[:8] + "***"
	} else if len(key) > 0 {
		maskedKey = key[:1] + "***"
	}
	log.Printf("API key validation failed: invalid key %s", maskedKey)
	return nil, false
}

// HasPermission checks if a user has a specific permission
func (apiKey *APIKey) HasPermission(permission string) bool {
	for _, perm := range apiKey.Permissions {
		if perm == permission || perm == "*" {
			return true
		}
	}
	return false
}
