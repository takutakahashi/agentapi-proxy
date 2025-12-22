package proxy

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
	"github.com/takutakahashi/agentapi-proxy/pkg/userdir"
)

// StartupConfig holds configuration for session startup
type StartupConfig struct {
	Port                      int
	UserID                    string
	RepoFullName              string
	CloneDir                  string
	GitHubToken               string
	GitHubAppID               string
	GitHubInstallationID      string
	GitHubAppPEMPath          string
	GitHubAPI                 string
	GitHubPersonalAccessToken string
	MCPConfigs                string
	AgentAPIArgs              string
	ClaudeArgs                string
	Environment               map[string]string
	Config                    *config.Config
	Verbose                   bool
	IsRestore                 bool // Whether this is a restored session
}

// StartupManager manages the startup process
type StartupManager struct {
	config  *config.Config
	verbose bool
}

// NewStartupManager creates a new startup manager
func NewStartupManager(config *config.Config, verbose bool) *StartupManager {
	return &StartupManager{
		config:  config,
		verbose: verbose,
	}
}

// StartAgentAPISession starts an AgentAPI session with the given configuration
func (sm *StartupManager) StartAgentAPISession(ctx context.Context, cfg *StartupConfig) (*exec.Cmd, error) {
	// Initialize GitHub repository if needed
	if cfg.RepoFullName != "" && cfg.CloneDir != "" {
		if err := sm.initGitHubRepository(cfg); err != nil {
			return nil, fmt.Errorf("failed to initialize GitHub repository: %w", err)
		}
	} else if cfg.CloneDir != "" {
		// Create session directory even when no repository is cloned
		if err := os.MkdirAll(cfg.CloneDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create session directory: %w", err)
		}
	}

	// Setup Claude Code configuration
	if err := sm.setupClaudeCode(cfg); err != nil {
		return nil, fmt.Errorf("failed to setup Claude Code: %w", err)
	}

	// Setup MCP servers if needed
	if cfg.MCPConfigs != "" {
		if err := sm.setupMCPServers(cfg, cfg.MCPConfigs); err != nil {
			log.Printf("Warning: Failed to setup MCP servers: %v", err)
		}
	}

	// Start agentapi server
	cmd := sm.createAgentAPICommand(ctx, cfg)

	// Set up environment
	if err := sm.setupEnvironment(cmd, cfg); err != nil {
		return nil, fmt.Errorf("failed to setup environment: %w", err)
	}

	// Change to clone directory if specified
	if cfg.CloneDir != "" {
		cmd.Dir = cfg.CloneDir
	}

	if sm.verbose {
		log.Printf("Starting agentapi process on port %d", cfg.Port)
		log.Printf("Working directory: %s", cmd.Dir)
	}

	return cmd, nil
}

// initGitHubRepository initializes the GitHub repository
func (sm *StartupManager) initGitHubRepository(cfg *StartupConfig) error {
	// Set environment variables for the GitHub repository initialization
	originalEnv := make(map[string]string)

	// Store original environment and set new ones
	envVars := map[string]string{
		"GITHUB_TOKEN":                 cfg.GitHubToken,
		"GITHUB_APP_ID":                cfg.GitHubAppID,
		"GITHUB_INSTALLATION_ID":       cfg.GitHubInstallationID,
		"GITHUB_APP_PEM_PATH":          cfg.GitHubAppPEMPath,
		"GITHUB_API":                   cfg.GitHubAPI,
		"GITHUB_PERSONAL_ACCESS_TOKEN": cfg.GitHubPersonalAccessToken,
	}

	for key, value := range envVars {
		if value != "" {
			originalEnv[key] = os.Getenv(key)
			if err := os.Setenv(key, value); err != nil {
				log.Printf("Warning: Failed to set environment variable %s: %v", key, err)
			}
		}
	}

	// Restore environment after function completes
	defer func() {
		for key, originalValue := range originalEnv {
			if originalValue == "" {
				if err := os.Unsetenv(key); err != nil {
					log.Printf("Warning: Failed to unset environment variable %s: %v", key, err)
				}
			} else {
				if err := os.Setenv(key, originalValue); err != nil {
					log.Printf("Warning: Failed to restore environment variable %s: %v", key, err)
				}
			}
		}
	}()

	if sm.verbose {
		log.Printf("Initializing GitHub repository: %s", cfg.RepoFullName)
	}

	return startup.InitGitHubRepo(cfg.RepoFullName, cfg.CloneDir, true)
}

// setupClaudeCode sets up Claude Code configuration
func (sm *StartupManager) setupClaudeCode(cfg *StartupConfig) error {
	// Get user home directory using userdir
	userEnv, err := userdir.SetupUserHome(cfg.UserID)
	if err != nil {
		return fmt.Errorf("failed to setup user home: %w", err)
	}

	// Use HOME from userdir, fallback to current user home
	homeDir := userEnv["HOME"]
	if homeDir == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	return startup.SetupClaudeCode(homeDir)
}

// setupMCPServers sets up MCP servers
func (sm *StartupManager) setupMCPServers(cfg *StartupConfig, mcpConfigs string) error {
	if sm.verbose {
		log.Printf("Setting up MCP servers from configuration")
	}

	// Get user home directory using userdir
	userEnv, err := userdir.SetupUserHome(cfg.UserID)
	if err != nil {
		return fmt.Errorf("failed to setup user home: %w", err)
	}

	// Use HOME from userdir, fallback to current user home
	homeDir := userEnv["HOME"]
	if homeDir == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	return startup.SetupMCPServers(homeDir, mcpConfigs)
}

// createAgentAPICommand creates the agentapi server command
func (sm *StartupManager) createAgentAPICommand(ctx context.Context, cfg *StartupConfig) *exec.Cmd {
	args := []string{"server", "--allowed-hosts", "*", "--allowed-origins", "*", "--port", strconv.Itoa(cfg.Port)}

	if cfg.AgentAPIArgs != "" {
		// Parse AgentAPIArgs safely to prevent command injection
		parsedArgs, err := parseCommandArgs(cfg.AgentAPIArgs)
		if err != nil {
			log.Printf("Warning: Failed to parse AgentAPIArgs: %v", err)
		} else {
			args = append(args, parsedArgs...)
		}
	}

	// Prepare Claude command with fallback pattern: claude -c || claude
	// This will try to resume existing session, and if not available, start new session
	var claudeCmd string
	if cfg.ClaudeArgs != "" {
		// Parse ClaudeArgs safely to prevent command injection
		parsedArgs, err := parseCommandArgs(cfg.ClaudeArgs)
		if err != nil {
			log.Printf("Warning: Failed to parse ClaudeArgs: %v", err)
			claudeCmd = "claude -c || claude"
		} else {
			claudeArgsStr := strings.Join(parsedArgs, " ")
			claudeCmd = fmt.Sprintf("claude -c %s || claude %s", claudeArgsStr, claudeArgsStr)
		}
	} else {
		claudeCmd = "claude -c || claude"
	}

	args = append(args, "--", "sh", "-c", claudeCmd)

	cmd := exec.CommandContext(ctx, "agentapi", args...)

	// Set process group ID for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return cmd
}

// parseCommandArgs safely parses command line arguments to prevent injection
func parseCommandArgs(argsStr string) ([]string, error) {
	// Validate input to prevent command injection
	if strings.ContainsAny(argsStr, "|&;()<>`$\\\"'") {
		return nil, fmt.Errorf("invalid characters in command arguments")
	}

	// Simple space-based splitting for now
	// This could be enhanced with proper shell-like parsing if needed
	args := strings.Fields(argsStr)

	// Validate each argument
	for _, arg := range args {
		if !isValidCommandArg(arg) {
			return nil, fmt.Errorf("invalid argument: %s", arg)
		}
	}

	return args, nil
}

// isValidCommandArg validates a single command argument
func isValidCommandArg(arg string) bool {
	// Allow alphanumeric, hyphens, dots, underscores, equals, and forward slashes
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9\-_./=]+$`)
	return validPattern.MatchString(arg)
}

// setupEnvironment sets up the environment for the command
func (sm *StartupManager) setupEnvironment(cmd *exec.Cmd, cfg *StartupConfig) error {
	// Create environment map to avoid duplicates
	envMap := make(map[string]string)

	// Start with the current environment
	for _, env := range os.Environ() {
		if parts := strings.SplitN(env, "=", 2); len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Set up user-specific HOME environment variable (lower priority)
	userEnv, err := userdir.SetupUserHome(cfg.UserID)
	if err != nil {
		return fmt.Errorf("failed to setup user home: %w", err)
	}

	// Apply user environment variables
	for key, value := range userEnv {
		envMap[key] = value
	}

	// Add GitHub-related environment variables (medium priority)
	if cfg.GitHubToken != "" {
		envMap["GITHUB_TOKEN"] = cfg.GitHubToken
	}
	if cfg.GitHubAppID != "" {
		envMap["GITHUB_APP_ID"] = cfg.GitHubAppID
	}
	if cfg.GitHubInstallationID != "" {
		envMap["GITHUB_INSTALLATION_ID"] = cfg.GitHubInstallationID
	}
	if cfg.GitHubAppPEMPath != "" {
		envMap["GITHUB_APP_PEM_PATH"] = cfg.GitHubAppPEMPath
	}
	if cfg.GitHubAPI != "" {
		envMap["GITHUB_API"] = cfg.GitHubAPI
	}
	if cfg.GitHubPersonalAccessToken != "" {
		envMap["GITHUB_PERSONAL_ACCESS_TOKEN"] = cfg.GitHubPersonalAccessToken
	}
	if cfg.RepoFullName != "" {
		envMap["GITHUB_REPO_FULLNAME"] = cfg.RepoFullName
	}
	if cfg.CloneDir != "" {
		envMap["GITHUB_CLONE_DIR"] = cfg.CloneDir
	}

	// Add custom environment variables from session (highest priority)
	if len(cfg.Environment) > 0 {
		log.Printf("[ENV] Adding %d session environment variables:", len(cfg.Environment))
		for key, value := range cfg.Environment {
			log.Printf("[ENV]   Setting %s=%s", key, value)
			envMap[key] = value
		}
	}

	// Convert map to slice
	var envSlice []string
	for key, value := range envMap {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	if sm.verbose {
		log.Printf("[ENV] Final environment variables for agentapi process (%d):", len(envSlice))
		for _, env := range envSlice {
			if strings.HasPrefix(env, "TEAM_") || strings.HasPrefix(env, "ROLE_") || strings.HasPrefix(env, "REQUEST_") {
				log.Printf("[ENV]   %s", env)
			}
		}
	}

	cmd.Env = envSlice
	return nil
}
