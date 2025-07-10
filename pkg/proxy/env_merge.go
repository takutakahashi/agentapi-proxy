package proxy

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// EnvMergeConfig contains configuration for environment variable merging
type EnvMergeConfig struct {
	RoleEnvFiles    *config.RoleEnvFilesConfig
	UserRole        string
	TeamEnvFile     string // From tags["env_file"]
	AuthTeamEnvFile string // From team_role_mapping
	RequestEnv      map[string]string
}

// MergeEnvironmentVariables merges environment variables from multiple sources
// with the following priority (highest to lowest):
// 1. Request environment variables
// 2. Team/organization specific environment file (from tags["env_file"])
// 3. Auth team environment file (from team_role_mapping)
// 4. Role-based environment variables
func MergeEnvironmentVariables(cfg EnvMergeConfig) (map[string]string, error) {
	mergedEnv := make(map[string]string)

	// 1. Load role-based environment variables (lowest priority)
	if cfg.RoleEnvFiles != nil && cfg.RoleEnvFiles.Enabled && cfg.UserRole != "" {
		envVars, err := config.LoadRoleEnvVars(cfg.RoleEnvFiles, cfg.UserRole)
		if err != nil {
			log.Printf("[ENV] Failed to load role environment variables: %v", err)
		} else if len(envVars) > 0 {
			for _, env := range envVars {
				mergedEnv[env.Key] = env.Value
			}
			log.Printf("[ENV] Loaded %d role-based environment variables for role '%s'", len(envVars), cfg.UserRole)
		}
	}

	// 2. Load auth team environment file if specified (medium-low priority)
	if cfg.AuthTeamEnvFile != "" {
		authTeamEnvVars, err := config.LoadTeamEnvVars(cfg.AuthTeamEnvFile)
		if err != nil {
			log.Printf("[ENV] Failed to load auth team environment file %s: %v", cfg.AuthTeamEnvFile, err)
		} else if len(authTeamEnvVars) > 0 {
			for _, env := range authTeamEnvVars {
				mergedEnv[env.Key] = env.Value
			}
			log.Printf("[ENV] Loaded %d environment variables from auth team file: %s", len(authTeamEnvVars), cfg.AuthTeamEnvFile)
		}
	}

	// 3. Load team/organization specific environment file if specified (medium priority)
	if cfg.TeamEnvFile != "" {
		teamEnvVars, err := config.LoadTeamEnvVars(cfg.TeamEnvFile)
		if err != nil {
			log.Printf("[ENV] Failed to load team environment file %s: %v", cfg.TeamEnvFile, err)
		} else if len(teamEnvVars) > 0 {
			for _, env := range teamEnvVars {
				mergedEnv[env.Key] = env.Value
			}
			log.Printf("[ENV] Loaded %d environment variables from team file: %s", len(teamEnvVars), cfg.TeamEnvFile)
		}
	}

	// 4. Override with request environment variables (highest priority)
	if cfg.RequestEnv != nil {
		for key, value := range cfg.RequestEnv {
			mergedEnv[key] = value
		}
		if len(cfg.RequestEnv) > 0 {
			log.Printf("[ENV] Applied %d environment variables from request", len(cfg.RequestEnv))
		}
	}

	return mergedEnv, nil
}

// ExtractTeamEnvFile extracts the env_file value from tags
func ExtractTeamEnvFile(tags map[string]string) string {
	if tags == nil {
		return ""
	}
	return tags["env_file"]
}
