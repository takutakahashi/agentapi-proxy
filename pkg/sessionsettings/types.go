package sessionsettings

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileTypeCodexAuth is the type name for ~/.codex/auth.json (Codex CLI credentials).
const FileTypeCodexAuth = "codex_auth"

// FileTypeClaudeCredentials is the type name for ~/.claude/.credentials.json (Claude Code credentials).
const FileTypeClaudeCredentials = "claude_credentials"

// ManagedFileTypes maps a file type name to its absolute path inside the agent container.
// This is the single source of truth shared by the provisioner (runFilesSync) and
// the credentials controller (PUT /credentials/{name}).
var ManagedFileTypes = map[string]string{
	FileTypeCodexAuth:         "/home/agentapi/.codex/auth.json",
	FileTypeClaudeCredentials: "/home/agentapi/.claude/.credentials.json",
}

// ManagedFileTypeOrder defines the canonical ordering of file types used when
// serialising ManagedFile slices to/from Kubernetes Secret data.
var ManagedFileTypeOrder = []string{
	FileTypeCodexAuth,
	FileTypeClaudeCredentials,
}

// ManagedFile represents a file path and its contents, used to persist arbitrary
// files across sessions via the agentapi-agent-files-{userID} Kubernetes Secret.
type ManagedFile struct {
	Path    string `yaml:"path"    json:"path"`
	Content string `yaml:"content" json:"content"`
}

// FilesToSecretData converts a slice of ManagedFile into a flat map suitable for
// storing in a Kubernetes Secret.  The format is index-based:
//
//	"0.path"    → files[0].Path
//	"0.content" → files[0].Content
//	"1.path"    → files[1].Path
//	…
func FilesToSecretData(files []ManagedFile) map[string][]byte {
	data := make(map[string][]byte, len(files)*2)
	for i, f := range files {
		prefix := strconv.Itoa(i)
		data[prefix+".path"] = []byte(f.Path)
		data[prefix+".content"] = []byte(f.Content)
	}
	return data
}

// SecretDataToFiles reconstructs a slice of ManagedFile from the flat index-based
// map produced by FilesToSecretData.  Entries that do not match the expected format
// are silently skipped.
func SecretDataToFiles(data map[string][]byte) []ManagedFile {
	// Collect unique indices first.
	indices := map[int]struct{}{}
	for k := range data {
		if idx, ok := parseFileSecretKey(k); ok {
			indices[idx] = struct{}{}
		}
	}

	files := make([]ManagedFile, 0, len(indices))
	for idx := range indices {
		prefix := strconv.Itoa(idx)
		path, hasPath := data[prefix+".path"]
		content, hasContent := data[prefix+".content"]
		if !hasPath || !hasContent {
			continue
		}
		files = append(files, ManagedFile{
			Path:    string(path),
			Content: string(content),
		})
	}
	return files
}

// parseFileSecretKey parses a key of the form "<index>.path" or "<index>.content"
// and returns the index.  Returns (0, false) if the key does not match.
func parseFileSecretKey(k string) (int, bool) {
	dot := strings.LastIndex(k, ".")
	if dot < 0 {
		return 0, false
	}
	suffix := k[dot+1:]
	if suffix != "path" && suffix != "content" {
		return 0, false
	}
	idx, err := strconv.Atoi(k[:dot])
	if err != nil {
		return 0, false
	}
	return idx, true
}

// SessionSettings is the top-level unified settings YAML structure.
// It consolidates all configuration needed for a session Pod.
type SessionSettings struct {
	Session        SessionMeta          `yaml:"session"                   json:"session"`
	Env            map[string]string    `yaml:"env,omitempty"             json:"env,omitempty"`
	Claude         ClaudeConfig         `yaml:"claude,omitempty"          json:"claude,omitempty"`
	Repository     *RepositoryConfig    `yaml:"repository,omitempty"      json:"repository,omitempty"`
	InitialMessage string               `yaml:"initial_message,omitempty" json:"initial_message,omitempty"`
	WebhookPayload string               `yaml:"webhook_payload,omitempty" json:"webhook_payload,omitempty"`
	Startup        StartupConfig        `yaml:"startup,omitempty"         json:"startup,omitempty"`
	Github         *GithubConfig        `yaml:"github,omitempty"          json:"github,omitempty"`
	SlackParams    *SlackParams         `yaml:"slack_params,omitempty"    json:"slack_params,omitempty"`
	OtelCollector  *OtelCollectorConfig `yaml:"otel_collector,omitempty"  json:"otel_collector,omitempty"`
	// Files holds the managed files to be restored at session startup.
	// They are read from the agentapi-agent-files-{userID} Secret at session creation
	// time and written to their respective paths by the provisioner.
	// The runFilesSync goroutine watches those paths and syncs changes back to the Secret.
	Files []ManagedFile `yaml:"files,omitempty" json:"files,omitempty"`
	// Credentials is deprecated: use Files instead.
	// Kept for backward compatibility with sessions provisioned before the Files field
	// was introduced.  The provisioner falls back to this field when Files is empty.
	Credentials string `yaml:"credentials,omitempty" json:"credentials,omitempty"`
}

// OtelCollectorConfig holds OpenTelemetry Collector configuration for in-process mode.
// When set, the provisioner will launch otelcol as a subprocess after user context
// is established, ensuring metrics labels (user_id, session_id, etc.) are correct
// even when using the stock inventory feature.
type OtelCollectorConfig struct {
	Enabled        bool   `yaml:"enabled"          json:"enabled"`
	ScrapeInterval string `yaml:"scrape_interval"  json:"scrape_interval"`
	ClaudeCodePort int    `yaml:"claude_code_port" json:"claude_code_port"`
	ExporterPort   int    `yaml:"exporter_port"    json:"exporter_port"`
	// Label values resolved at session creation time (not startup time)
	SessionID  string `yaml:"session_id"  json:"session_id"`
	UserID     string `yaml:"user_id"     json:"user_id"`
	TeamID     string `yaml:"team_id"     json:"team_id"`
	ScheduleID string `yaml:"schedule_id" json:"schedule_id"`
	WebhookID  string `yaml:"webhook_id"  json:"webhook_id"`
	AgentType  string `yaml:"agent_type"  json:"agent_type"`
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
