// Package settingspatch provides a unified type and merge algorithm for agentapi-proxy settings.
//
// # Design
//
// Every settings layer (base, team, user, oneshot) is represented as a SettingsPatch.
// The JSON format is identical to the format stored in Kubernetes Secrets, so no
// conversion is needed: json.Unmarshal(secretData, &patch) works directly.
//
// Merge semantics per field type:
//   - String fields:  "" = "not set, inherit from lower layer"; non-empty = override.
//   - Pointer fields: nil = "not set, inherit from lower layer"; non-nil = override.
//   - Map fields:     absent key = inherit; nil value = explicitly delete; non-nil = override.
//   - Slice fields:   accumulated (union) across all layers (plugins).
package settingspatch

// SettingsPatch represents a single layer of settings configuration.
// It is the canonical type for both storage and merge operations.
//
// Layers are resolved by Resolve() from lowest to highest priority:
//
//	base → team[0] → team[1] → ... → user → oneshot
type SettingsPatch struct {
	// AuthMode specifies the authentication mode: "oauth" or "bedrock".
	// "" = inherit from lower layer (do not override auth-related env vars).
	AuthMode string `json:"auth_mode,omitempty"`

	// OAuthToken is the Claude Code OAuth token.
	// "" = inherit from lower layer.
	OAuthToken string `json:"claude_code_oauth_token,omitempty"`

	// Bedrock holds AWS Bedrock configuration.
	// nil = inherit from lower layer entirely.
	// Non-nil = merge field by field (empty sub-fields still inherit).
	Bedrock *BedrockPatch `json:"bedrock,omitempty"`

	// MCPServers is the MCP server configuration map.
	// Absent key = inherit from lower layer.
	// nil value = explicitly delete server.
	// Non-nil value = override/add server.
	MCPServers map[string]*MCPServerPatch `json:"mcp_servers,omitempty"`

	// EnvVars are custom environment variables.
	// Absent key = inherit from lower layer.
	// Last non-absent value wins (no delete semantics).
	EnvVars map[string]string `json:"env_vars,omitempty"`

	// Marketplaces is the plugin marketplace configuration map.
	// Absent key = inherit from lower layer.
	// nil value = explicitly delete marketplace.
	// Non-nil value = override/add marketplace.
	Marketplaces map[string]*MarketplacePatch `json:"marketplaces,omitempty"`

	// Hooks is a map of event name → hook configuration.
	// Later layers override earlier layers for the same event name.
	Hooks map[string]interface{} `json:"hooks,omitempty"`

	// EnabledPlugins lists plugins contributed by this layer.
	// Accumulated (union) across all layers.
	EnabledPlugins []string `json:"enabled_plugins,omitempty"`

	// PreferredTeamID specifies which team's settings to use exclusively.
	// "" = use all teams in the default order.
	// Non-empty = use only this team's settings (skip all other teams).
	PreferredTeamID string `json:"preferred_team_id,omitempty"`

	// MemorySummarizeDrafts controls whether draft memories are automatically
	// summarized into main memory when the session ends.
	// nil = inherit from lower layer (default: disabled).
	// true = enable draft summarization.
	// false = explicitly disable draft summarization.
	MemorySummarizeDrafts *bool `json:"memory_summarize_drafts,omitempty"`

	// MemoryEnabled controls whether memory integration is active for sessions.
	// nil = inherit from lower layer (default: enabled when memory_key is available).
	// true = explicitly enable memory integration.
	// false = explicitly disable memory integration (memory_key is ignored even if set).
	MemoryEnabled *bool `json:"memory_enabled,omitempty"`
}

// BedrockPatch holds AWS Bedrock configuration.
// Empty string fields are treated as "not set" and inherit from lower layers.
// Bedrock activation is controlled solely by AuthMode ("bedrock" = on, "oauth" = off).
// Legacy fields like "enabled" stored in Kubernetes Secrets are silently ignored.
type BedrockPatch struct {
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// MCPServerPatch represents a single MCP server configuration.
// All fields are replaced as a whole when the server key is present.
type MCPServerPatch struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MarketplacePatch represents a single plugin marketplace configuration.
type MarketplacePatch struct {
	URL string `json:"url"`
}
