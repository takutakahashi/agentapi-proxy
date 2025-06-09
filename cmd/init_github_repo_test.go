package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
	defer os.Remove(pemFile)

	// Mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if r.URL.Path != "/app/installations/12345/access_tokens" {
			t.Errorf("Expected path /app/installations/12345/access_tokens, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth == "" || !startsWith(auth, "Bearer ") {
			t.Errorf("Expected Bearer token in Authorization header, got %s", auth)
		}

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"token":"test-installation-token","expires_at":"2025-06-09T04:00:00Z"}`)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          "123",
		InstallationID: "12345",
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
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("invalid pem content")
	tmpFile.Close()

	config := GitHubAppConfig{
		AppID:          "123",
		InstallationID: "12345",
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
	defer os.Remove(pemFile)

	// Mock GitHub API server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          "123",
		InstallationID: "12345",
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
		expected    string
		expectError bool
	}{
		{
			name:     "HTTPS URL",
			repoURL:  "https://github.com/user/repo",
			token:    "test-token",
			expected: "https://test-token@github.com/user/repo",
		},
		{
			name:     "SSH URL",
			repoURL:  "git@github.com:user/repo.git",
			token:    "test-token",
			expected: "https://test-token@github.com/user/repo.git",
		},
		{
			name:     "SSH URL without .git",
			repoURL:  "git@github.com:user/repo",
			token:    "test-token",
			expected: "https://test-token@github.com/user/repo.git",
		},
		{
			name:        "Invalid URL",
			repoURL:     "invalid-url",
			token:       "test-token",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				os.Setenv("GITHUB_API", tt.envValue)
				defer os.Unsetenv("GITHUB_API")
			} else {
				os.Unsetenv("GITHUB_API")
			}

			result := getAPIBase()
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
	defer os.Remove(envFile)

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
	// Test missing GITHUB_REPO_URL
	os.Unsetenv("GITHUB_REPO_URL")
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GITHUB_APP_ID")
	os.Unsetenv("GITHUB_INSTALLATION_ID")
	os.Unsetenv("GITHUB_APP_PEM_PATH")

	err := runInitGitHubRepo(nil, nil)
	if err == nil || !contains(err.Error(), "GITHUB_REPO_URL") {
		t.Errorf("Expected error about missing GITHUB_REPO_URL, got: %v", err)
	}

	// Test missing authentication
	os.Setenv("GITHUB_REPO_URL", "https://github.com/test/repo")
	defer os.Unsetenv("GITHUB_REPO_URL")

	err = runInitGitHubRepo(nil, nil)
	if err == nil || !contains(err.Error(), "GITHUB_TOKEN or GitHub App credentials") {
		t.Errorf("Expected error about missing authentication, got: %v", err)
	}
}

// Helper functions

func createTempPEMFile(privateKey *rsa.PrivateKey) (string, error) {
	tmpFile, err := os.CreateTemp("", "test-key-*.pem")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

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
