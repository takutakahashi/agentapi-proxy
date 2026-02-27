package settingspatch

import "encoding/json"

// legacySettingsJSON is the JSON format currently stored in Kubernetes Secrets
// (agentapi-settings-* secrets, settings.json key).
// Used only for reading existing data; new writes use SettingsPatch directly.
type legacySettingsJSON struct {
	Name                 string                            `json:"name,omitempty"`
	Bedrock              *legacyBedrockJSON                `json:"bedrock,omitempty"`
	MCPServers           map[string]*legacyMCPServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces         map[string]*legacyMarketplaceJSON `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken string                            `json:"claude_code_oauth_token,omitempty"`
	// AuthMode was stored as a plain string; empty = "not set".
	AuthMode       string   `json:"auth_mode,omitempty"`
	EnabledPlugins []string `json:"enabled_plugins,omitempty"`
	// EnvVars was stored as map[string]string; all present = set (no delete).
	EnvVars map[string]string      `json:"env_vars,omitempty"`
	Hooks   map[string]interface{} `json:"hooks,omitempty"`
}

type legacyBedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

type legacyMCPServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type legacyMarketplaceJSON struct {
	URL string `json:"url"`
}

// FromJSON parses raw JSON (settings.json Secret data) into a SettingsPatch.
// It handles both the current legacy format and the new SettingsPatch format.
//
// Key conversions from legacy format:
//   - auth_mode ""  → nil AuthMode  (was implicitly "not set")
//   - enabled_plugins → AddPlugins  (union semantics preserved)
//   - env_vars map[string]string → EnvVars map[string]*string  (all present = non-nil)
func FromJSON(data []byte) (SettingsPatch, error) {
	var legacy legacySettingsJSON
	if err := json.Unmarshal(data, &legacy); err != nil {
		return SettingsPatch{}, err
	}
	return fromLegacy(legacy), nil
}

// fromLegacy converts a legacySettingsJSON to a SettingsPatch.
func fromLegacy(legacy legacySettingsJSON) SettingsPatch {
	patch := SettingsPatch{}

	// auth_mode: empty string → nil (make "not set" explicit)
	if legacy.AuthMode != "" {
		mode := legacy.AuthMode
		patch.AuthMode = &mode
	}

	// OAuth token
	if legacy.ClaudeCodeOAuthToken != "" {
		token := legacy.ClaudeCodeOAuthToken
		patch.OAuthToken = &token
	}

	// Bedrock: convert scalar fields to pointers
	if legacy.Bedrock != nil {
		enabled := legacy.Bedrock.Enabled
		patch.Bedrock = &BedrockPatch{
			Enabled: &enabled,
		}
		if v := legacy.Bedrock.Model; v != "" {
			patch.Bedrock.Model = &v
		}
		if v := legacy.Bedrock.AccessKeyID; v != "" {
			patch.Bedrock.AccessKeyID = &v
		}
		if v := legacy.Bedrock.SecretAccessKey; v != "" {
			patch.Bedrock.SecretAccessKey = &v
		}
		if v := legacy.Bedrock.RoleARN; v != "" {
			patch.Bedrock.RoleARN = &v
		}
		if v := legacy.Bedrock.Profile; v != "" {
			patch.Bedrock.Profile = &v
		}
	}

	// MCP servers
	if len(legacy.MCPServers) > 0 {
		patch.MCPServers = make(map[string]*MCPServerPatch, len(legacy.MCPServers))
		for name, srv := range legacy.MCPServers {
			if srv == nil {
				continue
			}
			patch.MCPServers[name] = &MCPServerPatch{
				Type:    srv.Type,
				URL:     srv.URL,
				Command: srv.Command,
				Args:    srv.Args,
				Env:     srv.Env,
				Headers: srv.Headers,
			}
		}
	}

	// Marketplaces
	if len(legacy.Marketplaces) > 0 {
		patch.Marketplaces = make(map[string]*MarketplacePatch, len(legacy.Marketplaces))
		for name, mp := range legacy.Marketplaces {
			if mp == nil {
				continue
			}
			patch.Marketplaces[name] = &MarketplacePatch{URL: mp.URL}
		}
	}

	// enabled_plugins → AddPlugins (same union semantics)
	if len(legacy.EnabledPlugins) > 0 {
		patch.AddPlugins = make([]string, len(legacy.EnabledPlugins))
		copy(patch.AddPlugins, legacy.EnabledPlugins)
	}

	// env_vars: string → *string (all present values are set, no deletions in legacy format)
	if len(legacy.EnvVars) > 0 {
		patch.EnvVars = make(map[string]*string, len(legacy.EnvVars))
		for k, v := range legacy.EnvVars {
			val := v
			patch.EnvVars[k] = &val
		}
	}

	// Hooks
	if len(legacy.Hooks) > 0 {
		patch.Hooks = legacy.Hooks
	}

	return patch
}
