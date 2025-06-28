// Package config provides configuration management for agentapi-proxy using viper.
//
// Configuration can be loaded from:
//   - JSON files (backward compatibility)
//   - YAML files
//   - Environment variables with AGENTAPI_ prefix
//
// Environment variable examples:
//
//	AGENTAPI_START_PORT=8080
//	AGENTAPI_AUTH_ENABLED=true
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID=your_client_id
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET=your_client_secret
//	AGENTAPI_PERSISTENCE_ENABLED=true
//	AGENTAPI_PERSISTENCE_BACKEND=file
//
// Configuration file search paths:
//   - Current directory
//   - $HOME/.agentapi/
//   - /etc/agentapi/
//
// Configuration file names: config.json, config.yaml, config.yml
package config

import (
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Enabled bool              `json:"enabled" mapstructure:"enabled"`
	Static  *StaticAuthConfig `json:"static,omitempty" mapstructure:"static"`
	GitHub  *GitHubAuthConfig `json:"github,omitempty" mapstructure:"github"`
}

// StaticAuthConfig represents static API key authentication
type StaticAuthConfig struct {
	Enabled    bool     `json:"enabled" mapstructure:"enabled"`
	APIKeys    []APIKey `json:"api_keys" mapstructure:"api_keys"`
	KeysFile   string   `json:"keys_file" mapstructure:"keys_file"`
	HeaderName string   `json:"header_name" mapstructure:"header_name"`
}

// GitHubAuthConfig represents GitHub OAuth authentication
type GitHubAuthConfig struct {
	Enabled     bool               `json:"enabled" mapstructure:"enabled"`
	BaseURL     string             `json:"base_url" mapstructure:"base_url"`
	TokenHeader string             `json:"token_header" mapstructure:"token_header"`
	UserMapping GitHubUserMapping  `json:"user_mapping" mapstructure:"user_mapping"`
	OAuth       *GitHubOAuthConfig `json:"oauth,omitempty" mapstructure:"oauth"`
}

// GitHubOAuthConfig represents GitHub OAuth2 configuration
type GitHubOAuthConfig struct {
	ClientID     string `json:"client_id" mapstructure:"client_id"`
	ClientSecret string `json:"client_secret" mapstructure:"client_secret"`
	Scope        string `json:"scope" mapstructure:"scope"`
	BaseURL      string `json:"base_url,omitempty" mapstructure:"base_url"`
}

// GitHubUserMapping represents user role mapping configuration
type GitHubUserMapping struct {
	DefaultRole        string                  `json:"default_role" mapstructure:"default_role"`
	DefaultPermissions []string                `json:"default_permissions" mapstructure:"default_permissions"`
	TeamRoleMapping    map[string]TeamRoleRule `json:"team_role_mapping" mapstructure:"team_role_mapping"`
}

// TeamRoleRule represents a team-based role rule
type TeamRoleRule struct {
	Role        string   `json:"role" mapstructure:"role"`
	Permissions []string `json:"permissions" mapstructure:"permissions"`
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
	Enabled               bool   `json:"enabled" mapstructure:"enabled"`
	Backend               string `json:"backend" mapstructure:"backend"` // "file", "sqlite", "postgres"
	FilePath              string `json:"file_path" mapstructure:"file_path"`
	SyncInterval          int    `json:"sync_interval_seconds" mapstructure:"sync_interval_seconds"`
	EncryptSecrets        bool   `json:"encrypt_sensitive_data" mapstructure:"encrypt_sensitive_data"`
	SessionRecoveryMaxAge int    `json:"session_recovery_max_age_hours" mapstructure:"session_recovery_max_age_hours"` // Max age in hours for session recovery
}

// Config represents the proxy configuration
type Config struct {
	// StartPort is the starting port for agentapi servers
	StartPort int `json:"start_port" mapstructure:"start_port"`
	// Auth represents authentication configuration
	Auth AuthConfig `json:"auth" mapstructure:"auth"`
	// Persistence represents session persistence configuration
	Persistence PersistenceConfig `json:"persistence" mapstructure:"persistence"`
	// EnableMultipleUsers enables user-specific directory isolation
	EnableMultipleUsers bool `json:"enable_multiple_users" mapstructure:"enable_multiple_users"`
}

// LoadConfig loads configuration using viper with support for JSON, YAML, and environment variables
func LoadConfig(filename string) (*Config, error) {
	v := viper.New()

	// Set up configuration file
	if filename != "" {
		v.SetConfigFile(filename)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.agentapi")
		v.AddConfigPath("/etc/agentapi/")
	}

	// Enable environment variable support
	v.AutomaticEnv()
	v.SetEnvPrefix("AGENTAPI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set defaults
	setDefaults(v)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// If no config file is found, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		log.Printf("[CONFIG] No config file found, using defaults and environment variables")
	} else {
		log.Printf("[CONFIG] Using config file: %s", v.ConfigFileUsed())
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Apply defaults for any fields that weren't set in config file
	applyConfigDefaults(&config)

	// Apply post-processing
	if err := postProcessConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// setDefaults sets default values for viper configuration
func setDefaults(v *viper.Viper) {
	v.SetDefault("start_port", 9000)

	// Persistence defaults
	v.SetDefault("persistence.enabled", false)
	v.SetDefault("persistence.backend", "file")
	v.SetDefault("persistence.file_path", "./sessions.json")
	v.SetDefault("persistence.sync_interval_seconds", 30)
	v.SetDefault("persistence.encrypt_sensitive_data", true)
	v.SetDefault("persistence.session_recovery_max_age_hours", 24)

	// Auth defaults
	v.SetDefault("auth.enabled", false)
	v.SetDefault("auth.static.enabled", false)
	v.SetDefault("auth.static.header_name", "X-API-Key")
	v.SetDefault("auth.github.enabled", false)
	v.SetDefault("auth.github.base_url", "https://api.github.com")
	v.SetDefault("auth.github.token_header", "Authorization")
	v.SetDefault("auth.github.oauth.client_id", "")
	v.SetDefault("auth.github.oauth.client_secret", "")
	v.SetDefault("auth.github.oauth.scope", "read:user read:org")
	v.SetDefault("auth.github.oauth.base_url", "")

	// Multiple users default
	v.SetDefault("enable_multiple_users", false)
}

// applyConfigDefaults applies default values to any unset configuration fields
func applyConfigDefaults(config *Config) {
	// Apply defaults for persistence if the entire struct is uninitialized
	if config.Persistence.Backend == "" {
		config.Persistence.Backend = "file"
	}
	if config.Persistence.FilePath == "" {
		config.Persistence.FilePath = "./sessions.json"
	}
	if config.Persistence.SyncInterval == 0 {
		config.Persistence.SyncInterval = 30
	}
	if !config.Persistence.Enabled {
		config.Persistence.EncryptSecrets = true
		config.Persistence.SessionRecoveryMaxAge = 24
	}

	// Apply auth defaults
	if config.Auth.Static != nil && config.Auth.Static.HeaderName == "" {
		config.Auth.Static.HeaderName = "X-API-Key"
	}
	if config.Auth.GitHub != nil {
		if config.Auth.GitHub.BaseURL == "" {
			config.Auth.GitHub.BaseURL = "https://api.github.com"
		}
		if config.Auth.GitHub.TokenHeader == "" {
			config.Auth.GitHub.TokenHeader = "Authorization"
		}
		if config.Auth.GitHub.OAuth != nil && config.Auth.GitHub.OAuth.Scope == "" {
			config.Auth.GitHub.OAuth.Scope = "read:user read:org"
		}
	}
}

// postProcessConfig applies post-processing logic to the configuration
func postProcessConfig(config *Config) error {
	// Set default auth configuration
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		if config.Auth.GitHub.OAuth.BaseURL == "" {
			config.Auth.GitHub.OAuth.BaseURL = config.Auth.GitHub.BaseURL
		}
	}

	// Expand environment variables in OAuth configuration (for ${VAR_NAME} syntax in config files)
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		config.Auth.GitHub.OAuth.ClientID = expandEnvVars(config.Auth.GitHub.OAuth.ClientID)
		config.Auth.GitHub.OAuth.ClientSecret = expandEnvVars(config.Auth.GitHub.OAuth.ClientSecret)
	}

	// Log OAuth configuration status (after expansion)
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		log.Printf("[CONFIG] OAuth ClientID configured: %v", config.Auth.GitHub.OAuth.ClientID != "")
		log.Printf("[CONFIG] OAuth ClientSecret configured: %v", config.Auth.GitHub.OAuth.ClientSecret != "")

		// Warn if OAuth is configured but credentials are missing
		if config.Auth.GitHub.OAuth.ClientID == "" || config.Auth.GitHub.OAuth.ClientSecret == "" {
			log.Printf("[CONFIG] Warning: OAuth is configured but Client ID or Client Secret is missing")
		}
	}

	// Load API keys from external file if specified
	if config.Auth.Enabled && config.Auth.Static != nil && config.Auth.Static.KeysFile != "" {
		if err := config.loadAPIKeysFromFile(); err != nil {
			log.Printf("Warning: Failed to load API keys from %s: %v", config.Auth.Static.KeysFile, err)
		}
	}

	return nil
}

// LoadConfigLegacy loads configuration from a JSON file (legacy method)
func LoadConfigLegacy(filename string) (*Config, error) {
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

	// Set default persistence configuration
	if config.Persistence.Backend == "" {
		config.Persistence.Backend = "file"
	}
	if config.Persistence.FilePath == "" {
		config.Persistence.FilePath = "./sessions.json"
	}
	if config.Persistence.SyncInterval == 0 {
		config.Persistence.SyncInterval = 30
	}
	if !config.Persistence.Enabled {
		config.Persistence.EncryptSecrets = true
		config.Persistence.SessionRecoveryMaxAge = 24
	}

	// Apply post-processing
	if err := postProcessConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// expandEnvVars expands environment variables in the form ${VAR_NAME}
func expandEnvVars(s string) string {
	if s == "" {
		return s
	}

	// Match ${VAR_NAME} pattern
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name (remove ${})
		varName := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")

		// Get environment variable value
		if value := os.Getenv(varName); value != "" {
			return value
		}

		// Return original string if environment variable is not set
		log.Printf("[CONFIG] Warning: Environment variable %s is not set", varName)
		return match
	})
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		StartPort: 9000,
		Auth: AuthConfig{
			Enabled: false,
			Static: &StaticAuthConfig{
				Enabled:    false,
				HeaderName: "X-API-Key",
				APIKeys:    []APIKey{},
			},
		},
		Persistence: PersistenceConfig{
			Enabled:               false,
			Backend:               "file",
			FilePath:              "./sessions.json",
			SyncInterval:          30,
			EncryptSecrets:        true,
			SessionRecoveryMaxAge: 24, // Default 24 hours
		},
		EnableMultipleUsers: false,
	}
}

// loadAPIKeysFromFile loads API keys from an external JSON file
func (c *Config) loadAPIKeysFromFile() error {
	file, err := os.Open(c.Auth.Static.KeysFile)
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

	c.Auth.Static.APIKeys = keysData.APIKeys
	return nil
}

// ValidateAPIKey validates an API key and returns user information
func (c *Config) ValidateAPIKey(key string) (*APIKey, bool) {
	if !c.Auth.Enabled || c.Auth.Static == nil || !c.Auth.Static.Enabled {
		return nil, false
	}

	for _, apiKey := range c.Auth.Static.APIKeys {
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
