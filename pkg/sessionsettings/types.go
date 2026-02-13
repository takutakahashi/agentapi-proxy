package sessionsettings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SessionSettings is the top-level unified settings YAML structure.
// It consolidates all configuration needed for a session Pod.
type SessionSettings struct {
	Session        SessionMeta       `yaml:"session"`
	Env            map[string]string `yaml:"env,omitempty"`
	Claude         ClaudeConfig      `yaml:"claude,omitempty"`
	Repository     *RepositoryConfig `yaml:"repository,omitempty"`
	InitialMessage string            `yaml:"initial_message,omitempty"`
	WebhookPayload string            `yaml:"webhook_payload,omitempty"`
	Startup        StartupConfig     `yaml:"startup,omitempty"`
	Github         *GithubConfig     `yaml:"github,omitempty"`
}

// SessionMeta contains session identification metadata.
type SessionMeta struct {
	ID        string   `yaml:"id"`
	UserID    string   `yaml:"user_id"`
	Scope     string   `yaml:"scope"`
	TeamID    string   `yaml:"team_id,omitempty"`
	AgentType string   `yaml:"agent_type,omitempty"`
	Oneshot   bool     `yaml:"oneshot,omitempty"`
	Teams     []string `yaml:"teams,omitempty"`
}

// ClaudeConfig holds Claude-related configuration data.
type ClaudeConfig struct {
	ClaudeJSON   map[string]interface{} `yaml:"claude_json,omitempty"`
	SettingsJSON map[string]interface{} `yaml:"settings_json,omitempty"`
	MCPServers   map[string]interface{} `yaml:"mcp_servers,omitempty"`
}

// RepositoryConfig holds repository information.
type RepositoryConfig struct {
	FullName string `yaml:"fullname"`
	CloneDir string `yaml:"clone_dir"`
}

// StartupConfig holds the startup command configuration.
type StartupConfig struct {
	Command []string `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

// GithubConfig holds GitHub authentication configuration reference info.
type GithubConfig struct {
	Token            string `yaml:"token,omitempty"`
	SecretName       string `yaml:"secret_name,omitempty"`
	ConfigSecretName string `yaml:"config_secret_name,omitempty"`
}

// LoadSettings reads and parses a SessionSettings from a YAML file.
func LoadSettings(path string) (*SessionSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings SessionSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings YAML: %w", err)
	}

	return &settings, nil
}

// MarshalYAML marshals SessionSettings to YAML bytes.
func MarshalYAML(settings *SessionSettings) ([]byte, error) {
	return yaml.Marshal(settings)
}
