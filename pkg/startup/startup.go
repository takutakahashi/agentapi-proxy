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
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
)

// Global re-authentication service instance
var globalReauthService *ReauthService

// ReauthService manages periodic re-authentication for GitHub tokens
type ReauthService struct {
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	ticker          *time.Ticker
	interval        time.Duration
	currentToken    string
	running         bool
	tokenUpdateChan chan string
	errorChan       chan error
}

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
		return GenerateGitHubAppToken(appID, installationID, pemPath)
	}

	// Check for GITHUB_PERSONAL_ACCESS_TOKEN as fallback
	if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication found: GITHUB_TOKEN, GITHUB_PERSONAL_ACCESS_TOKEN, or GitHub App credentials (GITHUB_APP_ID, GITHUB_INSTALLATION_ID, GITHUB_APP_PEM_PATH) are required")
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
			if err := authenticateGHCLI(githubHost, token, env); err != nil {
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

// authenticateGHCLI authenticates gh CLI for GitHub Enterprise Server
func authenticateGHCLI(githubHost, token string, env []string) error {
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

// StartReauthService starts the global re-authentication service
func StartReauthService() error {
	// Only start if GitHub App credentials are available
	appID := os.Getenv("GITHUB_APP_ID")
	installationID := os.Getenv("GITHUB_INSTALLATION_ID")
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")
	
	if appID == "" || installationID == "" || pemPath == "" {
		log.Printf("GitHub App credentials not found, skipping re-authentication service")
		return nil
	}

	// Create and start the service with 4-hour interval
	service := NewReauthService(4 * time.Hour)
	if err := service.Start(); err != nil {
		return fmt.Errorf("failed to start re-authentication service: %w", err)
	}

	// Store globally for later cleanup
	globalReauthService = service

	// Start monitoring for errors
	go func() {
		for err := range service.Errors() {
			log.Printf("Re-authentication service error: %v", err)
		}
	}()

	return nil
}

// StopReauthService stops the global re-authentication service
func StopReauthService() {
	if globalReauthService != nil {
		globalReauthService.Stop()
		globalReauthService = nil
	}
}

// NewReauthService creates a new re-authentication service
func NewReauthService(interval time.Duration) *ReauthService {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ReauthService{
		ctx:             ctx,
		cancel:          cancel,
		interval:        interval,
		tokenUpdateChan: make(chan string, 1),
		errorChan:       make(chan error, 1),
	}
}

// Start begins the periodic re-authentication process
func (rs *ReauthService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.running {
		return fmt.Errorf("re-authentication service is already running")
	}

	// Perform initial authentication
	token, err := rs.authenticate()
	if err != nil {
		return fmt.Errorf("initial authentication failed: %w", err)
	}

	rs.currentToken = token
	rs.running = true
	rs.ticker = time.NewTicker(rs.interval)

	// Start the background goroutine
	go rs.run()

	log.Printf("Re-authentication service started with interval: %v", rs.interval)
	return nil
}

// Stop terminates the re-authentication service
func (rs *ReauthService) Stop() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.running {
		return
	}

	rs.running = false
	rs.cancel()
	
	if rs.ticker != nil {
		rs.ticker.Stop()
	}

	log.Printf("Re-authentication service stopped")
}

// GetCurrentToken returns the current authentication token
func (rs *ReauthService) GetCurrentToken() string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.currentToken
}

// Errors returns a channel that receives authentication errors
func (rs *ReauthService) Errors() <-chan error {
	return rs.errorChan
}

// run is the main loop for the re-authentication service
func (rs *ReauthService) run() {
	for {
		select {
		case <-rs.ctx.Done():
			return
		case <-rs.ticker.C:
			rs.performReauth()
		}
	}
}

// performReauth performs the re-authentication process
func (rs *ReauthService) performReauth() {
	log.Printf("Starting re-authentication process...")

	token, err := rs.authenticate()
	if err != nil {
		log.Printf("Re-authentication failed: %v", err)
		select {
		case rs.errorChan <- err:
		default:
		}
		return
	}

	rs.mu.Lock()
	rs.currentToken = token
	rs.mu.Unlock()

	// Update gh CLI and git remote
	if err := rs.updateGHCLI(token); err != nil {
		log.Printf("Failed to update gh CLI: %v", err)
		select {
		case rs.errorChan <- fmt.Errorf("failed to update gh CLI: %w", err):
		default:
		}
	}

	if err := rs.updateGitRemote(token); err != nil {
		log.Printf("Failed to update git remote: %v", err)
		select {
		case rs.errorChan <- fmt.Errorf("failed to update git remote: %w", err):
		default:
		}
	}

	// Notify about token update
	select {
	case rs.tokenUpdateChan <- token:
	default:
	}

	log.Printf("Re-authentication completed successfully")
}

// authenticate generates a new GitHub token
func (rs *ReauthService) authenticate() (string, error) {
	// Check for personal access token first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	// Try GitHub App authentication
	appID := os.Getenv("GITHUB_APP_ID")
	installationID := os.Getenv("GITHUB_INSTALLATION_ID")
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")

	if appID != "" && installationID != "" && pemPath != "" {
		return GenerateGitHubAppToken(appID, installationID, pemPath)
	}

	// Check for personal access token as fallback
	if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication found: GITHUB_TOKEN, GITHUB_PERSONAL_ACCESS_TOKEN, or GitHub App credentials are required")
}

// updateGHCLI updates the GitHub CLI authentication
func (rs *ReauthService) updateGHCLI(token string) error {
	// Set environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("GITHUB_TOKEN=%s", token))

	// Handle GitHub Enterprise
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		githubHost := strings.TrimPrefix(githubAPI, "https://")
		githubHost = strings.TrimPrefix(githubHost, "http://")
		githubHost = strings.TrimSuffix(githubHost, "/api/v3")
		
		log.Printf("Updating gh CLI authentication for Enterprise Server: %s", githubHost)
		
		// Use gh auth login with token for Enterprise Server
		cmd := exec.Command("gh", "auth", "login", "--hostname", githubHost, "--with-token")
		cmd.Stdin = strings.NewReader(token)
		cmd.Env = env
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh auth login failed for %s: %w", githubHost, err)
		}
		
		log.Printf("Successfully updated gh CLI authentication for %s", githubHost)
	} else {
		// For regular GitHub.com, gh will use GITHUB_TOKEN environment variable
		log.Printf("Updated gh CLI authentication for github.com")
	}

	return nil
}

// updateGitRemote updates the git remote URL with the new token
func (rs *ReauthService) updateGitRemote(token string) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Check if we're in a git repository
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = cwd
	if err := cmd.Run(); err != nil {
		log.Printf("Not in a git repository, skipping git remote update")
		return nil
	}

	// Get current remote URL
	cmd = exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	currentURL := strings.TrimSpace(string(output))
	
	// Update remote URL with new token
	newURL, err := rs.updateURLWithToken(currentURL, token)
	if err != nil {
		return fmt.Errorf("failed to update URL with token: %w", err)
	}

	// Set new remote URL
	cmd = exec.Command("git", "remote", "set-url", "origin", newURL)
	cmd.Dir = cwd
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set remote URL: %w", err)
	}

	log.Printf("Successfully updated git remote URL")
	return nil
}

// updateURLWithToken updates a git URL with a new token
func (rs *ReauthService) updateURLWithToken(currentURL, token string) (string, error) {
	// Skip SSH URLs
	if strings.HasPrefix(currentURL, "git@") {
		return currentURL, nil
	}

	// Handle HTTPS URLs
	if strings.HasPrefix(currentURL, "https://") {
		// Remove existing token if present
		if strings.Contains(currentURL, "@") {
			parts := strings.Split(currentURL, "@")
			if len(parts) >= 2 {
				currentURL = "https://" + parts[1]
			}
		}

		// Insert new token
		newURL := strings.Replace(currentURL, "https://", fmt.Sprintf("https://%s@", token), 1)
		return newURL, nil
	}

	return currentURL, nil
}
