package proxy

import (
	"log"
	"regexp"
	"strings"

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
		validatedEnv := validateEnvironmentVariables(cfg.RequestEnv)
		for key, value := range validatedEnv {
			mergedEnv[key] = value
		}
		if len(validatedEnv) > 0 {
			log.Printf("[ENV] Applied %d validated environment variables from request", len(validatedEnv))
		}
		if len(validatedEnv) != len(cfg.RequestEnv) {
			log.Printf("[ENV] Warning: %d environment variables were rejected due to validation", len(cfg.RequestEnv)-len(validatedEnv))
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

// validateEnvironmentVariables validates user-provided environment variables
func validateEnvironmentVariables(env map[string]string) map[string]string {
	validatedEnv := make(map[string]string)

	// Valid environment variable name pattern
	validKeyPattern := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// Dangerous environment variables that should not be overridden
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

	for key, value := range env {
		// Validate key format
		if !validKeyPattern.MatchString(key) {
			log.Printf("[ENV] Rejected invalid environment variable name: %s", key)
			continue
		}

		// Check if it's a dangerous variable
		if dangerousVars[strings.ToUpper(key)] {
			log.Printf("[ENV] Rejected dangerous environment variable: %s", key)
			continue
		}

		// Validate value (basic checks)
		if len(value) > 4096 {
			log.Printf("[ENV] Rejected environment variable %s: value too long", key)
			continue
		}

		// Check for potential shell metacharacters in value
		if strings.ContainsAny(value, "|&;()<>`$\\") {
			log.Printf("[ENV] Rejected environment variable %s: contains dangerous characters", key)
			continue
		}

		validatedEnv[key] = value
	}

	return validatedEnv
}
