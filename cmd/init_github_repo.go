package cmd

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/spf13/cobra"
)

type GitHubAppConfig struct {
	AppID          string
	InstallationID string
	PEMPath        string
	APIBase        string
}

type InstallationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

var initGitHubRepoCmd = &cobra.Command{
	Use:   "init-github-repository",
	Short: "Initialize GitHub repository with authentication",
	Long: `Initialize GitHub repository with proper authentication.
This command will:
1. Validate GitHub authentication (token or app credentials)
2. Clone or update the specified GitHub repository
3. Set up MCP integration with Claude

Required environment variables:
- GITHUB_REPO_URL: The GitHub repository URL to clone

Authentication options (one required):
- GITHUB_TOKEN: Personal access token or existing token
- GitHub App credentials:
  - GITHUB_APP_PEM_PATH: Path to private key file
  - GITHUB_APP_ID: GitHub App ID
  - GITHUB_INSTALLATION_ID: Installation ID

Optional:
- GITHUB_CLONE_DIR: Target directory for cloning (defaults to current directory)
- GITHUB_API: GitHub API base URL (defaults to https://api.github.com)
`,
	RunE: runInitGitHubRepo,
}

func runInitGitHubRepo(cmd *cobra.Command, args []string) error {
	// Validate required environment variables
	repoURL := os.Getenv("GITHUB_REPO_URL")
	if repoURL == "" {
		return fmt.Errorf("GITHUB_REPO_URL environment variable is required")
	}

	token := os.Getenv("GITHUB_TOKEN")

	// If no token provided, try to generate one from GitHub App credentials
	if token == "" {
		fmt.Println("No GITHUB_TOKEN found, attempting to generate from GitHub App credentials...")

		appConfig := GitHubAppConfig{
			AppID:          os.Getenv("GITHUB_APP_ID"),
			InstallationID: os.Getenv("GITHUB_INSTALLATION_ID"),
			PEMPath:        os.Getenv("GITHUB_APP_PEM_PATH"),
			APIBase:        getAPIBase(),
		}

		if appConfig.AppID == "" || appConfig.InstallationID == "" || appConfig.PEMPath == "" {
			return fmt.Errorf("either GITHUB_TOKEN or GitHub App credentials (GITHUB_APP_ID, GITHUB_INSTALLATION_ID, GITHUB_APP_PEM_PATH) must be provided")
		}

		generatedToken, err := generateInstallationToken(appConfig)
		if err != nil {
			return fmt.Errorf("failed to generate installation token: %w", err)
		}

		token = generatedToken
		fmt.Println("Successfully generated installation token")
	}

	// Get clone directory
	cloneDir := os.Getenv("GITHUB_CLONE_DIR")
	if cloneDir == "" {
		var err error
		cloneDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Setup repository
	if err := setupRepository(repoURL, token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	// Setup MCP integration
	if err := setupMCPIntegration(token); err != nil {
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

func generateInstallationToken(config GitHubAppConfig) (string, error) {
	// Read the private key
	pemData, err := os.ReadFile(config.PEMPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PEM file: %w", err)
	}

	// Parse the private key
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	if block.Type == "RSA PRIVATE KEY" {
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("failed to parse PKCS8 private key: %w", err)
		}
		var ok bool
		privateKey, ok = parsedKey.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("parsed key is not RSA private key")
		}
	} else {
		return "", fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}

	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create JWT token
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": config.AppID,
	})

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Exchange JWT for installation token
	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", config.APIBase, config.InstallationID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "agentapi-proxy-helper")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp InstallationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return tokenResp.Token, nil
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
	// Parse the repository URL and insert the token
	if strings.HasPrefix(repoURL, "https://github.com/") {
		parts := strings.TrimPrefix(repoURL, "https://github.com/")
		return fmt.Sprintf("https://%s@github.com/%s", token, parts), nil
	} else if strings.HasPrefix(repoURL, "git@github.com:") {
		parts := strings.TrimPrefix(repoURL, "git@github.com:")
		parts = strings.TrimSuffix(parts, ".git")
		return fmt.Sprintf("https://%s@github.com/%s.git", token, parts), nil
	}

	return "", fmt.Errorf("unsupported repository URL format: %s", repoURL)
}

func setupMCPIntegration(token string) error {
	fmt.Println("Setting up MCP integration...")

	// Add GitHub MCP server to Claude
	cmd := exec.Command("mise", "exec", "--", "claude", "mcp", "add", "github")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: failed to add GitHub MCP server to Claude: %v\n", err)
	}

	// Run GitHub MCP server as Docker container
	dockerCmd := exec.Command("docker", "run", "-d",
		"--name", "github-mcp-server",
		"-e", "GITHUB_TOKEN="+token,
		"github-mcp-server")

	if err := dockerCmd.Run(); err != nil {
		fmt.Printf("Warning: failed to start GitHub MCP server container: %v\n", err)
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

func init() {
	HelpersCmd.AddCommand(initGitHubRepoCmd)
}
