package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/cobra"
)

type GitHubAppConfig struct {
	AppID          int64
	InstallationID int64
	PEMPath        string
	APIBase        string
}

var initGitHubRepoCmd = &cobra.Command{
	Use:   "init-github-repository",
	Short: "Initialize GitHub repository with authentication",
	Long: `Initialize GitHub repository with proper authentication.
This command will:
1. Validate GitHub authentication (token or app credentials)
2. Clone or update the specified GitHub repository
3. Set up MCP integration with Claude

Parameters:
- --repo-fullname: The GitHub repository in org/repo format (e.g., "owner/repository")
- --clone-dir: Target directory for cloning (defaults to current directory)

Authentication options (one required):
- GITHUB_TOKEN: Personal access token or existing token
- GitHub App credentials:
  - GITHUB_APP_PEM_PATH: Path to private key file
  - GITHUB_APP_ID: GitHub App ID
  - GITHUB_INSTALLATION_ID: Installation ID (optional, will auto-detect if not provided)

Optional:
- GITHUB_API: GitHub API base URL (defaults to https://api.github.com)
- GITHUB_URL: GitHub base URL (defaults to https://github.com)
`,
	RunE: runInitGitHubRepo,
}

var ignoreMissingConfig bool
var repoFullName string
var cloneDir string

func runInitGitHubRepo(cmd *cobra.Command, args []string) error {
	// Get repository fullname from flag or environment variable
	if repoFullName == "" {
		repoFullName = os.Getenv("GITHUB_REPO_FULLNAME")
	}
	if repoFullName == "" && !ignoreMissingConfig {
		return fmt.Errorf("repository fullname is required (use --repo-fullname flag or GITHUB_REPO_FULLNAME environment variable)")
	}

	token := os.Getenv("GITHUB_TOKEN")

	// If no token provided, try to generate one from GitHub App credentials
	if token == "" {
		if ignoreMissingConfig {
			fmt.Println("Skipping GitHub setup due to --ignore-missing-config flag")
			return nil
		}
		fmt.Println("No GITHUB_TOKEN found, attempting to generate from GitHub App credentials...")

		appID, err := parseAppID(os.Getenv("GITHUB_APP_ID"))
		if err != nil {
			return fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
		}

		installationIDStr := os.Getenv("GITHUB_INSTALLATION_ID")
		var installationID int64

		if installationIDStr != "" {
			// Use manually provided installation ID
			installationID, err = parseInstallationID(installationIDStr)
			if err != nil {
				return fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
			}
		} else {
			// Auto-detect installation ID
			fmt.Println("No GITHUB_INSTALLATION_ID provided, attempting auto-detection...")
			installationID, err = findInstallationIDForRepo(appID, os.Getenv("GITHUB_APP_PEM_PATH"), repoFullName, getAPIBase())
			if err != nil {
				return fmt.Errorf("failed to auto-detect installation ID: %w", err)
			}
			fmt.Printf("Auto-detected installation ID: %d\n", installationID)
		}

		appConfig := GitHubAppConfig{
			AppID:          appID,
			InstallationID: installationID,
			PEMPath:        os.Getenv("GITHUB_APP_PEM_PATH"),
			APIBase:        getAPIBase(),
		}

		if appConfig.AppID == 0 || appConfig.InstallationID == 0 || appConfig.PEMPath == "" {
			return fmt.Errorf("either GITHUB_TOKEN or GitHub App credentials (GITHUB_APP_ID, GITHUB_APP_PEM_PATH) must be provided")
		}

		generatedToken, err := generateInstallationToken(appConfig)
		if err != nil {
			return fmt.Errorf("failed to generate installation token: %w", err)
		}

		token = generatedToken
		fmt.Println("Successfully generated installation token")
	}

	// Get clone directory from flag or environment variable
	if cloneDir == "" {
		cloneDir = os.Getenv("GITHUB_CLONE_DIR")
	}
	if cloneDir == "" {
		var err error
		cloneDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Convert fullname to URL for cloning
	githubURL := getGitHubURL()
	repoURL := fmt.Sprintf("%s/%s", githubURL, repoFullName)

	// Setup repository
	if err := setupRepository(repoURL, token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	// Setup MCP integration
	if err := setupMCPIntegration(token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup MCP integration: %w", err)
	}

	// Save environment variables for future use
	if err := saveEnvironmentVariables(token, cloneDir); err != nil {
		fmt.Printf("Warning: failed to save environment variables: %v\n", err)
	}

	fmt.Println("GitHub repository initialization completed successfully!")
	return nil
}

func getAPIBase() string {
	apiBase := os.Getenv("GITHUB_API")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return apiBase
}

func getGitHubURL() string {
	githubURL := os.Getenv("GITHUB_URL")
	if githubURL == "" {
		githubURL = "https://github.com"
	}
	return githubURL
}

func generateInstallationToken(config GitHubAppConfig) (string, error) {
	// Read the private key
	pemData, err := os.ReadFile(config.PEMPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PEM file: %w", err)
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, config.AppID, pemData)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if config.APIBase != "" && config.APIBase != "https://api.github.com" {
		transport.BaseURL = config.APIBase
	}

	// Create GitHub client
	var client *github.Client
	if config.APIBase == "" || strings.Contains(config.APIBase, "https://api.github.com") {
		client = github.NewClient(&http.Client{Transport: transport})
	} else {
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(config.APIBase, config.APIBase)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	// Create installation token
	ctx := context.Background()
	token, _, err := client.Apps.CreateInstallationToken(
		ctx,
		config.InstallationID,
		&github.InstallationTokenOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}

	return token.GetToken(), nil
}

func setupRepository(repoURL, token, cloneDir string) error {
	fmt.Printf("Setting up repository in: %s\n", cloneDir)

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
		fmt.Println("Git repository detected, updating...")
		cmd := exec.Command("git", "pull")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to pull updates: %w", err)
		}
	} else {
		fmt.Println("Initializing new git repository...")

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

	fmt.Println("Repository setup completed")
	return nil
}

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
	}

	return "", fmt.Errorf("unsupported repository URL format: %s", repoURL)
}

func setupMCPIntegration(token, cloneDir string) error {
	fmt.Println("Setting up MCP integration...")

	// Add GitHub MCP server to Claude
	cmd := exec.Command("claude",
		"mcp", "add", "github",
		"--",
		"docker", "run", "-i", "--rm",
		"-e", "GITHUB_PERSONAL_ACCESS_TOKEN="+token,
		"ghcr.io/github/github-mcp-server")
	cmd.Dir = cloneDir
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: failed to add GitHub MCP server to Claude: %v\n", err)
	}
	fmt.Println("MCP integration setup completed")
	return nil
}

func saveEnvironmentVariables(token, cloneDir string) error {
	pid := os.Getpid()
	envFile := fmt.Sprintf("/tmp/github_env_%d", pid)

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("GITHUB_TOKEN=%s\n", token))
	buf.WriteString(fmt.Sprintf("GITHUB_CLONE_DIR=%s\n", cloneDir))

	return os.WriteFile(envFile, buf.Bytes(), 0600)
}

func parseAppID(appIDStr string) (int64, error) {
	if appIDStr == "" {
		return 0, fmt.Errorf("GITHUB_APP_ID is required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("GITHUB_APP_ID must be a valid integer: %w", err)
	}
	return appID, nil
}

func parseInstallationID(installationIDStr string) (int64, error) {
	if installationIDStr == "" {
		return 0, fmt.Errorf("GITHUB_INSTALLATION_ID is required")
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("GITHUB_INSTALLATION_ID must be a valid integer: %w", err)
	}
	return installationID, nil
}

func findInstallationIDForRepo(appID int64, pemPath, repoFullName, apiBase string) (int64, error) {
	// Parse repository owner and name from fullname
	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid repository fullname format, expected 'owner/repo': %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	// Read the private key
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read PEM file: %w", err)
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pemData)
	if err != nil {
		return 0, fmt.Errorf("failed to create GitHub App transport: %w", err)
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
			return 0, fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	ctx := context.Background()

	// List installations for the app
	installations, _, err := client.Apps.ListInstallations(ctx, &github.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list installations: %w", err)
	}

	// Check each installation for repository access
	for _, installation := range installations {
		installationID := installation.GetID()

		// Create installation client to check repository access
		installationTransport := ghinstallation.NewFromAppsTransport(transport, installationID)
		var installationClient *github.Client
		if apiBase == "" || strings.Contains(apiBase, "https://api.github.com") {
			installationClient = github.NewClient(&http.Client{Transport: installationTransport})
		} else {
			installationClient, err = github.NewClient(&http.Client{Transport: installationTransport}).WithEnterpriseURLs(apiBase, apiBase)
			if err != nil {
				continue // Skip this installation if we can't create a client
			}
		}

		// Try to access the repository with this installation
		_, _, err := installationClient.Repositories.Get(ctx, owner, repo)
		if err == nil {
			// Successfully accessed the repository with this installation
			return installationID, nil
		}
	}

	return 0, fmt.Errorf("no installation found with access to repository %s/%s", owner, repo)
}

func init() {
	initGitHubRepoCmd.Flags().BoolVar(&ignoreMissingConfig, "ignore-missing-config", false, "Skip execution when required configuration is not provided")
	initGitHubRepoCmd.Flags().StringVar(&repoFullName, "repo-fullname", "", "GitHub repository in org/repo format")
	initGitHubRepoCmd.Flags().StringVar(&cloneDir, "clone-dir", "", "Target directory for cloning")
	HelpersCmd.AddCommand(initGitHubRepoCmd)
}
