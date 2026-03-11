package sessionsettings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SessionSettings is the top-level unified settings YAML structure.
// It consolidates all configuration needed for a session Pod.
type SessionSettings struct {
	Session        SessionMeta       `yaml:"session"                   json:"session"`
	Env            map[string]string `yaml:"env,omitempty"             json:"env,omitempty"`
	Claude         ClaudeConfig      `yaml:"claude,omitempty"          json:"claude,omitempty"`
	Repository     *RepositoryConfig `yaml:"repository,omitempty"      json:"repository,omitempty"`
	InitialMessage string            `yaml:"initial_message,omitempty" json:"initial_message,omitempty"`
	WebhookPayload string            `yaml:"webhook_payload,omitempty" json:"webhook_payload,omitempty"`
	Startup        StartupConfig     `yaml:"startup,omitempty"         json:"startup,omitempty"`
	Github         *GithubConfig     `yaml:"github,omitempty"          json:"github,omitempty"`
	SlackParams    *SlackParams      `yaml:"slack_params,omitempty"    json:"slack_params,omitempty"`
}

// SlackParams holds Slack integration parameters for the provisioner subprocess.
// When set, the provisioner will launch claude-posts as a subprocess to forward
// agent output to the specified Slack channel/thread.
type SlackParams struct {
	Channel  string `yaml:"channel"            json:"channel"`
	ThreadTS string `yaml:"thread_ts,omitempty" json:"thread_ts,omitempty"`
	BotToken string `yaml:"bot_token"          json:"bot_token"`
}

// SessionMeta contains session identification metadata.
type SessionMeta struct {
	ID        string            `yaml:"id"                  json:"id"`
	UserID    string            `yaml:"user_id"             json:"user_id"`
	Scope     string            `yaml:"scope"               json:"scope"`
	TeamID    string            `yaml:"team_id,omitempty"   json:"team_id,omitempty"`
	AgentType string            `yaml:"agent_type,omitempty" json:"agent_type,omitempty"`
	Oneshot   bool              `yaml:"oneshot,omitempty"   json:"oneshot,omitempty"`
	Teams     []string          `yaml:"teams,omitempty"     json:"teams,omitempty"`
	MemoryKey map[string]string `yaml:"memory_key,omitempty" json:"memory_key,omitempty"`
}

// ClaudeConfig holds Claude-related configuration data.
type ClaudeConfig struct {
	ClaudeJSON   map[string]interface{} `yaml:"claude_json,omitempty"   json:"claude_json,omitempty"`
	SettingsJSON map[string]interface{} `yaml:"settings_json,omitempty" json:"settings_json,omitempty"`
	MCPServers   map[string]interface{} `yaml:"mcp_servers,omitempty"   json:"mcp_servers,omitempty"`
}

// RepositoryConfig holds repository information.
type RepositoryConfig struct {
	FullName string `yaml:"fullname"   json:"fullname"`
	CloneDir string `yaml:"clone_dir"  json:"clone_dir"`
}

// StartupConfig holds the startup command configuration.
type StartupConfig struct {
	Command []string `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"    json:"args,omitempty"`
}

// GithubConfig holds GitHub authentication configuration reference info.
type GithubConfig struct {
	Token            string `yaml:"token,omitempty"              json:"token,omitempty"`
	SecretName       string `yaml:"secret_name,omitempty"        json:"secret_name,omitempty"`
	ConfigSecretName string `yaml:"config_secret_name,omitempty" json:"config_secret_name,omitempty"`
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

// LoadSettingsFromBytes parses a SessionSettings from YAML bytes.
func LoadSettingsFromBytes(data []byte) (*SessionSettings, error) {
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
