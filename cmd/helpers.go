package cmd

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/cobra"
)

//go:embed claude_code_settings.json
var claudeCodeSettings string

var HelpersCmd = &cobra.Command{
	Use:   "helpers",
	Short: "Helper utilities for agentapi-proxy",
	Long:  "Collection of helper utilities and tools for working with agentapi-proxy",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Available helpers:")
		fmt.Println("  setup-claude-code - Setup Claude Code configuration")
		fmt.Println("  generate-token - Generate GitHub tokens and save to JSON file")
		fmt.Println("Use 'agentapi-proxy helpers --help' for more information about available subcommands.")
	},
}

var setupClaudeCodeCmd = &cobra.Command{
	Use:   "setup-claude-code",
	Short: "Setup Claude Code configuration",
	Long:  "Creates Claude Code configuration directory and settings file at $CLAUDE_DIR/.claude/settings.json",
	Run:   runSetupClaudeCode,
}

var generateTokenCmd = &cobra.Command{
	Use:   "generate-token",
	Short: "Generate GitHub tokens and save to JSON file",
	Long: `Generate GitHub installation tokens using GitHub App credentials and save to a JSON file.

This command generates installation tokens using GitHub App authentication and saves them to
a specified JSON file. If the file already exists, the new tokens will be merged with existing content.

Authentication (all required):
- GITHUB_APP_ID: GitHub App ID
- GITHUB_APP_PEM_PATH: Path to private key file
- GITHUB_INSTALLATION_ID: Installation ID (optional, will auto-detect if not provided)

Optional:
- GITHUB_API: GitHub API base URL (defaults to https://api.github.com)
- --repo-fullname: Repository to generate token for (for auto-detection of installation ID)

Usage:
  agentapi-proxy helpers generate-token --output-path /path/to/tokens.json`,
	RunE: runGenerateToken,
}

var outputPath string
var repoFullNameForToken string

func init() {
	generateTokenCmd.Flags().StringVar(&outputPath, "output-path", "", "Path to JSON file where tokens will be saved (required)")
	generateTokenCmd.Flags().StringVar(&repoFullNameForToken, "repo-fullname", "", "Repository fullname for auto-detection of installation ID (optional)")
	if err := generateTokenCmd.MarkFlagRequired("output-path"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(setupClaudeCodeCmd)
	HelpersCmd.AddCommand(generateTokenCmd)
}

func runSetupClaudeCode(cmd *cobra.Command, args []string) {
	claudeDir := os.Getenv("CLAUDE_DIR")
	if claudeDir == "" {
		fmt.Println("Error: CLAUDE_DIR environment variable is not set")
		os.Exit(1)
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(claudeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", claudeConfigDir, err)
		os.Exit(1)
	}

	// Validate that the embedded JSON is valid
	var tempSettings interface{}
	if err := json.Unmarshal([]byte(claudeCodeSettings), &tempSettings); err != nil {
		fmt.Printf("Error: Invalid embedded settings JSON: %v\n", err)
		os.Exit(1)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(claudeCodeSettings), 0644); err != nil {
		fmt.Printf("Error writing settings file %s: %v\n", settingsPath, err)
		os.Exit(1)
	}
	claudeSettingsMap := map[string]string{
		"hasTrustDialogAccepted":        "true",
		"hasCompletedProjectOnboarding": "true",
		"dontCrawlDirectory":            "true",
	}

	for key, value := range claudeSettingsMap {
		claudeCmd := exec.Command("claude", "config", "set", key, value)
		if err := claudeCmd.Run(); err != nil {
			fmt.Printf("Error setting Claude config: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Successfully created Claude Code configuration at %s\n", settingsPath)
}

type TokenData struct {
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	AppID          string    `json:"app_id"`
	InstallationID string    `json:"installation_id"`
}

type TokenStorage struct {
	Tokens map[string]TokenData `json:"tokens"`
}

func runGenerateToken(cmd *cobra.Command, args []string) error {
	// Validate required environment variables
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return fmt.Errorf("GITHUB_APP_ID environment variable is required")
	}

	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")
	if pemPath == "" {
		return fmt.Errorf("GITHUB_APP_PEM_PATH environment variable is required")
	}

	// Parse App ID
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	// Get or auto-detect Installation ID
	installationIDStr := os.Getenv("GITHUB_INSTALLATION_ID")
	var installationID int64

	if installationIDStr != "" {
		installationID, err = strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
		}
	} else {
		if repoFullNameForToken == "" {
			return fmt.Errorf("either GITHUB_INSTALLATION_ID environment variable or --repo-fullname flag is required for auto-detection")
		}

		fmt.Println("Auto-detecting installation ID...")
		installationID, err = findInstallationIDForRepo(appID, pemPath, repoFullNameForToken, getAPIBaseForToken())
		if err != nil {
			return fmt.Errorf("failed to auto-detect installation ID: %w", err)
		}
		fmt.Printf("Auto-detected installation ID: %d\n", installationID)
	}

	// Generate token
	fmt.Println("Generating installation token...")
	token, expiresAt, err := generateInstallationTokenWithExpiry(appID, installationID, pemPath, getAPIBaseForToken())
	if err != nil {
		return fmt.Errorf("failed to generate installation token: %w", err)
	}

	// Create token data
	tokenKey := fmt.Sprintf("github_app_%s_installation_%d", appIDStr, installationID)
	tokenData := TokenData{
		Token:          token,
		ExpiresAt:      expiresAt,
		CreatedAt:      time.Now(),
		AppID:          appIDStr,
		InstallationID: strconv.FormatInt(installationID, 10),
	}

	// Load existing tokens or create new structure
	var storage TokenStorage
	if _, err := os.Stat(outputPath); err == nil {
		// File exists, load and merge
		data, err := os.ReadFile(outputPath)
		if err != nil {
			return fmt.Errorf("failed to read existing token file: %w", err)
		}

		if err := json.Unmarshal(data, &storage); err != nil {
			return fmt.Errorf("failed to parse existing token file: %w", err)
		}
	} else {
		// File doesn't exist, create new structure
		storage = TokenStorage{
			Tokens: make(map[string]TokenData),
		}
	}

	// Add new token
	storage.Tokens[tokenKey] = tokenData

	// Save to file
	data, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	fmt.Printf("Successfully generated and saved token to %s\n", outputPath)
	fmt.Printf("Token key: %s\n", tokenKey)
	fmt.Printf("Expires at: %s\n", expiresAt.Format(time.RFC3339))
	return nil
}

func getAPIBaseForToken() string {
	apiBase := os.Getenv("GITHUB_API")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return apiBase
}

func generateInstallationTokenWithExpiry(appID, installationID int64, pemPath, apiBase string) (string, time.Time, error) {
	// Read the private key
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to read PEM file: %w", err)
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(nil, appID, pemData)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if apiBase != "" && apiBase != "https://api.github.com" {
		transport.BaseURL = apiBase
	}

	// Create GitHub client
	var client *github.Client
	if apiBase == "" || strings.Contains(apiBase, "https://api.github.com") {
		client = github.NewClient(&http.Client{Transport: transport})
	} else {
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(apiBase, apiBase)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
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
		return "", time.Time{}, fmt.Errorf("failed to create installation token: %w", err)
	}

	return token.GetToken(), token.GetExpiresAt().Time, nil
}
