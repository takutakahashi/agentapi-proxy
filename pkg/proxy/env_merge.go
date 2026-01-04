package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// Type aliases for backward compatibility
// These types are now defined in internal/infrastructure/services

// EnvMergeConfig contains configuration for environment variable merging
type EnvMergeConfig = services.EnvMergeConfig

// MergeEnvironmentVariables merges environment variables from multiple sources
var MergeEnvironmentVariables = services.MergeEnvironmentVariables

// ExtractTeamEnvFile extracts the env_file value from tags
var ExtractTeamEnvFile = services.ExtractTeamEnvFile
