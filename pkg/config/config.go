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
//	AGENTAPI_ENABLE_MULTIPLE_USERS=true
//	AGENTAPI_WEBHOOK_BASE_URL=https://example.com
//	AGENTAPI_WEBHOOK_GITHUB_ENTERPRISE_HOST=github.enterprise.com
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
	Static *StaticAuthConfig `json:"static,omitempty" mapstructure:"static"`
	GitHub *GitHubAuthConfig `json:"github,omitempty" mapstructure:"github"`
	AWS    *AWSAuthConfig    `json:"aws,omitempty" mapstructure:"aws"`
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
	EnvFile     string   `json:"env_file,omitempty" mapstructure:"env_file" yaml:"env_file"`
}

// AWSAuthConfig represents AWS IAM authentication configuration
type AWSAuthConfig struct {
	Enabled           bool           `json:"enabled" mapstructure:"enabled"`
	Region            string         `json:"region" mapstructure:"region"`
	AllowedAccountIDs []string       `json:"allowed_account_ids" mapstructure:"allowed_account_ids"` // Required: list of allowed AWS account IDs (empty = deny all)
	TeamTagKey        string         `json:"team_tag_key" mapstructure:"team_tag_key"`
	RequiredTagKey    string         `json:"required_tag_key" mapstructure:"required_tag_key"`     // Tag key that must exist (e.g., "agentapi-proxy")
	RequiredTagVal    string         `json:"required_tag_value" mapstructure:"required_tag_value"` // Expected tag value (e.g., "enabled")
	CacheTTL          string         `json:"cache_ttl" mapstructure:"cache_ttl"`
	UserMapping       AWSUserMapping `json:"user_mapping" mapstructure:"user_mapping"`
}

// AWSUserMapping represents AWS user role mapping configuration
type AWSUserMapping struct {
	DefaultRole        string                  `json:"default_role" mapstructure:"default_role" yaml:"default_role"`
	DefaultPermissions []string                `json:"default_permissions" mapstructure:"default_permissions" yaml:"default_permissions"`
	TeamRoleMapping    map[string]TeamRoleRule `json:"team_role_mapping" mapstructure:"team_role_mapping" yaml:"team_role_mapping"`
}

// RoleEnvFilesConfig represents role-based environment files configuration
type RoleEnvFilesConfig struct {
	// Enabled enables role-based environment file loading
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// Path is the directory path containing role-specific .env files
	Path string `json:"path" mapstructure:"path"`
	// LoadDefault loads default.env before role-specific env file
	LoadDefault bool `json:"load_default" mapstructure:"load_default"`
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

// Toleration represents a Kubernetes toleration for session pods
type Toleration struct {
	// Key is the taint key that the toleration applies to
	Key string `json:"key" mapstructure:"key" yaml:"key"`
	// Operator represents a key's relationship to the value (Equal or Exists)
	Operator string `json:"operator" mapstructure:"operator" yaml:"operator"`
	// Value is the taint value the toleration matches to
	Value string `json:"value" mapstructure:"value" yaml:"value"`
	// Effect indicates the taint effect to match (NoSchedule, PreferNoSchedule, NoExecute)
	Effect string `json:"effect" mapstructure:"effect" yaml:"effect"`
	// TolerationSeconds is the period of time the toleration tolerates the taint (for NoExecute)
	TolerationSeconds *int64 `json:"toleration_seconds,omitempty" mapstructure:"toleration_seconds" yaml:"toleration_seconds"`
}

// ScheduleWorkerConfig represents schedule worker configuration
type ScheduleWorkerConfig struct {
	// Enabled enables the schedule worker
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// CheckInterval is how often to check for due schedules (e.g., "30s", "1m")
	CheckInterval string `json:"check_interval" mapstructure:"check_interval"`
	// Namespace is the Kubernetes namespace for schedule resources
	Namespace string `json:"namespace" mapstructure:"namespace"`
	// LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership
	LeaseDuration string `json:"lease_duration" mapstructure:"lease_duration"`
	// RenewDeadline is the duration that the acting master will retry refreshing leadership before giving up
	RenewDeadline string `json:"renew_deadline" mapstructure:"renew_deadline"`
	// RetryPeriod is the duration the LeaderElector clients should wait between tries of actions
	RetryPeriod string `json:"retry_period" mapstructure:"retry_period"`
}

// WebhookConfig represents webhook configuration
type WebhookConfig struct {
	// BaseURL is the base URL for webhook endpoints (e.g., "https://example.com")
	// If not set, the URL will be auto-detected from incoming request headers
	BaseURL string `json:"base_url" mapstructure:"base_url"`
	// GitHubEnterpriseHost is the default GitHub Enterprise host for webhook matching
	// When set, webhooks without explicit enterprise_url will match against this host
	// Example: "github.enterprise.com" (hostname only, without https://)
	GitHubEnterpriseHost string `json:"github_enterprise_host" mapstructure:"github_enterprise_host"`
}

// KubernetesSessionConfig represents Kubernetes session manager configuration
type KubernetesSessionConfig struct {
	// Namespace is the Kubernetes namespace where session resources are created
	Namespace string `json:"namespace" mapstructure:"namespace"`
	// Image is the container image for session pods
	Image string `json:"image" mapstructure:"image"`
	// ImagePullPolicy is the image pull policy for session pods
	ImagePullPolicy string `json:"image_pull_policy" mapstructure:"image_pull_policy"`
	// ServiceAccount is the service account for session pods
	ServiceAccount string `json:"service_account" mapstructure:"service_account"`
	// BasePort is the port that agentapi listens on in session pods
	BasePort int `json:"base_port" mapstructure:"base_port"`
	// CPURequest is the CPU request for session pods
	CPURequest string `json:"cpu_request" mapstructure:"cpu_request"`
	// CPULimit is the CPU limit for session pods
	CPULimit string `json:"cpu_limit" mapstructure:"cpu_limit"`
	// MemoryRequest is the memory request for session pods
	MemoryRequest string `json:"memory_request" mapstructure:"memory_request"`
	// MemoryLimit is the memory limit for session pods
	MemoryLimit string `json:"memory_limit" mapstructure:"memory_limit"`
	// PVCEnabled enables PersistentVolumeClaim for session pods workdir
	// When disabled, EmptyDir is used instead (data is not persisted across pod restarts)
	PVCEnabled *bool `json:"pvc_enabled,omitempty" mapstructure:"pvc_enabled"`
	// PVCStorageClass is the storage class for session PVCs
	PVCStorageClass string `json:"pvc_storage_class" mapstructure:"pvc_storage_class"`
	// PVCStorageSize is the storage size for session PVCs
	PVCStorageSize string `json:"pvc_storage_size" mapstructure:"pvc_storage_size"`
	// PodStartTimeout is the timeout in seconds for pod startup
	PodStartTimeout int `json:"pod_start_timeout" mapstructure:"pod_start_timeout"`
	// PodStopTimeout is the timeout in seconds for pod termination
	PodStopTimeout int `json:"pod_stop_timeout" mapstructure:"pod_stop_timeout"`
	// ClaudeConfigBaseSecret is the name of the base Secret for Claude configuration
	// This Secret should contain claude.json and settings.json files
	// Note: Changed from ConfigMap to Secret to support sensitive data like GITHUB_TOKEN
	ClaudeConfigBaseSecret string `json:"claude_config_base_secret" mapstructure:"claude_config_base_secret"`
	// ClaudeConfigUserConfigMapPrefix is the prefix for user-specific ConfigMap names
	// Full name will be: {prefix}-{username} (e.g., claude-config-johndoe)
	ClaudeConfigUserConfigMapPrefix string `json:"claude_config_user_configmap_prefix" mapstructure:"claude_config_user_configmap_prefix"`
	// InitContainerImage is the image used for the init container that sets up Claude configuration
	// Defaults to the same image as the session container (Image field) if not specified
	InitContainerImage string `json:"init_container_image" mapstructure:"init_container_image"`
	// GitHubSecretName is the name of the Kubernetes Secret containing GitHub authentication credentials
	// This Secret is used by the clone-repo init container for repository cloning
	// Expected keys: GITHUB_TOKEN, GITHUB_APP_ID, GITHUB_APP_PEM, GITHUB_INSTALLATION_ID
	GitHubSecretName string `json:"github_secret_name" mapstructure:"github_secret_name"`
	// GitHubConfigSecretName is the name of the Kubernetes Secret containing GitHub configuration (non-auth)
	// This Secret contains GITHUB_API and GITHUB_URL for GitHub Enterprise Server support
	// It is kept separate from GitHubSecretName so that params.github_token can override authentication
	// without losing Enterprise Server URL settings
	GitHubConfigSecretName string `json:"github_config_secret_name" mapstructure:"github_config_secret_name"`
	// ConfigFile is the path to an external configuration file for kubernetes session settings
	// This file can contain node_selector and tolerations settings
	ConfigFile string `json:"config_file,omitempty" mapstructure:"config_file"`
	// NodeSelector is a selector which must be true for the pod to fit on a node
	// Example: {"disktype": "ssd", "kubernetes.io/arch": "amd64"}
	NodeSelector map[string]string `json:"node_selector,omitempty" mapstructure:"node_selector" yaml:"node_selector"`
	// Tolerations are tolerations for session pods to schedule onto nodes with matching taints
	Tolerations []Toleration `json:"tolerations,omitempty" mapstructure:"tolerations" yaml:"tolerations"`

	// Settings configuration
	// SettingsBaseSecret is the name of the Kubernetes Secret containing base settings configurations
	// This Secret is applied to all sessions and contains marketplaces and enabled_plugins settings
	// Team and user settings can override these base settings
	SettingsBaseSecret string `json:"settings_base_secret" mapstructure:"settings_base_secret"`

	// OpenTelemetry Collector configuration
	// OtelCollectorEnabled enables OpenTelemetry Collector sidecar for metrics collection
	OtelCollectorEnabled bool `json:"otel_collector_enabled" mapstructure:"otel_collector_enabled"`
	// OtelCollectorImage is the container image for otelcol sidecar
	OtelCollectorImage string `json:"otel_collector_image" mapstructure:"otel_collector_image"`
	// OtelCollectorScrapeInterval is the scrape interval for Claude Code metrics
	OtelCollectorScrapeInterval string `json:"otel_collector_scrape_interval" mapstructure:"otel_collector_scrape_interval"`
	// OtelCollectorClaudeCodePort is the port where Claude Code exposes metrics
	OtelCollectorClaudeCodePort int `json:"otel_collector_claude_code_port" mapstructure:"otel_collector_claude_code_port"`
	// OtelCollectorExporterPort is the port where otelcol exposes labeled metrics
	OtelCollectorExporterPort int `json:"otel_collector_exporter_port" mapstructure:"otel_collector_exporter_port"`
	// OtelCollectorCPURequest is the CPU request for otelcol sidecar
	OtelCollectorCPURequest string `json:"otel_collector_cpu_request" mapstructure:"otel_collector_cpu_request"`
	// OtelCollectorCPULimit is the CPU limit for otelcol sidecar
	OtelCollectorCPULimit string `json:"otel_collector_cpu_limit" mapstructure:"otel_collector_cpu_limit"`
	// OtelCollectorMemoryRequest is the memory request for otelcol sidecar
	OtelCollectorMemoryRequest string `json:"otel_collector_memory_request" mapstructure:"otel_collector_memory_request"`
	// OtelCollectorMemoryLimit is the memory limit for otelcol sidecar
	OtelCollectorMemoryLimit string `json:"otel_collector_memory_limit" mapstructure:"otel_collector_memory_limit"`

	// Slack Integration configuration (claude-posts sidecar)
	// SlackIntegrationImage is the container image for the claude-posts Slack integration sidecar
	// Defaults to ghcr.io/takutakahashi/claude-posts:0.2.0
	SlackIntegrationImage string `json:"slack_integration_image" mapstructure:"slack_integration_image"`
	// SlackBotTokenSecretName is the Kubernetes Secret name containing the Slack bot token
	// The token is exposed as SLACK_BOT_TOKEN env var in the sidecar
	SlackBotTokenSecretName string `json:"slack_bot_token_secret_name" mapstructure:"slack_bot_token_secret_name"`
	// SlackBotTokenSecretKey is the key within the Secret that holds the Slack bot token
	// Defaults to "bot-token"
	SlackBotTokenSecretKey string `json:"slack_bot_token_secret_key" mapstructure:"slack_bot_token_secret_key"`
}

// MemoryConfig represents memory backend configuration
type MemoryConfig struct {
	// Backend is the storage backend type: "kubernetes" (default) or "s3"
	Backend string          `json:"backend" mapstructure:"backend"`
	S3      *MemoryS3Config `json:"s3,omitempty" mapstructure:"s3"`
}

// MemoryS3Config represents S3 backend configuration for memory storage
type MemoryS3Config struct {
	// Bucket is the S3 bucket name (required)
	Bucket string `json:"bucket" mapstructure:"bucket"`
	// Region is the AWS region (optional, uses AWS default config if empty)
	Region string `json:"region" mapstructure:"region"`
	// Prefix is the key prefix for all memory objects (default: "agentapi-memory/")
	Prefix string `json:"prefix" mapstructure:"prefix"`
	// Endpoint is a custom S3-compatible endpoint URL (e.g., for rustfs or other S3-compatible services)
	Endpoint string `json:"endpoint" mapstructure:"endpoint"`
}

// Config represents the proxy configuration
type Config struct {
	// Auth represents authentication configuration
	Auth AuthConfig `json:"auth" mapstructure:"auth"`
	// AuthConfigFile is the path to an external auth configuration file (e.g., from ConfigMap)
	AuthConfigFile string `json:"auth_config_file" mapstructure:"auth_config_file"`
	// RoleEnvFiles is the configuration for role-based environment files
	RoleEnvFiles RoleEnvFilesConfig `json:"role_env_files" mapstructure:"role_env_files"`
	// KubernetesSession is the configuration for Kubernetes-based session management
	KubernetesSession KubernetesSessionConfig `json:"kubernetes_session" mapstructure:"kubernetes_session"`
	// ScheduleWorker is the configuration for the schedule worker
	ScheduleWorker ScheduleWorkerConfig `json:"schedule_worker" mapstructure:"schedule_worker"`
	// Webhook is the configuration for webhook functionality
	Webhook WebhookConfig `json:"webhook" mapstructure:"webhook"`
	// Memory is the configuration for memory storage backend
	Memory MemoryConfig `json:"memory" mapstructure:"memory"`
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
	log.Printf("[CONFIG] Static auth enabled: %v", config.Auth.Static != nil && config.Auth.Static.Enabled)
	log.Printf("[CONFIG] GitHub auth enabled: %v", config.Auth.GitHub != nil && config.Auth.GitHub.Enabled)
	if config.Auth.GitHub != nil {
		log.Printf("[CONFIG] GitHub OAuth configured: %v", config.Auth.GitHub.OAuth != nil)
	}
	log.Printf("[CONFIG] AWS auth enabled: %v", config.Auth.AWS != nil && config.Auth.AWS.Enabled)
	if config.Auth.AWS != nil && config.Auth.AWS.Enabled {
		log.Printf("[CONFIG] AWS region: %s", config.Auth.AWS.Region)
		log.Printf("[CONFIG] AWS allowed account IDs: %v", config.Auth.AWS.AllowedAccountIDs)
		log.Printf("[CONFIG] AWS team tag key: %s", config.Auth.AWS.TeamTagKey)
	}
	log.Printf("[CONFIG] Role-based env files enabled: %v", config.RoleEnvFiles.Enabled)

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

	// Initialize Auth.AWS if environment variables are set
	if config.Auth.AWS == nil && (v.GetBool("auth.aws.enabled") || v.GetString("auth.aws.region") != "" || len(v.GetStringSlice("auth.aws.allowed_account_ids")) > 0) {
		config.Auth.AWS = &AWSAuthConfig{
			Enabled:           v.GetBool("auth.aws.enabled"),
			Region:            v.GetString("auth.aws.region"),
			AllowedAccountIDs: v.GetStringSlice("auth.aws.allowed_account_ids"),
			TeamTagKey:        v.GetString("auth.aws.team_tag_key"),
			CacheTTL:          v.GetString("auth.aws.cache_ttl"),
			UserMapping: AWSUserMapping{
				DefaultRole:        v.GetString("auth.aws.user_mapping.default_role"),
				DefaultPermissions: v.GetStringSlice("auth.aws.user_mapping.default_permissions"),
			},
		}
		log.Printf("[CONFIG] Initialized AWS auth config from environment variables")
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

}

// bindEnvVars explicitly binds environment variables to configuration keys
func bindEnvVars(v *viper.Viper) {
	// Bind nested configuration keys to environment variables
	// Note: BindEnv errors are generally not critical and can be ignored
	// as they typically occur only when the key is already bound

	// Auth configuration
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

	// AWS auth configuration
	_ = v.BindEnv("auth.aws.enabled")
	_ = v.BindEnv("auth.aws.region")
	_ = v.BindEnv("auth.aws.allowed_account_ids")
	_ = v.BindEnv("auth.aws.team_tag_key")
	_ = v.BindEnv("auth.aws.cache_ttl")
	_ = v.BindEnv("auth.aws.user_mapping.default_role")
	_ = v.BindEnv("auth.aws.user_mapping.default_permissions")

	// Other configuration
	_ = v.BindEnv("auth_config_file")

	// Role-based environment files configuration
	_ = v.BindEnv("role_env_files.enabled")
	_ = v.BindEnv("role_env_files.path")
	_ = v.BindEnv("role_env_files.load_default")

	// Kubernetes session configuration
	_ = v.BindEnv("kubernetes_session.namespace", "AGENTAPI_K8S_SESSION_NAMESPACE")
	_ = v.BindEnv("kubernetes_session.image", "AGENTAPI_K8S_SESSION_IMAGE")
	_ = v.BindEnv("kubernetes_session.image_pull_policy", "AGENTAPI_K8S_SESSION_IMAGE_PULL_POLICY")
	_ = v.BindEnv("kubernetes_session.service_account", "AGENTAPI_K8S_SESSION_SERVICE_ACCOUNT")
	_ = v.BindEnv("kubernetes_session.base_port", "AGENTAPI_K8S_SESSION_BASE_PORT")
	_ = v.BindEnv("kubernetes_session.cpu_request", "AGENTAPI_K8S_SESSION_CPU_REQUEST")
	_ = v.BindEnv("kubernetes_session.cpu_limit", "AGENTAPI_K8S_SESSION_CPU_LIMIT")
	_ = v.BindEnv("kubernetes_session.memory_request", "AGENTAPI_K8S_SESSION_MEMORY_REQUEST")
	_ = v.BindEnv("kubernetes_session.memory_limit", "AGENTAPI_K8S_SESSION_MEMORY_LIMIT")
	_ = v.BindEnv("kubernetes_session.pvc_enabled", "AGENTAPI_K8S_SESSION_PVC_ENABLED")
	_ = v.BindEnv("kubernetes_session.pvc_storage_class", "AGENTAPI_K8S_SESSION_PVC_STORAGE_CLASS")
	_ = v.BindEnv("kubernetes_session.pvc_storage_size", "AGENTAPI_K8S_SESSION_PVC_STORAGE_SIZE")
	_ = v.BindEnv("kubernetes_session.pod_start_timeout", "AGENTAPI_K8S_SESSION_POD_START_TIMEOUT")
	_ = v.BindEnv("kubernetes_session.pod_stop_timeout", "AGENTAPI_K8S_SESSION_POD_STOP_TIMEOUT")
	_ = v.BindEnv("kubernetes_session.claude_config_base_secret", "AGENTAPI_K8S_SESSION_CLAUDE_CONFIG_BASE_SECRET")
	_ = v.BindEnv("kubernetes_session.claude_config_user_configmap_prefix", "AGENTAPI_K8S_SESSION_CLAUDE_CONFIG_USER_CONFIGMAP_PREFIX")
	_ = v.BindEnv("kubernetes_session.init_container_image", "AGENTAPI_K8S_SESSION_INIT_CONTAINER_IMAGE")
	_ = v.BindEnv("kubernetes_session.github_secret_name", "AGENTAPI_K8S_SESSION_GITHUB_SECRET_NAME")
	_ = v.BindEnv("kubernetes_session.github_config_secret_name", "AGENTAPI_K8S_SESSION_GITHUB_CONFIG_SECRET_NAME")
	_ = v.BindEnv("kubernetes_session.config_file", "AGENTAPI_K8S_SESSION_CONFIG_FILE")

	// MCP servers configuration

	// Settings base secret configuration
	_ = v.BindEnv("kubernetes_session.settings_base_secret", "AGENTAPI_K8S_SESSION_SETTINGS_BASE_SECRET")

	// OpenTelemetry Collector configuration
	_ = v.BindEnv("kubernetes_session.otel_collector_enabled", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_ENABLED")
	_ = v.BindEnv("kubernetes_session.otel_collector_image", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_IMAGE")
	_ = v.BindEnv("kubernetes_session.otel_collector_scrape_interval", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_SCRAPE_INTERVAL")
	_ = v.BindEnv("kubernetes_session.otel_collector_claude_code_port", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_CLAUDE_CODE_PORT")
	_ = v.BindEnv("kubernetes_session.otel_collector_exporter_port", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_EXPORTER_PORT")
	_ = v.BindEnv("kubernetes_session.otel_collector_cpu_request", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_CPU_REQUEST")
	_ = v.BindEnv("kubernetes_session.otel_collector_cpu_limit", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_CPU_LIMIT")
	_ = v.BindEnv("kubernetes_session.otel_collector_memory_request", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_MEMORY_REQUEST")
	_ = v.BindEnv("kubernetes_session.otel_collector_memory_limit", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_MEMORY_LIMIT")

	// Slack Integration configuration
	_ = v.BindEnv("kubernetes_session.slack_integration_image", "AGENTAPI_KUBERNETES_SESSION_SLACK_INTEGRATION_IMAGE")
	_ = v.BindEnv("kubernetes_session.slack_bot_token_secret_name", "AGENTAPI_KUBERNETES_SESSION_SLACK_BOT_TOKEN_SECRET_NAME")
	_ = v.BindEnv("kubernetes_session.slack_bot_token_secret_key", "AGENTAPI_KUBERNETES_SESSION_SLACK_BOT_TOKEN_SECRET_KEY")

	// Schedule worker configuration
	_ = v.BindEnv("schedule_worker.enabled", "AGENTAPI_SCHEDULE_WORKER_ENABLED")
	_ = v.BindEnv("schedule_worker.check_interval", "AGENTAPI_SCHEDULE_WORKER_CHECK_INTERVAL")
	_ = v.BindEnv("schedule_worker.namespace", "AGENTAPI_SCHEDULE_WORKER_NAMESPACE")
	_ = v.BindEnv("schedule_worker.lease_duration", "AGENTAPI_SCHEDULE_WORKER_LEASE_DURATION")
	_ = v.BindEnv("schedule_worker.renew_deadline", "AGENTAPI_SCHEDULE_WORKER_RENEW_DEADLINE")
	_ = v.BindEnv("schedule_worker.retry_period", "AGENTAPI_SCHEDULE_WORKER_RETRY_PERIOD")

	// Webhook configuration
	_ = v.BindEnv("webhook.base_url", "AGENTAPI_WEBHOOK_BASE_URL")
	_ = v.BindEnv("webhook.github_enterprise_host", "AGENTAPI_WEBHOOK_GITHUB_ENTERPRISE_HOST")

	// Memory backend configuration
	_ = v.BindEnv("memory.backend", "AGENTAPI_MEMORY_BACKEND")
	_ = v.BindEnv("memory.s3.bucket", "AGENTAPI_MEMORY_S3_BUCKET")
	_ = v.BindEnv("memory.s3.region", "AGENTAPI_MEMORY_S3_REGION")
	_ = v.BindEnv("memory.s3.prefix", "AGENTAPI_MEMORY_S3_PREFIX")
	_ = v.BindEnv("memory.s3.endpoint", "AGENTAPI_MEMORY_S3_ENDPOINT")
}

// setDefaults sets default values for viper configuration
func setDefaults(v *viper.Viper) {
	// Auth defaults
	v.SetDefault("auth.static.enabled", false)
	v.SetDefault("auth.static.header_name", "X-API-Key")
	v.SetDefault("auth.github.enabled", false)
	v.SetDefault("auth.github.base_url", "https://api.github.com")
	v.SetDefault("auth.github.token_header", "Authorization")
	v.SetDefault("auth.github.oauth.client_id", "")
	v.SetDefault("auth.github.oauth.client_secret", "")
	v.SetDefault("auth.github.oauth.scope", "read:user read:org")
	v.SetDefault("auth.github.oauth.base_url", "")

	// AWS auth defaults
	v.SetDefault("auth.aws.enabled", false)
	v.SetDefault("auth.aws.region", "ap-northeast-1")
	v.SetDefault("auth.aws.allowed_account_ids", []string{})
	v.SetDefault("auth.aws.team_tag_key", "Team")
	v.SetDefault("auth.aws.cache_ttl", "1h")

	// Role-based environment files defaults
	v.SetDefault("role_env_files.enabled", false)
	v.SetDefault("role_env_files.path", "/etc/agentapi/env")
	v.SetDefault("role_env_files.load_default", true)

	// Kubernetes session defaults
	v.SetDefault("kubernetes_session.namespace", "")
	v.SetDefault("kubernetes_session.image", "")
	v.SetDefault("kubernetes_session.image_pull_policy", "IfNotPresent")
	v.SetDefault("kubernetes_session.service_account", "agentapi-proxy")
	v.SetDefault("kubernetes_session.base_port", 9000)
	v.SetDefault("kubernetes_session.cpu_request", "500m")
	v.SetDefault("kubernetes_session.cpu_limit", "2")
	v.SetDefault("kubernetes_session.memory_request", "512Mi")
	v.SetDefault("kubernetes_session.memory_limit", "4Gi")
	v.SetDefault("kubernetes_session.pvc_enabled", true)
	v.SetDefault("kubernetes_session.pvc_storage_class", "")
	v.SetDefault("kubernetes_session.pvc_storage_size", "10Gi")
	v.SetDefault("kubernetes_session.pod_start_timeout", 120)
	v.SetDefault("kubernetes_session.pod_stop_timeout", 30)
	v.SetDefault("kubernetes_session.claude_config_base_secret", "claude-config-base")
	v.SetDefault("kubernetes_session.claude_config_user_configmap_prefix", "claude-config")
	v.SetDefault("kubernetes_session.init_container_image", "")
	v.SetDefault("kubernetes_session.github_secret_name", "")

	// MCP servers defaults

	// Settings base secret default (used for proxy-side merge of settings.json and mcp-servers.json)
	v.SetDefault("kubernetes_session.settings_base_secret", "agentapi-settings-base")

	// Schedule worker defaults
	v.SetDefault("schedule_worker.enabled", true)
	v.SetDefault("schedule_worker.check_interval", "30s")
	v.SetDefault("schedule_worker.namespace", "")
	v.SetDefault("schedule_worker.lease_duration", "15s")
	v.SetDefault("schedule_worker.renew_deadline", "10s")
	v.SetDefault("schedule_worker.retry_period", "2s")

	// Webhook defaults
	v.SetDefault("webhook.base_url", "")
	v.SetDefault("webhook.github_enterprise_host", "")

	// Memory backend defaults
	v.SetDefault("memory.backend", "kubernetes")
	v.SetDefault("memory.s3.prefix", "agentapi-memory/")
	v.SetDefault("memory.s3.region", "")
	v.SetDefault("memory.s3.endpoint", "")
}

// applyConfigDefaults applies default values to any unset configuration fields
func applyConfigDefaults(config *Config) {

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
	if config.Auth.AWS != nil {
		if config.Auth.AWS.Region == "" {
			config.Auth.AWS.Region = "ap-northeast-1"
		}
		if config.Auth.AWS.TeamTagKey == "" {
			config.Auth.AWS.TeamTagKey = "Team"
		}
		if config.Auth.AWS.CacheTTL == "" {
			config.Auth.AWS.CacheTTL = "1h"
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

	// Log role-based environment files configuration
	if config.RoleEnvFiles.Enabled {
		log.Printf("[CONFIG] Role-based environment files enabled")
		log.Printf("[CONFIG] Environment files path: %s", config.RoleEnvFiles.Path)
		log.Printf("[CONFIG] Load default.env: %v", config.RoleEnvFiles.LoadDefault)
	}

	// Load API keys from external file if specified
	if config.Auth.Static != nil && config.Auth.Static.KeysFile != "" {
		if err := config.loadAPIKeysFromFile(); err != nil {
			log.Printf("Warning: Failed to load API keys from %s: %v", config.Auth.Static.KeysFile, err)
		}
	}

	// Load kubernetes session config from external file if specified
	if config.KubernetesSession.ConfigFile != "" {
		if err := loadK8sSessionConfigFromFile(config, config.KubernetesSession.ConfigFile); err != nil {
			log.Printf("[CONFIG] Warning: Failed to load kubernetes session config from %s: %v", config.KubernetesSession.ConfigFile, err)
		} else {
			log.Printf("[CONFIG] Loaded kubernetes session config from: %s", config.KubernetesSession.ConfigFile)
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
		Auth: AuthConfig{
			Static: &StaticAuthConfig{
				Enabled:    false,
				HeaderName: "X-API-Key",
				APIKeys:    []APIKey{},
			},
		},
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
	if c.Auth.Static == nil || !c.Auth.Static.Enabled {
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
			log.Printf("[CONFIG] Applying GitHub user mapping from external config:")
			log.Printf("[CONFIG]   Default role: %s", authOverride.GitHub.UserMapping.DefaultRole)
			log.Printf("[CONFIG]   Default permissions: %v", authOverride.GitHub.UserMapping.DefaultPermissions)
			log.Printf("[CONFIG]   Team role mappings: %+v", authOverride.GitHub.UserMapping.TeamRoleMapping)

			config.Auth.GitHub.UserMapping = *authOverride.GitHub.UserMapping
			log.Printf("[CONFIG] Applied GitHub user mapping from external config")

			// Verify the configuration was applied
			log.Printf("[CONFIG] After applying - Default role: %s", config.Auth.GitHub.UserMapping.DefaultRole)
			log.Printf("[CONFIG] After applying - Default permissions: %v", config.Auth.GitHub.UserMapping.DefaultPermissions)
			log.Printf("[CONFIG] After applying - Team role mappings: %+v", config.Auth.GitHub.UserMapping.TeamRoleMapping)
		}
	} else {
		log.Printf("[CONFIG] No GitHub config found in auth override file")
	}

	return nil
}

// K8sSessionConfigOverride represents kubernetes session configuration overrides from external file
type K8sSessionConfigOverride struct {
	KubernetesSession *struct {
		NodeSelector map[string]string `json:"node_selector,omitempty" yaml:"node_selector"`
		Tolerations  []Toleration      `json:"tolerations,omitempty" yaml:"tolerations"`
	} `json:"kubernetes_session,omitempty" yaml:"kubernetes_session"`
}

// loadK8sSessionConfigFromFile loads kubernetes session configuration from an external file
func loadK8sSessionConfigFromFile(config *Config, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close kubernetes session config file: %v", err)
		}
	}()

	var k8sOverride K8sSessionConfigOverride

	// Determine file format based on extension
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		decoder := yaml.NewDecoder(file)
		if err := decoder.Decode(&k8sOverride); err != nil {
			return err
		}
	} else {
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&k8sOverride); err != nil {
			return err
		}
	}

	// Apply overrides to the main config
	if k8sOverride.KubernetesSession != nil {
		if k8sOverride.KubernetesSession.NodeSelector != nil {
			config.KubernetesSession.NodeSelector = k8sOverride.KubernetesSession.NodeSelector
			log.Printf("[CONFIG] Applied kubernetes session node_selector: %v", config.KubernetesSession.NodeSelector)
		}
		if k8sOverride.KubernetesSession.Tolerations != nil {
			config.KubernetesSession.Tolerations = k8sOverride.KubernetesSession.Tolerations
			log.Printf("[CONFIG] Applied kubernetes session tolerations: %+v", config.KubernetesSession.Tolerations)
		}
	}

	return nil
}
