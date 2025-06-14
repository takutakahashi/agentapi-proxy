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

// Config represents the proxy configuration
type Config struct {
	// StartPort is the starting port for agentapi servers
	StartPort int `json:"start_port" mapstructure:"start_port"`
	// Auth represents authentication configuration
	Auth AuthConfig `json:"auth" mapstructure:"auth"`
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
					log.Printf("API key expired for user %s", apiKey.UserID)
					continue
				}
			}
			return &apiKey, true
		}
	}
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
