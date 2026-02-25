package services

import (
	"context"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// deepMergeAgentapiSettings merges a slice of *agentapiSettingsJSON in priority order
// (lowest priority first, highest priority last). Later entries override earlier entries.
// nil entries in the slice are safely skipped.
//
// Merge rules per field:
//   - EnvVars (map):               union merge; later entry wins on key conflict
//   - MCPServers (map):            union merge; later entry wins on name conflict
//   - AuthMode (string):           later non-empty value wins; empty does not override
//   - ClaudeCodeOAuthToken (string): later non-empty value wins; empty does not override
//   - Bedrock (struct):            if later entry has nil Bedrock, earlier is kept;
//     if both are non-nil, fields are merged individually:
//     Enabled (bool) — later always wins (false is explicit, not "unset");
//     string fields   — later non-empty wins; empty does not override
func deepMergeAgentapiSettings(sources []*agentapiSettingsJSON) *agentapiSettingsJSON {
	result := &agentapiSettingsJSON{
		EnvVars:    make(map[string]string),
		MCPServers: make(map[string]*agentapiMCPServerJSON),
	}

	for _, src := range sources {
		if src == nil {
			continue
		}

		// EnvVars: union, later wins on conflict
		for k, v := range src.EnvVars {
			result.EnvVars[k] = v
		}

		// MCPServers: union, later wins on name conflict
		for name, server := range src.MCPServers {
			result.MCPServers[name] = server
		}

		// AuthMode: later non-empty wins
		if src.AuthMode != "" {
			result.AuthMode = src.AuthMode
		}

		// ClaudeCodeOAuthToken: later non-empty wins
		if src.ClaudeCodeOAuthToken != "" {
			result.ClaudeCodeOAuthToken = src.ClaudeCodeOAuthToken
		}

		// Bedrock: per-field merge
		if src.Bedrock != nil {
			if result.Bedrock == nil {
				// No existing bedrock block — copy the entire block from src
				result.Bedrock = &agentapiBedrockJSON{
					Enabled:         src.Bedrock.Enabled,
					Model:           src.Bedrock.Model,
					AccessKeyID:     src.Bedrock.AccessKeyID,
					SecretAccessKey: src.Bedrock.SecretAccessKey,
					RoleARN:         src.Bedrock.RoleARN,
					Profile:         src.Bedrock.Profile,
				}
			} else {
				// Both have a Bedrock block — merge field by field.
				// Enabled: later always wins (even false is an explicit override).
				result.Bedrock.Enabled = src.Bedrock.Enabled
				// String fields: later non-empty wins; empty means "keep earlier value"
				if src.Bedrock.Model != "" {
					result.Bedrock.Model = src.Bedrock.Model
				}
				if src.Bedrock.AccessKeyID != "" {
					result.Bedrock.AccessKeyID = src.Bedrock.AccessKeyID
				}
				if src.Bedrock.SecretAccessKey != "" {
					result.Bedrock.SecretAccessKey = src.Bedrock.SecretAccessKey
				}
				if src.Bedrock.RoleARN != "" {
					result.Bedrock.RoleARN = src.Bedrock.RoleARN
				}
				if src.Bedrock.Profile != "" {
					result.Bedrock.Profile = src.Bedrock.Profile
				}
			}
		}
	}

	// Normalise: set nil instead of empty maps to match the behaviour of
	// readAgentapiSettingsSecret (which leaves these nil when absent in JSON).
	if len(result.EnvVars) == 0 {
		result.EnvVars = nil
	}
	if len(result.MCPServers) == 0 {
		result.MCPServers = nil
	}

	return result
}

// collectAgentapiSettingsSources reads agentapi-settings-* Kubernetes Secrets for the
// sources relevant to the given request and returns them in ascending priority order
// (index 0 = lowest priority, last index = highest priority).
//
// Priority rules:
//   - User-scoped sessions:  req.Teams (in order) → req.UserID (highest)
//   - Team-scoped sessions:  req.TeamID only (user settings are NOT applied)
//
// The base secret (SettingsBaseSecret) is intentionally excluded because it is meant
// for system-wide Claude config (MCP servers, marketplaces, plugins), not for
// env_vars / auth credentials. The base secret is handled separately inside
// mergeSettingsAndMCP().
func (m *KubernetesSessionManager) collectAgentapiSettingsSources(
	ctx context.Context,
	req *entities.RunServerRequest,
) []*agentapiSettingsJSON {
	var sources []*agentapiSettingsJSON

	if req.Scope == entities.ScopeTeam {
		// Team-scoped: only the team's own settings; user settings are not applied.
		if req.TeamID != "" {
			secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.TeamID))
			sources = append(sources, m.readAgentapiSettingsSecret(ctx, secretName))
		}
	} else {
		// User-scoped: teams first (lower priority), then the requesting user (highest).
		for _, team := range req.Teams {
			secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(team))
			sources = append(sources, m.readAgentapiSettingsSecret(ctx, secretName))
		}
		if req.UserID != "" {
			secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.UserID))
			sources = append(sources, m.readAgentapiSettingsSecret(ctx, secretName))
		}
	}

	return sources
}
