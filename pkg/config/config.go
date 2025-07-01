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
//	AGENTAPI_AUTH_STATIC_ENABLED=true
//	AGENTAPI_AUTH_STATIC_HEADER_NAME=X-API-Key
//	AGENTAPI_AUTH_STATIC_KEYS_FILE=/path/to/keys.json
//	AGENTAPI_AUTH_GITHUB_ENABLED=true
//	AGENTAPI_AUTH_GITHUB_BASE_URL=https://api.github.com
//	AGENTAPI_AUTH_GITHUB_TOKEN_HEADER=Authorization
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID=your_client_id
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET=your_client_secret
//	AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE=read:user read:org
//	AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE=user
//	AGENTAPI_PERSISTENCE_ENABLED=true
//	AGENTAPI_PERSISTENCE_BACKEND=file
//	AGENTAPI_PERSISTENCE_FILE_PATH=./sessions.json
//	AGENTAPI_PERSISTENCE_S3_BUCKET=my-bucket
//	AGENTAPI_PERSISTENCE_S3_REGION=us-east-1
//	AGENTAPI_ENABLE_MULTIPLE_USERS=true
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
	"gopkg.in/yaml.v2"
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
	DefaultRole        string                  `json:"default_role" mapstructure:"default_role" yaml:"default_role"`
	DefaultPermissions []string                `json:"default_permissions" mapstructure:"default_permissions" yaml:"default_permissions"`
	TeamRoleMapping    map[string]TeamRoleRule `json:"team_role_mapping" mapstructure:"team_role_mapping" yaml:"team_role_mapping"`
}

// TeamRoleRule represents a team-based role rule
type TeamRoleRule struct {
	Role        string   `json:"role" mapstructure:"role" yaml:"role"`
	Permissions []string `json:"permissions" mapstructure:"permissions" yaml:"permissions"`
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
	Backend               string `json:"backend" mapstructure:"backend"` // "file", "sqlite", "postgres", "s3"
	FilePath              string `json:"file_path" mapstructure:"file_path"`
	SyncInterval          int    `json:"sync_interval_seconds" mapstructure:"sync_interval_seconds"`
	EncryptSecrets        bool   `json:"encrypt_sensitive_data" mapstructure:"encrypt_sensitive_data"`
	SessionRecoveryMaxAge int    `json:"session_recovery_max_age_hours" mapstructure:"session_recovery_max_age_hours"` // Max age in hours for session recovery

	// S3-specific configuration
	S3Bucket    string `json:"s3_bucket" mapstructure:"s3_bucket"`
	S3Region    string `json:"s3_region" mapstructure:"s3_region"`
	S3Prefix    string `json:"s3_prefix" mapstructure:"s3_prefix"`
	S3Endpoint  string `json:"s3_endpoint" mapstructure:"s3_endpoint"` // For custom S3-compatible services
	S3AccessKey string `json:"s3_access_key" mapstructure:"s3_access_key"`
	S3SecretKey string `json:"s3_secret_key" mapstructure:"s3_secret_key"`
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
	// AuthConfigFile is the path to an external auth configuration file (e.g., from ConfigMap)
	AuthConfigFile string `json:"auth_config_file" mapstructure:"auth_config_file"`
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

	// Explicitly bind environment variables for nested configuration
	bindEnvVars(v)

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

	// Initialize config structs from environment variables if they don't exist
	initializeConfigStructsFromEnv(&config, v)

	// Load external auth configuration if specified
	if config.AuthConfigFile != "" {
		if err := loadAuthConfigFromFile(&config, config.AuthConfigFile); err != nil {
			log.Printf("[CONFIG] Warning: Failed to load auth config from %s: %v", config.AuthConfigFile, err)
		} else {
			log.Printf("[CONFIG] Loaded auth config from: %s", config.AuthConfigFile)
		}
	}

	// Apply post-processing
	if err := postProcessConfig(&config); err != nil {
		return nil, err
	}

	// Debug: Log configuration summary
	log.Printf("[CONFIG] Auth enabled: %v", config.Auth.Enabled)
	log.Printf("[CONFIG] Static auth enabled: %v", config.Auth.Static != nil && config.Auth.Static.Enabled)
	log.Printf("[CONFIG] GitHub auth enabled: %v", config.Auth.GitHub != nil && config.Auth.GitHub.Enabled)
	if config.Auth.GitHub != nil {
		log.Printf("[CONFIG] GitHub OAuth configured: %v", config.Auth.GitHub.OAuth != nil)
	}
	log.Printf("[CONFIG] Persistence enabled: %v (backend: %s)", config.Persistence.Enabled, config.Persistence.Backend)
	log.Printf("[CONFIG] Multiple users enabled: %v", config.EnableMultipleUsers)

	return &config, nil
}

// initializeConfigStructsFromEnv initializes config structs from environment variables
func initializeConfigStructsFromEnv(config *Config, v *viper.Viper) {
	// Initialize Auth.Static if environment variables are set
	if config.Auth.Static == nil && (v.GetBool("auth.static.enabled") || v.GetString("auth.static.header_name") != "" || v.GetString("auth.static.keys_file") != "") {
		config.Auth.Static = &StaticAuthConfig{
			Enabled:    v.GetBool("auth.static.enabled"),
			HeaderName: v.GetString("auth.static.header_name"),
			KeysFile:   v.GetString("auth.static.keys_file"),
			APIKeys:    []APIKey{},
		}
		log.Printf("[CONFIG] Initialized Static auth config from environment variables")
	}

	// Initialize Auth.GitHub if environment variables are set
	if config.Auth.GitHub == nil && (v.GetBool("auth.github.enabled") || v.GetString("auth.github.base_url") != "" || v.GetString("auth.github.token_header") != "") {
		config.Auth.GitHub = &GitHubAuthConfig{
			Enabled:     v.GetBool("auth.github.enabled"),
			BaseURL:     v.GetString("auth.github.base_url"),
			TokenHeader: v.GetString("auth.github.token_header"),
			UserMapping: GitHubUserMapping{
				DefaultRole:        v.GetString("auth.github.user_mapping.default_role"),
				DefaultPermissions: v.GetStringSlice("auth.github.user_mapping.default_permissions"),
			},
		}
		log.Printf("[CONFIG] Initialized GitHub auth config from environment variables")
	}

	// Initialize Auth.GitHub.OAuth if environment variables are set
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth == nil {
		// Check if OAuth environment variables are set
		clientID := v.GetString("auth.github.oauth.client_id")
		clientSecret := v.GetString("auth.github.oauth.client_secret")

		if clientID != "" || clientSecret != "" {
			config.Auth.GitHub.OAuth = &GitHubOAuthConfig{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				Scope:        v.GetString("auth.github.oauth.scope"),
				BaseURL:      v.GetString("auth.github.oauth.base_url"),
			}
			log.Printf("[CONFIG] Initialized GitHub OAuth config from environment variables")
			log.Printf("[CONFIG] OAuth ClientID from env: %v", clientID != "")
			log.Printf("[CONFIG] OAuth ClientSecret from env: %v", clientSecret != "")
		}
	}

	// Override fields if environment variables are set (even if structures already exist)
	if config.Auth.Static != nil {
		if v.IsSet("auth.static.keys_file") {
			config.Auth.Static.KeysFile = v.GetString("auth.static.keys_file")
		}
	}

	if config.Auth.GitHub != nil {
		if v.IsSet("auth.github.user_mapping.default_role") {
			config.Auth.GitHub.UserMapping.DefaultRole = v.GetString("auth.github.user_mapping.default_role")
		}
		if v.IsSet("auth.github.user_mapping.default_permissions") {
			config.Auth.GitHub.UserMapping.DefaultPermissions = v.GetStringSlice("auth.github.user_mapping.default_permissions")
		}

		// Override OAuth settings if already exists
		if config.Auth.GitHub.OAuth != nil {
			if clientID := v.GetString("auth.github.oauth.client_id"); clientID != "" {
				config.Auth.GitHub.OAuth.ClientID = clientID
			}
			if clientSecret := v.GetString("auth.github.oauth.client_secret"); clientSecret != "" {
				config.Auth.GitHub.OAuth.ClientSecret = clientSecret
			}
			if scope := v.GetString("auth.github.oauth.scope"); scope != "" {
				config.Auth.GitHub.OAuth.Scope = scope
			}
			if baseURL := v.GetString("auth.github.oauth.base_url"); baseURL != "" {
				config.Auth.GitHub.OAuth.BaseURL = baseURL
			}
		}
	}

	// Initialize Persistence config if environment variables are set
	if !config.Persistence.Enabled && v.GetBool("persistence.enabled") {
		config.Persistence.Enabled = true
		log.Printf("[CONFIG] Enabled persistence from environment variables")
	}
	if config.Persistence.Backend == "" && v.GetString("persistence.backend") != "" {
		config.Persistence.Backend = v.GetString("persistence.backend")
	}
	if config.Persistence.FilePath == "" && v.GetString("persistence.file_path") != "" {
		config.Persistence.FilePath = v.GetString("persistence.file_path")
	}
	if config.Persistence.SyncInterval == 0 && v.GetInt("persistence.sync_interval_seconds") != 0 {
		config.Persistence.SyncInterval = v.GetInt("persistence.sync_interval_seconds")
	}
	if config.Persistence.S3Bucket == "" && v.GetString("persistence.s3_bucket") != "" {
		config.Persistence.S3Bucket = v.GetString("persistence.s3_bucket")
		config.Persistence.S3Region = v.GetString("persistence.s3_region")
		config.Persistence.S3Prefix = v.GetString("persistence.s3_prefix")
		config.Persistence.S3Endpoint = v.GetString("persistence.s3_endpoint")
		config.Persistence.S3AccessKey = v.GetString("persistence.s3_access_key")
		config.Persistence.S3SecretKey = v.GetString("persistence.s3_secret_key")
		log.Printf("[CONFIG] Initialized S3 persistence config from environment variables")
	}
}

// bindEnvVars explicitly binds environment variables to configuration keys
func bindEnvVars(v *viper.Viper) {
	// Bind nested configuration keys to environment variables
	// Note: BindEnv errors are generally not critical and can be ignored
	// as they typically occur only when the key is already bound

	// Auth configuration
	_ = v.BindEnv("auth.enabled")
	_ = v.BindEnv("auth.static.enabled")
	_ = v.BindEnv("auth.static.header_name")
	_ = v.BindEnv("auth.static.keys_file")

	// GitHub auth configuration
	_ = v.BindEnv("auth.github.enabled")
	_ = v.BindEnv("auth.github.base_url")
	_ = v.BindEnv("auth.github.token_header")
	_ = v.BindEnv("auth.github.user_mapping.default_role")
	_ = v.BindEnv("auth.github.user_mapping.default_permissions")

	// GitHub OAuth configuration
	_ = v.BindEnv("auth.github.oauth.client_id")
	_ = v.BindEnv("auth.github.oauth.client_secret")
	_ = v.BindEnv("auth.github.oauth.scope")
	_ = v.BindEnv("auth.github.oauth.base_url")

	// Persistence configuration
	_ = v.BindEnv("persistence.enabled")
	_ = v.BindEnv("persistence.backend")
	_ = v.BindEnv("persistence.file_path")
	_ = v.BindEnv("persistence.sync_interval_seconds")
	_ = v.BindEnv("persistence.encrypt_sensitive_data")
	_ = v.BindEnv("persistence.session_recovery_max_age_hours")
	_ = v.BindEnv("persistence.s3_bucket")
	_ = v.BindEnv("persistence.s3_region")
	_ = v.BindEnv("persistence.s3_prefix")
	_ = v.BindEnv("persistence.s3_endpoint")
	_ = v.BindEnv("persistence.s3_access_key")
	_ = v.BindEnv("persistence.s3_secret_key")

	// Other configuration
	_ = v.BindEnv("start_port")
	_ = v.BindEnv("enable_multiple_users")
	_ = v.BindEnv("auth_config_file")
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

	// S3 persistence defaults
	v.SetDefault("persistence.s3_region", "us-east-1")
	v.SetDefault("persistence.s3_prefix", "sessions/")

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

// AuthConfigOverride represents auth configuration overrides from external file
type AuthConfigOverride struct {
	GitHub *GitHubAuthConfigOverride `json:"github,omitempty" yaml:"github,omitempty"`
}

// GitHubAuthConfigOverride represents GitHub auth configuration overrides
type GitHubAuthConfigOverride struct {
	UserMapping *GitHubUserMapping `json:"user_mapping,omitempty" yaml:"user_mapping,omitempty"`
}

// LoadAuthConfigFromFile loads auth configuration from an external file (e.g., ConfigMap)
func LoadAuthConfigFromFile(config *Config, filename string) error {
	return loadAuthConfigFromFile(config, filename)
}

// loadAuthConfigFromFile loads auth configuration from an external file (e.g., ConfigMap)
func loadAuthConfigFromFile(config *Config, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close auth config file: %v", err)
		}
	}()

	var authOverride AuthConfigOverride

	// Determine file format based on extension
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		// Use yaml package directly for YAML files
		decoder := yaml.NewDecoder(file)
		if err := decoder.Decode(&authOverride); err != nil {
			return err
		}
	} else {
		// Default to JSON
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&authOverride); err != nil {
			return err
		}
	}

	// Apply overrides to the main config
	if authOverride.GitHub != nil {
		// Initialize GitHub config if it doesn't exist
		if config.Auth.GitHub == nil {
			config.Auth.GitHub = &GitHubAuthConfig{}
		}

		// Override user mapping if provided
		if authOverride.GitHub.UserMapping != nil {
			config.Auth.GitHub.UserMapping = *authOverride.GitHub.UserMapping
			log.Printf("[CONFIG] Applied GitHub user mapping from external config")
		}
	}

	return nil
}
