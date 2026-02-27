// Package settingspatch provides a unified type and merge algorithm for agentapi-proxy settings.
//
// # Design: JSON Merge Patch (RFC 7396)
//
// Every settings layer (base, team, user, oneshot) is represented as a SettingsPatch.
// Merging is done by Apply() / Resolve(), and the result is converted to runtime
// configuration by Materialize().
//
// Merge semantics per field type:
//   - Pointer fields:   nil = "not set, inherit from lower layer"; non-nil = override.
//   - Map fields:       absent key = inherit; nil value = explicitly delete; non-nil = override.
//   - Slice fields:     accumulated (union) across all layers (plugins).
package settingspatch

// SettingsPatch represents a single layer of settings configuration.
// It is the canonical type for both storage and merge operations.
//
// Layers are resolved by Resolve() from lowest to highest priority:
//
//	base → team[0] → team[1] → ... → user → oneshot
type SettingsPatch struct {
	// AuthMode specifies the authentication mode: "oauth" or "bedrock".
	// nil = inherit from lower layer (do not override auth-related env vars).
	AuthMode *string `json:"auth_mode,omitempty"`

	// OAuthToken is the Claude Code OAuth token.
	// nil = inherit from lower layer.
	OAuthToken *string `json:"claude_code_oauth_token,omitempty"`

	// Bedrock holds AWS Bedrock configuration.
	// nil = inherit from lower layer entirely.
	// Non-nil = merge field by field (nil sub-fields still inherit).
	Bedrock *BedrockPatch `json:"bedrock,omitempty"`

	// MCPServers is the MCP server configuration map.
	// Absent key = inherit from lower layer.
	// nil value = explicitly delete server.
	// Non-nil value = override/add server.
	MCPServers map[string]*MCPServerPatch `json:"mcp_servers,omitempty"`

	// EnvVars are custom environment variables.
	// Absent key = inherit from lower layer.
	// nil value = explicitly delete variable.
	// Non-nil value = set/override variable.
	EnvVars map[string]*string `json:"env_vars,omitempty"`

	// Marketplaces is the plugin marketplace configuration map.
	// Absent key = inherit from lower layer.
	// nil value = explicitly delete marketplace.
	// Non-nil value = override/add marketplace.
	Marketplaces map[string]*MarketplacePatch `json:"marketplaces,omitempty"`

	// Hooks is a map of event name → hook configuration.
	// Later layers override earlier layers for the same event name.
	Hooks map[string]interface{} `json:"hooks,omitempty"`

	// AddPlugins lists plugins contributed by this layer.
	// Accumulated (union) across all layers.
	AddPlugins []string `json:"add_plugins,omitempty"`

	// RemovePlugins lists plugins to subtract from the accumulated set.
	// Applied after the union of all AddPlugins across all layers.
	RemovePlugins []string `json:"remove_plugins,omitempty"`
}

// BedrockPatch holds AWS Bedrock configuration.
// Pointer fields: nil = inherit from lower layer, non-nil = override.
type BedrockPatch struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	Model           *string `json:"model,omitempty"`
	AccessKeyID     *string `json:"access_key_id,omitempty"`
	SecretAccessKey *string `json:"secret_access_key,omitempty"`
	RoleARN         *string `json:"role_arn,omitempty"`
	Profile         *string `json:"profile,omitempty"`
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

// ptr returns a pointer to v. Helper for constructing patches in tests.
func ptr[T any](v T) *T { return &v }
