package settingspatch

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// MaterializedSettings is the resolved, ready-to-use session configuration
// produced by Materialize().
type MaterializedSettings struct {
	// EnvVars are the environment variables to inject into the session container.
	EnvVars map[string]string

	// SettingsJSON is the Claude settings JSON (marketplaces, plugins, hooks).
	// nil if no settings to apply.
	SettingsJSON map[string]interface{}

	// MCPServers is the resolved MCP server configuration.
	// nil if no MCP servers are configured.
	MCPServers map[string]interface{}

	// ActivePlugins is the final list of enabled plugins
	// (union of AddPlugins across all layers, minus RemovePlugins).
	ActivePlugins []string
}

// Materialize converts a resolved SettingsPatch into concrete session configuration.
//
// This is the single place where all business logic lives:
//   - Auth mode decision (bedrock vs oauth vs unset)
//   - Env var validation and dangerous-variable filtering
//   - Plugin set operations (AddPlugins minus RemovePlugins)
//   - Claude settings JSON assembly (marketplaces, plugins, hooks)
func Materialize(resolved SettingsPatch) (MaterializedSettings, error) {
	result := MaterializedSettings{
		EnvVars: make(map[string]string),
	}

	// 1. Custom env vars — applied first so auth overrides can trump them.
	for k, vPtr := range resolved.EnvVars {
		if vPtr != nil {
			result.EnvVars[k] = *vPtr
		}
	}
	result.EnvVars = validateEnvVars(result.EnvVars)

	// 2. Auth mode and credentials.
	//    nil AuthMode = "no layer configured auth" = do not touch auth env vars,
	//    so that externally-injected auth (e.g. node-level IAM) is preserved.
	if resolved.AuthMode != nil {
		switch *resolved.AuthMode {
		case "bedrock":
			result.EnvVars["CLAUDE_CODE_USE_BEDROCK"] = "1"
			if b := resolved.Bedrock; b != nil {
				if v := b.Model; v != nil && *v != "" {
					result.EnvVars["ANTHROPIC_MODEL"] = *v
				}
				if v := b.AccessKeyID; v != nil && *v != "" {
					result.EnvVars["AWS_ACCESS_KEY_ID"] = *v
				}
				if v := b.SecretAccessKey; v != nil && *v != "" {
					result.EnvVars["AWS_SECRET_ACCESS_KEY"] = *v
				}
				if v := b.RoleARN; v != nil && *v != "" {
					result.EnvVars["AWS_ROLE_ARN"] = *v
				}
				if v := b.Profile; v != nil && *v != "" {
					result.EnvVars["AWS_PROFILE"] = *v
				}
			}
		case "oauth":
			result.EnvVars["CLAUDE_CODE_USE_BEDROCK"] = "0"
			delete(result.EnvVars, "ANTHROPIC_MODEL")
			delete(result.EnvVars, "AWS_ACCESS_KEY_ID")
			delete(result.EnvVars, "AWS_SECRET_ACCESS_KEY")
			delete(result.EnvVars, "AWS_ROLE_ARN")
			delete(result.EnvVars, "AWS_PROFILE")
		default:
			log.Printf("[SETTINGSPATCH] Unknown auth_mode %q, ignoring", *resolved.AuthMode)
		}
	}

	// 3. OAuth token.
	if t := resolved.OAuthToken; t != nil && *t != "" {
		result.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"] = *t
	}

	// 4. MCP servers — serialize the typed map to map[string]interface{}.
	if len(resolved.MCPServers) > 0 {
		raw, err := json.Marshal(resolved.MCPServers)
		if err == nil {
			var servers map[string]interface{}
			if err := json.Unmarshal(raw, &servers); err == nil {
				result.MCPServers = servers
			}
		}
	}

	// 5. Active plugins = union(AddPlugins) minus union(RemovePlugins).
	removeSet := make(map[string]bool, len(resolved.RemovePlugins))
	for _, p := range resolved.RemovePlugins {
		removeSet[p] = true
	}
	for _, p := range resolved.AddPlugins {
		if !removeSet[p] {
			result.ActivePlugins = append(result.ActivePlugins, p)
		}
	}

	// 6. Claude SettingsJSON (marketplaces, plugins, hooks).
	settingsMap := make(map[string]interface{})
	if len(resolved.Marketplaces) > 0 {
		m := make(map[string]interface{}, len(resolved.Marketplaces))
		for k, v := range resolved.Marketplaces {
			if v != nil {
				m[k] = map[string]string{"url": v.URL}
			}
		}
		if len(m) > 0 {
			settingsMap["marketplaces"] = m
		}
	}
	if len(result.ActivePlugins) > 0 {
		settingsMap["enabled_plugins"] = result.ActivePlugins
	}
	if len(resolved.Hooks) > 0 {
		settingsMap["hooks"] = resolved.Hooks
	}
	if len(settingsMap) > 0 {
		result.SettingsJSON = settingsMap
	}

	return result, nil
}

// validateEnvVars filters out invalid or dangerous environment variables.
// Returns a new map containing only valid entries.
func validateEnvVars(env map[string]string) map[string]string {
	validKeyPattern := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	dangerousVars := map[string]bool{
		"PATH":            true,
		"LD_PRELOAD":      true,
		"LD_LIBRARY_PATH": true,
		"SHELL":           true,
		"HOME":            true,
		"USER":            true,
		"SUDO_USER":       true,
		"PWD":             true,
		"OLDPWD":          true,
	}

	result := make(map[string]string, len(env))
	for k, v := range env {
		if !validKeyPattern.MatchString(k) {
			log.Printf("[SETTINGSPATCH] Rejected invalid env var name: %s", k)
			continue
		}
		if dangerousVars[strings.ToUpper(k)] {
			log.Printf("[SETTINGSPATCH] Rejected dangerous env var: %s", k)
			continue
		}
		if len(v) > 4096 {
			log.Printf("[SETTINGSPATCH] Rejected env var %s: value too long", k)
			continue
		}
		if strings.ContainsAny(v, "|&;()<>`$\\") {
			log.Printf("[SETTINGSPATCH] Rejected env var %s: contains dangerous characters", k)
			continue
		}
		result[k] = v
	}
	return result
}
