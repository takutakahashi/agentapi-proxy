package startup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

// Global installation cache instance for startup package
var installationCache = github_pkg.NewInstallationCache()

// SetupClaudeCode sets up Claude Code configuration
func SetupClaudeCode(homeDir string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", claudeConfigDir, err)
	}

	// Claude Code settings JSON
	claudeCodeSettings := `{
  "workspaceFolders": [],
  "recentWorkspaces": [],
  "settings": {
    "mcp.enabled": true
  }
}`

	// Validate that the embedded JSON is valid
	var tempSettings interface{}
	if err := json.Unmarshal([]byte(claudeCodeSettings), &tempSettings); err != nil {
		return fmt.Errorf("invalid embedded settings JSON: %w", err)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(claudeCodeSettings), 0644); err != nil {
		return fmt.Errorf("failed to write settings file %s: %w", settingsPath, err)
	}

	// Merge config into ~/.claude.json
	if err := mergeClaudeConfig(homeDir); err != nil {
		log.Printf("Warning: Failed to merge claude config: %v", err)
		// Don't return error, just warn
	}

	claudeSettingsMap := map[string]string{
		"hasTrustDialogAccepted":        "true",
		"hasCompletedProjectOnboarding": "true",
		"dontCrawlDirectory":            "true",
	}

	for key, value := range claudeSettingsMap {
		claudeCmd := exec.Command("claude", "config", "set", key, value)
		if err := claudeCmd.Run(); err != nil {
			log.Printf("Warning: Failed to set Claude config for key '%s': %v", key, err)
			// Don't return error for claude config, just warn
		}
	}

	log.Printf("Successfully created Claude Code configuration at %s", settingsPath)
	return nil
}

func mergeClaudeConfig(homeDir string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	targetPath := filepath.Join(homeDir, ".claude.json")

	// Define the configuration to merge as a map
	configToMerge := map[string]interface{}{
		"hasCompletedOnboarding":        true,
		"bypassPermissionsModeAccepted": true,
	}

	// Read existing ~/.claude.json if it exists
	var targetJSON map[string]interface{}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read ~/.claude.json: %w", err)
		}
		// File doesn't exist, we'll create it
		targetJSON = make(map[string]interface{})
	} else {
		// Parse existing JSON
		if err := json.Unmarshal(targetData, &targetJSON); err != nil {
			return fmt.Errorf("failed to parse ~/.claude.json: %w", err)
		}
	}

	// Merge configuration map into target
	for key, value := range configToMerge {
		targetJSON[key] = value
	}

	// Write merged JSON back
	mergedData, err := json.MarshalIndent(targetJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged JSON: %w", err)
	}

	if err := os.WriteFile(targetPath, mergedData, 0644); err != nil {
		return fmt.Errorf("failed to write merged ~/.claude.json: %w", err)
	}

	return nil
}

// MCPServerConfig represents MCP server configuration
type MCPServerConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Endpoint  string            `json:"endpoint"`
	Enabled   bool              `json:"enabled"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
}

// SetupMCPServers sets up MCP servers from configuration
func SetupMCPServers(homeDir, configFlag string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}
	claudeDir := filepath.Join(homeDir, ".claude")

	if configFlag == "" {
		return fmt.Errorf("config flag is required")
	}

	// Decode base64 configuration
	configJson, err := decodeBase64(configFlag)
	if err != nil {
		return fmt.Errorf("failed to decode base64 config: %w", err)
	}

	// Parse MCP server configurations
	var mcpConfigs []MCPServerConfig
	if err := json.Unmarshal([]byte(configJson), &mcpConfigs); err != nil {
		return fmt.Errorf("failed to parse MCP configuration: %w", err)
	}

	log.Printf("Adding %d MCP servers to Claude configuration", len(mcpConfigs))

	// Add each MCP server using claude command
	for _, mcpConfig := range mcpConfigs {
		if err := addMcpServer(claudeDir, mcpConfig); err != nil {
			log.Printf("Warning: Failed to add MCP server %s: %v", mcpConfig.Name, err)
			// Continue with other servers even if one fails
		} else {
			log.Printf("Successfully added MCP server: %s", mcpConfig.Name)
		}
	}

	return nil
}

func decodeBase64(encoded string) (string, error) {
	// Try base64 decoding first
	if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return string(decoded), nil
	}

	// If base64 fails, try returning as-is (assume it's already decoded)
	return encoded, nil
}

func addMcpServer(claudeDir string, mcpConfig MCPServerConfig) error {
	// Build claude mcp add command
	args := []string{"mcp", "add", mcpConfig.Name}

	// Add command and args for stdio transport
	if mcpConfig.Transport == "stdio" && mcpConfig.Command != "" {
		args = append(args, mcpConfig.Command)
		if len(mcpConfig.Args) > 0 {
			args = append(args, mcpConfig.Args...)
		}
	}

	// Set environment variables
	env := os.Environ()

	// Add custom environment variables from MCP config
	for key, value := range mcpConfig.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Execute claude command
	claudeCmd := exec.Command("claude", args...)
	claudeCmd.Env = env
	claudeCmd.Dir = claudeDir

	output, err := claudeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude command failed: %w, output: %s", err, string(output))
	}

	log.Printf("Claude mcp add output for %s: %s", mcpConfig.Name, string(output))
	return nil
}

// InitGitHubRepo initializes a GitHub repository with proper git clone functionality
func InitGitHubRepo(repoFullName, cloneDir string, ignoreMissingConfig bool) error {
	log.Printf("Initializing GitHub repository: %s to %s", repoFullName, cloneDir)

	if repoFullName == "" {
		if ignoreMissingConfig {
			log.Printf("Warning: No repository fullname provided, skipping GitHub initialization")
			return nil
		}
		return fmt.Errorf("repository fullname is required")
	}

	// Create the clone directory
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		return fmt.Errorf("failed to create clone directory: %w", err)
	}

	// Get GitHub token for authentication
	token, err := GetGitHubToken(repoFullName)
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Create repository URL using the correct GitHub URL
	githubURL := getGitHubURL()
	repoURL := fmt.Sprintf("%s/%s", githubURL, repoFullName)

	// Setup the repository with proper git clone
	if err := setupRepository(repoURL, token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	log.Printf("GitHub repository initialization completed successfully")
	return nil
}

// GetGitHubToken retrieves the GitHub token for authentication
func GetGitHubToken(repoFullName string) (string, error) {
	// Check for personal access token first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	// If no token available, try to use GitHub App authentication
	appID := os.Getenv("GITHUB_APP_ID")
	installationID := os.Getenv("GITHUB_INSTALLATION_ID")
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")

	// GitHub App authentication requires App ID and PEM path
	if appID != "" && pemPath != "" {
		// If Installation ID is not provided, try auto-discovery
		if installationID == "" {
			// Auto-discovery requires repository fullname
			if repoFullName == "" {
				// Fall back to environment variable if not provided
				repoFullName = os.Getenv("GITHUB_REPO_FULLNAME")
			}
			if repoFullName == "" {
				log.Println("GITHUB_INSTALLATION_ID not provided and repoFullName not available for auto-discovery")
				// Fall through to next authentication method
			} else {
				log.Printf("GITHUB_INSTALLATION_ID not provided, attempting auto-discovery for repository: %s", repoFullName)
				discoveredID, err := AutoDiscoverInstallationID(appID, pemPath, repoFullName)
				if err != nil {
					log.Printf("Failed to auto-discover installation ID: %v", err)
					// Fall through to next authentication method
				} else {
					installationID = discoveredID
					log.Printf("Auto-discovered installation ID: %s", installationID)
				}
			}
		}

		// If we have installation ID (manual or auto-discovered), proceed with token generation
		if installationID != "" {
			return GenerateGitHubAppToken(appID, installationID, pemPath)
		}
	}

	// Check for GITHUB_PERSONAL_ACCESS_TOKEN as fallback
	if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication found: GITHUB_TOKEN, GITHUB_PERSONAL_ACCESS_TOKEN, or GitHub App credentials (GITHUB_APP_ID, GITHUB_APP_PEM_PATH) are required. GITHUB_INSTALLATION_ID is optional and will be auto-discovered if GITHUB_REPO_FULLNAME is set")
}

// GenerateGitHubAppToken generates a GitHub App installation token
func GenerateGitHubAppToken(appIDStr, installationIDStr, pemPath string) (string, error) {
	// Parse app ID
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	// Parse installation ID
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
	}

	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err = os.ReadFile(pemPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			log.Printf("Failed to read PEM file %s, using GITHUB_APP_PEM environment variable", pemPath)
			pemData = []byte(pemContent)
		} else {
			// より詳細なエラー情報を提供
			fileInfo, statErr := os.Stat(pemPath)
			if statErr != nil {
				return "", fmt.Errorf("failed to read PEM file %s: file does not exist or is not accessible. Also checked GITHUB_APP_PEM environment variable: %w", pemPath, err)
			}
			return "", fmt.Errorf("failed to read PEM file %s (size: %d bytes, mode: %s). Also checked GITHUB_APP_PEM environment variable: %w",
				pemPath, fileInfo.Size(), fileInfo.Mode(), err)
		}
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pemData)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		transport.BaseURL = githubAPI
	}

	// Create GitHub client
	client := github.NewClient(&http.Client{Transport: transport})

	// Handle GitHub Enterprise
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" {
		var err error
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(githubAPI, githubAPI)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	// Create installation token
	ctx := context.Background()
	token, _, err := client.Apps.CreateInstallationToken(
		ctx,
		installationID,
		&github.InstallationTokenOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}

	return token.GetToken(), nil
}

// AutoDiscoverInstallationID discovers the installation ID for a given repository using cache
func AutoDiscoverInstallationID(appIDStr, pemPath, repoFullName string) (string, error) {
	// Parse app ID
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err = os.ReadFile(pemPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			pemData = []byte(pemContent)
		} else {
			return "", fmt.Errorf("failed to read PEM file %s and GITHUB_APP_PEM environment variable not set: %w", pemPath, err)
		}
	}

	// Get API base URL
	apiBase := os.Getenv("GITHUB_API")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}

	ctx := context.Background()
	installationID, err := installationCache.GetInstallationID(ctx, appID, pemData, repoFullName, apiBase)
	if err != nil {
		return "", err
	}

	return strconv.FormatInt(installationID, 10), nil
}

// setupRepository sets up the git repository using gh repo clone
func setupRepository(repoURL, token, cloneDir string) error {
	log.Printf("Setting up repository in: %s", cloneDir)

	// Check if directory exists and is a git repository
	gitDir := filepath.Join(cloneDir, ".git")
	isGitRepo := false
	if _, err := os.Stat(gitDir); err == nil {
		isGitRepo = true
	}

	if isGitRepo {
		log.Printf("Git repository detected, updating...")
		cmd := exec.Command("git", "pull")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to pull updates: %w", err)
		}
	} else {
		log.Printf("Cloning repository using gh repo clone...")

		// Extract repository name from URL
		repoName, err := extractRepoName(repoURL)
		if err != nil {
			return fmt.Errorf("failed to extract repository name: %w", err)
		}

		// Set up environment for gh command
		env := os.Environ()
		env = append(env, fmt.Sprintf("GITHUB_TOKEN=%s", token))

		// Set GitHub Enterprise host if applicable
		if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
			githubHost := strings.TrimPrefix(githubAPI, "https://")
			githubHost = strings.TrimPrefix(githubHost, "http://")
			githubHost = strings.TrimSuffix(githubHost, "/api/v3")
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))

			// Authenticate gh CLI for GitHub Enterprise Server
			if err := AuthenticateGHCLI(githubHost, token, env); err != nil {
				log.Printf("Warning: Failed to authenticate gh CLI for %s: %v", githubHost, err)
			}
		}

		// Create parent directory
		parentDir := filepath.Dir(cloneDir)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Use gh repo clone command
		cmd := exec.Command("gh", "repo", "clone", repoName, cloneDir)
		cmd.Env = env
		cmd.Dir = parentDir

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to clone repository with gh: %w, output: %s", err, string(output))
		}

		log.Printf("Successfully cloned repository using gh repo clone")
	}

	log.Printf("Repository setup completed")
	return nil
}

// AuthenticateGHCLI authenticates gh CLI for GitHub Enterprise Server
func AuthenticateGHCLI(githubHost, token string, env []string) error {
	log.Printf("Authenticating gh CLI for Enterprise Server: %s", githubHost)

	// Use gh auth login with token for Enterprise Server
	cmd := exec.Command("gh", "auth", "login", "--hostname", githubHost, "--with-token")
	cmd.Stdin = strings.NewReader(token)

	// Set environment variables to use user-specific home directory
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth login failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully authenticated gh CLI for %s", githubHost)
	return nil
}

// extractRepoName extracts owner/repo from various GitHub URL formats
func extractRepoName(repoURL string) (string, error) {
	// Handle various URL formats
	if strings.HasPrefix(repoURL, "git@") {
		// git@github.com:owner/repo.git or git@github.enterprise.com:owner/repo.git
		parts := strings.Split(repoURL, ":")
		if len(parts) >= 2 {
			repoPath := parts[len(parts)-1]
			return strings.TrimSuffix(repoPath, ".git"), nil
		}
	} else if strings.Contains(repoURL, "://") {
		// https://github.com/owner/repo or https://github.enterprise.com/owner/repo
		parts := strings.Split(repoURL, "/")
		if len(parts) >= 2 {
			owner := parts[len(parts)-2]
			repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
			return fmt.Sprintf("%s/%s", owner, repo), nil
		}
	}

	return "", fmt.Errorf("unable to extract repository name from URL: %s", repoURL)
}

// getGitHubURL returns the GitHub URL (supports enterprise)
func getGitHubURL() string {
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" {
		// For enterprise GitHub, convert API URL to web URL
		if strings.HasSuffix(githubAPI, "/api/v3") {
			return strings.TrimSuffix(githubAPI, "/api/v3")
		}
		return githubAPI
	}
	return "https://github.com"
}

// SetupGitHubAuth sets up GitHub authentication using gh CLI
func SetupGitHubAuth(repoFullName string) error {
	log.Printf("Setting up GitHub authentication for repository: %s", repoFullName)

	// Determine GitHub host
	githubHost := "github.com"
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		githubHost = strings.TrimPrefix(githubAPI, "https://")
		githubHost = strings.TrimPrefix(githubHost, "http://")
		githubHost = strings.TrimSuffix(githubHost, "/api/v3")
	}

	// Check if GITHUB_TOKEN is already set in the environment
	existingToken := os.Getenv("GITHUB_TOKEN")
	if existingToken != "" {
		log.Printf("GITHUB_TOKEN environment variable is already set")

		// Set up environment for git setup
		env := os.Environ()

		// For GitHub Enterprise Server, we need to explicitly authenticate
		// even when GITHUB_TOKEN is set, because gh auth setup-git requires
		// the host to be in the authenticated hosts list
		if githubHost != "github.com" {
			log.Printf("GitHub Enterprise Server detected (%s), performing explicit gh auth login", githubHost)
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))

			// Perform gh auth login for GHES
			if err := performGHAuthLogin(githubHost, existingToken, env); err != nil {
				return fmt.Errorf("failed to authenticate gh CLI for GHES: %w", err)
			}
		} else {
			log.Printf("Skipping gh auth login for github.com (GITHUB_TOKEN is automatically used)")
		}

		// Setup git authentication
		if err := performGHAuthSetupGit(githubHost, env); err != nil {
			return fmt.Errorf("failed to setup git authentication: %w", err)
		}

		log.Printf("Successfully set up GitHub authentication using existing GITHUB_TOKEN")
		return nil
	}

	// Get GitHub token for authentication
	token, err := GetGitHubToken(repoFullName)
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Set up environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("GITHUB_TOKEN=%s", token))

	// Add GH_HOST for GitHub Enterprise Server
	if githubHost != "github.com" {
		env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))
	}

	// Authenticate gh CLI
	if err := performGHAuthLogin(githubHost, token, env); err != nil {
		return fmt.Errorf("failed to authenticate gh CLI: %w", err)
	}

	// Setup git authentication
	if err := performGHAuthSetupGit(githubHost, env); err != nil {
		return fmt.Errorf("failed to setup git authentication: %w", err)
	}

	log.Printf("Successfully set up GitHub authentication")
	return nil
}

// performGHAuthLogin performs gh auth login
func performGHAuthLogin(githubHost, token string, env []string) error {
	log.Printf("Performing gh auth login for host: %s", githubHost)

	var cmd *exec.Cmd
	if githubHost == "github.com" {
		// For GitHub.com, use simple auth login
		cmd = exec.Command("gh", "auth", "login", "--with-token")
	} else {
		// For GitHub Enterprise, specify hostname
		cmd = exec.Command("gh", "auth", "login", "--hostname", githubHost, "--with-token")
	}

	cmd.Stdin = strings.NewReader(token)
	cmd.Env = env

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth login failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully authenticated gh CLI for %s", githubHost)
	return nil
}

// performGHAuthSetupGit performs gh auth setup-git
func performGHAuthSetupGit(githubHost string, env []string) error {
	log.Printf("Performing gh auth setup-git for host: %s", githubHost)

	var cmd *exec.Cmd
	if githubHost == "" || githubHost == "github.com" {
		cmd = exec.Command("gh", "auth", "setup-git")
	} else {
		// For GitHub Enterprise Server, specify hostname with --force
		// --force is required when the host might not be in gh's authenticated hosts list
		// (e.g., when using GITHUB_TOKEN environment variable)
		cmd = exec.Command("gh", "auth", "setup-git", "--hostname", githubHost, "--force")
	}
	cmd.Env = env

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth setup-git failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully set up git authentication for %s", githubHost)
	return nil
}
