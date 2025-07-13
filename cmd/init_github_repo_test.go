package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-github/v57/github"
)

func TestGenerateInstallationToken(t *testing.T) {
	// Generate a test RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}

	// Create a temporary PEM file
	pemFile, err := createTempPEMFile(privateKey)
	if err != nil {
		t.Fatalf("Failed to create temp PEM file: %v", err)
	}
	defer func() { _ = os.Remove(pemFile) }()

	// Mock GitHub API server that handles installation token requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle installation token request with API v3 prefix
		if r.URL.Path == "/api/v3/app/installations/12345/access_tokens" && r.Method == "POST" {
			auth := r.Header.Get("Authorization")
			if auth == "" || !startsWith(auth, "Bearer ") {
				t.Errorf("Expected Bearer token in Authorization header, got %s", auth)
			}

			w.WriteHeader(http.StatusCreated)
			response := map[string]interface{}{
				"token":      "test-installation-token",
				"expires_at": "2025-06-09T04:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		// For go-github library, it may make additional requests for JWT authentication
		if r.Method == "GET" && (r.URL.Path == "/api/v3/app" || r.URL.Path == "/api/v3/user") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{}`)
			return
		}

		t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        pemFile,
		APIBase:        server.URL,
	}

	token, err := generateInstallationToken(config)
	if err != nil {
		t.Fatalf("generateInstallationToken failed: %v", err)
	}

	if token != "test-installation-token" {
		t.Errorf("Expected token 'test-installation-token', got '%s'", token)
	}
}

func TestGenerateInstallationTokenWithInvalidPEM(t *testing.T) {
	// Create a temporary invalid PEM file
	tmpFile, err := os.CreateTemp("", "invalid-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, _ = tmpFile.WriteString("invalid pem content")
	_ = tmpFile.Close()

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        tmpFile.Name(),
		APIBase:        "https://api.github.com",
	}

	_, err = generateInstallationToken(config)
	if err == nil {
		t.Error("Expected error for invalid PEM file, got nil")
	}
}

func TestGenerateInstallationTokenWithAPIError(t *testing.T) {
	// Generate a test RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}

	// Create a temporary PEM file
	pemFile, err := createTempPEMFile(privateKey)
	if err != nil {
		t.Fatalf("Failed to create temp PEM file: %v", err)
	}
	defer func() { _ = os.Remove(pemFile) }()

	// Mock GitHub API server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"message":"Bad credentials"}`)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        pemFile,
		APIBase:        server.URL,
	}

	_, err = generateInstallationToken(config)
	if err == nil {
		t.Error("Expected error for API error response, got nil")
	}
}

func TestCreateAuthenticatedURL(t *testing.T) {
	tests := []struct {
		name        string
		repoURL     string
		token       string
		githubURL   string
		expected    string
		expectError bool
	}{
		{
			name:      "HTTPS URL",
			repoURL:   "https://github.com/user/repo",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo",
		},
		{
			name:      "SSH URL",
			repoURL:   "git@github.com:user/repo.git",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo.git",
		},
		{
			name:      "SSH URL without .git",
			repoURL:   "git@github.com:user/repo",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo.git",
		},
		{
			name:      "Enterprise GitHub HTTPS URL",
			repoURL:   "https://github.enterprise.com/user/repo",
			token:     "test-token",
			githubURL: "https://github.enterprise.com",
			expected:  "https://test-token@github.enterprise.com/user/repo",
		},
		{
			name:      "Enterprise GitHub SSH URL",
			repoURL:   "git@github.enterprise.com:user/repo.git",
			token:     "test-token",
			githubURL: "https://github.enterprise.com",
			expected:  "https://test-token@github.enterprise.com/user/repo.git",
		},
		{
			name:        "Invalid URL",
			repoURL:     "invalid-url",
			token:       "test-token",
			githubURL:   "https://github.com",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set GITHUB_URL environment variable
			if tt.githubURL != "" {
				_ = os.Setenv("GITHUB_URL", tt.githubURL)
				defer func() { _ = os.Unsetenv("GITHUB_URL") }()
			} else {
				_ = os.Unsetenv("GITHUB_URL")
			}

			result, err := createAuthenticatedURL(tt.repoURL, tt.token)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetAPIBase(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default API base",
			envValue: "",
			expected: "https://api.github.com",
		},
		{
			name:     "Custom API base",
			envValue: "https://github.enterprise.com/api/v3",
			expected: "https://github.enterprise.com/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				_ = os.Setenv("GITHUB_API", tt.envValue)
				defer func() { _ = os.Unsetenv("GITHUB_API") }()
			} else {
				_ = os.Unsetenv("GITHUB_API")
			}

			result := getAPIBase()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default GitHub URL",
			envValue: "",
			expected: "https://github.com",
		},
		{
			name:     "Custom GitHub URL",
			envValue: "https://github.enterprise.com",
			expected: "https://github.enterprise.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				_ = os.Setenv("GITHUB_URL", tt.envValue)
				defer func() { _ = os.Unsetenv("GITHUB_URL") }()
			} else {
				_ = os.Unsetenv("GITHUB_URL")
			}

			result := getGitHubURL()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSaveEnvironmentVariables(t *testing.T) {
	token := "test-token"
	cloneDir := "/test/path"

	err := saveEnvironmentVariables(token, cloneDir)
	if err != nil {
		t.Fatalf("saveEnvironmentVariables failed: %v", err)
	}

	// Check if the file was created
	pid := os.Getpid()
	envFile := fmt.Sprintf("/tmp/github_env_%d", pid)
	defer func() { _ = os.Remove(envFile) }()

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("Failed to read environment file: %v", err)
	}

	expectedContent := fmt.Sprintf("GITHUB_TOKEN=%s\nGITHUB_CLONE_DIR=%s\n", token, cloneDir)
	if string(content) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(content))
	}

	// Check file permissions
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("Failed to stat environment file: %v", err)
	}

	expectedMode := os.FileMode(0600)
	if info.Mode() != expectedMode {
		t.Errorf("Expected file mode %o, got %o", expectedMode, info.Mode())
	}
}

func TestRunInitGitHubRepoValidation(t *testing.T) {
	// Test missing GITHUB_REPO_FULLNAME
	_ = os.Unsetenv("GITHUB_REPO_FULLNAME")
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_APP_ID")
	_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
	_ = os.Unsetenv("GITHUB_APP_PEM_PATH")

	err := RunInitGitHubRepo("", "", false)
	if err == nil || !contains(err.Error(), "repository fullname is required") {
		t.Errorf("Expected error about missing repository fullname, got: %v", err)
	}

	// Test missing authentication
	err = RunInitGitHubRepo("test/repo", "", false)
	if err == nil || !contains(err.Error(), "GITHUB_APP_ID") {
		t.Errorf("Expected error about invalid GITHUB_APP_ID, got: %v", err)
	}
}

func TestParseAppID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "Valid app ID",
			input:    "123",
			expected: 123,
		},
		{
			name:     "Large app ID",
			input:    "123456789",
			expected: 123456789,
		},
		{
			name:        "Empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "Invalid number",
			input:       "abc",
			expectError: true,
		},
		{
			name:     "Negative number",
			input:    "-123",
			expected: -123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAppID(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestParseInstallationID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "Valid installation ID",
			input:    "12345",
			expected: 12345,
		},
		{
			name:     "Large installation ID",
			input:    "987654321",
			expected: 987654321,
		},
		{
			name:        "Empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "Invalid number",
			input:       "xyz",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInstallationID(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestSetupRepository(t *testing.T) {
	// Save original PATH for restoration
	originalPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", originalPath) }()

	tests := []struct {
		name        string
		repoURL     string
		token       string
		cloneDir    string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
		errorMsg    string
	}{
		{
			name:     "Clone new repository success",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "Fetched"
    ;;
  "checkout")
    if [ "$2" = "main" ]; then
      echo "Switched to branch 'main'"
      exit 0
    elif [ "$2" = "master" ]; then
      echo "Switched to branch 'master'"
      exit 0
    fi
    exit 1
    ;;
  *)
    echo "Unknown git command: $1"
    exit 1
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				gitPath := mockPath + "/git"
				_ = os.WriteFile(gitPath, []byte(mockGit), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "Pull existing repository success",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create .git directory to simulate existing repo
				_ = os.MkdirAll(tmpDir+"/.git", 0755)

				// Create mock git executable
				mockGit := `#!/bin/bash
if [ "$1" = "pull" ]; then
  echo "Already up to date."
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				gitPath := mockPath + "/git"
				_ = os.WriteFile(gitPath, []byte(mockGit), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "Git init fails",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on init
				mockGit := `#!/bin/bash
if [ "$1" = "init" ]; then
  echo "fatal: cannot create directory" >&2
  exit 1
fi
exit 0`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				gitPath := mockPath + "/git"
				_ = os.WriteFile(gitPath, []byte(mockGit), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to initialize git",
		},
		{
			name:     "Git fetch fails",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on fetch
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "fatal: could not read from remote repository" >&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				gitPath := mockPath + "/git"
				_ = os.WriteFile(gitPath, []byte(mockGit), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to fetch",
		},
		{
			name:     "No main or master branch",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on checkout
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "Fetched"
    ;;
  "checkout")
    echo "error: pathspec '$2' did not match any file(s) known to git" >&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				gitPath := mockPath + "/git"
				_ = os.WriteFile(gitPath, []byte(mockGit), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to checkout main or master branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "test-repo-")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			// Use tmpDir as cloneDir if not specified
			if tt.cloneDir == "" {
				tt.cloneDir = tmpDir
			}

			// Setup mock
			if tt.setupMock != nil {
				tt.setupMock(t, tmpDir)
			}

			// Test setupRepository
			err = setupRepository(tt.repoURL, tt.token, tt.cloneDir)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTTPS URL",
			input:    "https://github.com",
			expected: "github.com",
		},
		{
			name:     "HTTP URL",
			input:    "http://github.com",
			expected: "github.com",
		},
		{
			name:     "URL with path",
			input:    "https://github.com/api/v3",
			expected: "github.com",
		},
		{
			name:     "URL with port",
			input:    "https://github.enterprise.com:8080",
			expected: "github.enterprise.com:8080",
		},
		{
			name:     "Just hostname",
			input:    "github.com",
			expected: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHostname(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAuthenticateGHCLI(t *testing.T) {
	// Save original PATH
	originalPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", originalPath) }()

	tests := []struct {
		name        string
		githubURL   string
		token       string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
	}{
		{
			name:      "Successful authentication",
			githubURL: "https://github.enterprise.com",
			token:     "test-token",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock gh executable
				mockGH := `#!/bin/bash
if [ "$1" = "auth" ] && [ "$2" = "login" ]; then
  # Read token from stdin
  read token
  echo "✓ Logged in to github.enterprise.com"
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				ghPath := mockPath + "/gh"
				_ = os.WriteFile(ghPath, []byte(mockGH), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:      "Authentication failure",
			githubURL: "https://github.enterprise.com",
			token:     "invalid-token",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock gh executable that fails
				mockGH := `#!/bin/bash
if [ "$1" = "auth" ] && [ "$2" = "login" ]; then
  echo "error: authentication failed" >&2
  exit 1
fi
exit 0`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				ghPath := mockPath + "/gh"
				_ = os.WriteFile(ghPath, []byte(mockGH), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "test-gh-")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			// Setup mock
			if tt.setupMock != nil {
				tt.setupMock(t, tmpDir)
			}

			// Test authenticateGHCLI
			err = authenticateGHCLI(tt.githubURL, tt.token)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSetupMCPIntegration(t *testing.T) {
	// Save original PATH
	originalPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", originalPath) }()

	tests := []struct {
		name        string
		token       string
		cloneDir    string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
	}{
		{
			name:     "Successful MCP setup",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock claude executable
				mockClaude := `#!/bin/bash
if [ "$1" = "mcp" ] && [ "$2" = "add" ] && [ "$3" = "github" ]; then
  echo "✓ Added GitHub MCP server"
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				claudePath := mockPath + "/claude"
				_ = os.WriteFile(claudePath, []byte(mockClaude), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "MCP setup fails",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock claude executable that fails
				mockClaude := `#!/bin/bash
echo "error: failed to add MCP server" >&2
exit 1`
				mockPath := tmpDir + "/mock-bin"
				_ = os.MkdirAll(mockPath, 0755)
				claudePath := mockPath + "/claude"
				_ = os.WriteFile(claudePath, []byte(mockClaude), 0755)
				_ = os.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false, // setupMCPIntegration only warns on failure
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "test-mcp-")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			// Use tmpDir as cloneDir if not specified
			if tt.cloneDir == "" {
				tt.cloneDir = tmpDir
			}

			// Setup mock
			if tt.setupMock != nil {
				tt.setupMock(t, tmpDir)
			}

			// Test setupMCPIntegration
			err = setupMCPIntegration(tt.token, tt.cloneDir)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestFindInstallationIDForRepo(t *testing.T) {
	// Generate a test RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}

	// Create a temporary PEM file
	pemFile, err := createTempPEMFile(privateKey)
	if err != nil {
		t.Fatalf("Failed to create temp PEM file: %v", err)
	}
	defer func() { _ = os.Remove(pemFile) }()

	tests := []struct {
		name          string
		appID         int64
		repoFullName  string
		apiBase       string
		serverHandler http.HandlerFunc
		expectedID    int64
		expectError   bool
		errorContains string
	}{
		{
			name:         "Found installation with access",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Log the request for debugging
				t.Logf("Request: %s %s", r.Method, r.URL.Path)

				switch r.URL.Path {
				case "/app/installations", "/api/v3/app/installations":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					// GitHub API returns array of Installation objects
					response := []*github.Installation{
						{ID: github.Int64(12345)},
						{ID: github.Int64(67890)},
					}
					_ = json.NewEncoder(w).Encode(response)
				case "/repos/owner/repo", "/api/v3/repos/owner/repo":
					// Return 200 for the first installation
					if r.Header.Get("Authorization") != "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = fmt.Fprint(w, `{"id": 123456, "name": "repo", "owner": {"login": "owner"}}`)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case "/app", "/user", "/api/v3/app", "/api/v3/user":
					// These endpoints are called during authentication
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{}`)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{}`)
				}
			},
			expectedID:  12345,
			expectError: false,
		},
		{
			name:         "No installation with access",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Log the request for debugging
				t.Logf("Request: %s %s", r.Method, r.URL.Path)

				switch r.URL.Path {
				case "/app/installations", "/api/v3/app/installations":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					// GitHub API returns array of Installation objects
					response := []*github.Installation{
						{ID: github.Int64(12345)},
					}
					_ = json.NewEncoder(w).Encode(response)
				case "/repos/owner/repo", "/api/v3/repos/owner/repo":
					// Return 404 for all installations
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message": "Not Found"}`)
				case "/app", "/user", "/api/v3/app", "/api/v3/user":
					// These endpoints are called during authentication
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{}`)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{}`)
				}
			},
			expectError:   true,
			errorContains: "no installation found with access",
		},
		{
			name:         "Invalid repo fullname format",
			appID:        123,
			repoFullName: "invalid-format",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{}`)
			},
			expectError:   true,
			errorContains: "invalid repository fullname format",
		},
		{
			name:         "API error listing installations",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app/installations" || r.URL.Path == "/api/v3/app/installations" {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprint(w, `{"message": "Internal server error"}`)
				} else {
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{}`)
				}
			},
			expectError:   true,
			errorContains: "failed to list installations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before each test to ensure independence
			installationCache.ClearCache()
			
			// Create test server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Use server URL as API base if not specified
			apiBase := tt.apiBase
			if apiBase == "" {
				apiBase = server.URL
			}

			// Test findInstallationIDForRepo
			result, err := findInstallationIDForRepo(tt.appID, pemFile, tt.repoFullName, apiBase)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expectedID {
					t.Errorf("Expected installation ID %d, got %d", tt.expectedID, result)
				}
			}
		})
	}
}

// Helper functions

func createTempPEMFile(privateKey *rsa.PrivateKey) (string, error) {
	tmpFile, err := os.CreateTemp("", "test-key-*.pem")
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpFile.Close() }()

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	if err := pem.Encode(tmpFile, privateKeyPEM); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findIndex(s, substr) >= 0
}

func findIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
