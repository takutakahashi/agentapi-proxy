package startup

import (
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
)

// SetupClaudeCode sets up Claude Code configuration
func SetupClaudeCode() error {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
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
	if err := mergeClaudeConfig(); err != nil {
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

func mergeClaudeConfig() error {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
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
func SetupMCPServers(configFlag string) error {
	// Get Claude directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
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
	token, err := getGitHubToken()
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

// getGitHubToken retrieves the GitHub token for authentication
func getGitHubToken() (string, error) {
	// Check for personal access token first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	// If no token available, try to use GitHub App authentication
	appID := os.Getenv("GITHUB_APP_ID")
	installationID := os.Getenv("GITHUB_INSTALLATION_ID")
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")

	if appID != "" && installationID != "" && pemPath != "" {
		return generateGitHubAppToken(appID, installationID, pemPath)
	}

	// Check for GITHUB_PERSONAL_ACCESS_TOKEN as fallback
	if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication found: GITHUB_TOKEN, GITHUB_PERSONAL_ACCESS_TOKEN, or GitHub App credentials (GITHUB_APP_ID, GITHUB_INSTALLATION_ID, GITHUB_APP_PEM_PATH) are required")
}

// generateGitHubAppToken generates a GitHub App installation token
func generateGitHubAppToken(appIDStr, installationIDStr, pemPath string) (string, error) {
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

	// Read private key file
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PEM file: %w", err)
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

// setupRepository sets up the git repository with proper cloning
func setupRepository(repoURL, token, cloneDir string) error {
	log.Printf("Setting up repository in: %s", cloneDir)

	// Check if directory exists and is a git repository
	gitDir := filepath.Join(cloneDir, ".git")
	isGitRepo := false
	if _, err := os.Stat(gitDir); err == nil {
		isGitRepo = true
	}

	// Create authenticated URL
	authURL, err := createAuthenticatedURL(repoURL, token)
	if err != nil {
		return fmt.Errorf("failed to create authenticated URL: %w", err)
	}

	if isGitRepo {
		log.Printf("Git repository detected, updating...")
		cmd := exec.Command("git", "pull")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to pull updates: %w", err)
		}
	} else {
		log.Printf("Initializing new git repository...")

		// Create directory if it doesn't exist
		if err := os.MkdirAll(cloneDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Initialize git
		cmd := exec.Command("git", "init")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to initialize git: %w", err)
		}

		// Add remote
		cmd = exec.Command("git", "remote", "add", "origin", authURL)
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to add remote: %w", err)
		}

		// Fetch
		cmd = exec.Command("git", "fetch")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to fetch: %w", err)
		}

		// Try to checkout main, fall back to master
		branches := []string{"main", "master"}
		var checkoutErr error
		for _, branch := range branches {
			cmd = exec.Command("git", "checkout", branch)
			cmd.Dir = cloneDir
			if err := cmd.Run(); err != nil {
				checkoutErr = err
				continue
			}
			checkoutErr = nil
			break
		}

		if checkoutErr != nil {
			return fmt.Errorf("failed to checkout main or master branch: %w", checkoutErr)
		}
	}

	log.Printf("Repository setup completed")
	return nil
}

// createAuthenticatedURL creates an authenticated URL for git operations
func createAuthenticatedURL(repoURL, token string) (string, error) {
	githubURL := getGitHubURL()
	githubHost := strings.TrimPrefix(githubURL, "https://")
	githubHost = strings.TrimPrefix(githubHost, "http://")

	// Parse the repository URL and insert the token
	if strings.HasPrefix(repoURL, githubURL+"/") {
		parts := strings.TrimPrefix(repoURL, githubURL+"/")
		return fmt.Sprintf("https://%s@%s/%s", token, githubHost, parts), nil
	} else if strings.HasPrefix(repoURL, "git@"+githubHost+":") {
		parts := strings.TrimPrefix(repoURL, "git@"+githubHost+":")
		parts = strings.TrimSuffix(parts, ".git")
		return fmt.Sprintf("https://%s@%s/%s.git", token, githubHost, parts), nil
	} else if strings.HasPrefix(repoURL, "https://github.com/") {
		// Handle the case where repoURL starts with standard GitHub URL
		// but we're using GitHub Enterprise
		parts := strings.TrimPrefix(repoURL, "https://github.com/")
		return fmt.Sprintf("https://%s@%s/%s", token, githubHost, parts), nil
	}

	return "", fmt.Errorf("unsupported repository URL format: %s", repoURL)
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
