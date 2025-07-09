package proxy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestMergeEnvironmentVariables(t *testing.T) {
	// Create temporary directory for test env files
	tempDir := t.TempDir()

	// Create test role env file
	roleEnvFile := filepath.Join(tempDir, "test-role.env")
	roleEnvContent := `# Role environment variables
ROLE_VAR=role_value
COMMON_VAR=role_common
OVERRIDE_VAR=role_override
`
	if err := os.WriteFile(roleEnvFile, []byte(roleEnvContent), 0644); err != nil {
		t.Fatalf("Failed to create role env file: %v", err)
	}

	// Create test team env file
	teamEnvFile := filepath.Join(tempDir, "team.env")
	teamEnvContent := `# Team environment variables
TEAM_VAR=team_value
COMMON_VAR=team_common
OVERRIDE_VAR=team_override
`
	if err := os.WriteFile(teamEnvFile, []byte(teamEnvContent), 0644); err != nil {
		t.Fatalf("Failed to create team env file: %v", err)
	}

	tests := []struct {
		name     string
		config   EnvMergeConfig
		expected map[string]string
		wantErr  bool
	}{
		{
			name: "empty config returns empty map",
			config: EnvMergeConfig{},
			expected: map[string]string{},
			wantErr: false,
		},
		{
			name: "only request environment variables",
			config: EnvMergeConfig{
				RequestEnv: map[string]string{
					"REQUEST_VAR1": "value1",
					"REQUEST_VAR2": "value2",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR1": "value1",
				"REQUEST_VAR2": "value2",
			},
			wantErr: false,
		},
		{
			name: "only role-based environment variables",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    tempDir,
				},
				UserRole: "test-role",
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"COMMON_VAR":   "role_common",
				"OVERRIDE_VAR": "role_override",
			},
			wantErr: false,
		},
		{
			name: "role + team environment variables (team overrides role)",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    tempDir,
				},
				UserRole:    "test-role",
				TeamEnvFile: teamEnvFile,
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"TEAM_VAR":     "team_value",
				"COMMON_VAR":   "team_common",  // team overrides role
				"OVERRIDE_VAR": "team_override", // team overrides role
			},
			wantErr: false,
		},
		{
			name: "role + auth team environment variables (auth team overrides role)",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    tempDir,
				},
				UserRole:        "test-role",
				AuthTeamEnvFile: teamEnvFile,
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"TEAM_VAR":     "team_value",
				"COMMON_VAR":   "team_common",  // auth team overrides role
				"OVERRIDE_VAR": "team_override", // auth team overrides role
			},
			wantErr: false,
		},
		{
			name: "role + auth team + team environment variables (team overrides auth team)",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    tempDir,
				},
				UserRole:        "test-role",
				AuthTeamEnvFile: teamEnvFile,
				TeamEnvFile:     teamEnvFile, // Same file for simplicity
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"TEAM_VAR":     "team_value",
				"COMMON_VAR":   "team_common",  // team overrides auth team
				"OVERRIDE_VAR": "team_override", // team overrides auth team
			},
			wantErr: false,
		},
		{
			name: "role + auth team + team + request (request has highest priority)",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    tempDir,
				},
				UserRole:        "test-role",
				AuthTeamEnvFile: teamEnvFile,
				TeamEnvFile:     teamEnvFile,
				RequestEnv: map[string]string{
					"REQUEST_VAR":  "request_value",
					"COMMON_VAR":   "request_common",
					"OVERRIDE_VAR": "request_override",
				},
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"TEAM_VAR":     "team_value",
				"REQUEST_VAR":  "request_value",
				"COMMON_VAR":   "request_common",   // request overrides all
				"OVERRIDE_VAR": "request_override", // request overrides all
			},
			wantErr: false,
		},
		{
			name: "non-existent team env file",
			config: EnvMergeConfig{
				TeamEnvFile: "/non/existent/file.env",
				RequestEnv: map[string]string{
					"REQUEST_VAR": "value",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR": "value",
			},
			wantErr: false, // Should not fail, just log warning
		},
		{
			name: "role env files disabled",
			config: EnvMergeConfig{
				RoleEnvFiles: &config.RoleEnvFilesConfig{
					Enabled: false,
					Path:    tempDir,
				},
				UserRole: "test-role",
				RequestEnv: map[string]string{
					"REQUEST_VAR": "value",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR": "value",
			},
			wantErr: false,
		},
		{
			name: "nil role env files config",
			config: EnvMergeConfig{
				RoleEnvFiles: nil,
				UserRole:     "test-role",
				RequestEnv: map[string]string{
					"REQUEST_VAR": "value",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR": "value",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeEnvironmentVariables(tt.config)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("MergeEnvironmentVariables() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("MergeEnvironmentVariables() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractTeamEnvFile(t *testing.T) {
	tests := []struct {
		name     string
		tags     map[string]string
		expected string
	}{
		{
			name:     "nil tags returns empty string",
			tags:     nil,
			expected: "",
		},
		{
			name:     "empty tags returns empty string",
			tags:     map[string]string{},
			expected: "",
		},
		{
			name: "tags with env_file returns value",
			tags: map[string]string{
				"env_file": "/path/to/env/file",
				"other":    "value",
			},
			expected: "/path/to/env/file",
		},
		{
			name: "tags without env_file returns empty string",
			tags: map[string]string{
				"other": "value",
				"tag":   "value2",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTeamEnvFile(tt.tags)
			if got != tt.expected {
				t.Errorf("ExtractTeamEnvFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMergeEnvironmentVariablesPriority(t *testing.T) {
	// Create temporary directory for test env files
	tempDir := t.TempDir()

	// Create multiple env files with same keys
	roleEnvFile := filepath.Join(tempDir, "role.env")
	if err := os.WriteFile(roleEnvFile, []byte("PRIORITY_TEST=role"), 0644); err != nil {
		t.Fatalf("Failed to create role env file: %v", err)
	}

	teamEnvFile := filepath.Join(tempDir, "team.env")
	if err := os.WriteFile(teamEnvFile, []byte("PRIORITY_TEST=team"), 0644); err != nil {
		t.Fatalf("Failed to create team env file: %v", err)
	}

	config := EnvMergeConfig{
		RoleEnvFiles: &config.RoleEnvFilesConfig{
			Enabled: true,
			Path:    tempDir,
		},
		UserRole:    "role",
		TeamEnvFile: teamEnvFile,
		RequestEnv: map[string]string{
			"PRIORITY_TEST": "request",
		},
	}

	got, err := MergeEnvironmentVariables(config)
	if err != nil {
		t.Fatalf("MergeEnvironmentVariables() error = %v", err)
	}

	// Request value should win
	if got["PRIORITY_TEST"] != "request" {
		t.Errorf("Expected PRIORITY_TEST=request, got %s", got["PRIORITY_TEST"])
	}

	// Test without request env
	config.RequestEnv = nil
	got, err = MergeEnvironmentVariables(config)
	if err != nil {
		t.Fatalf("MergeEnvironmentVariables() error = %v", err)
	}

	// Team value should win
	if got["PRIORITY_TEST"] != "team" {
		t.Errorf("Expected PRIORITY_TEST=team, got %s", got["PRIORITY_TEST"])
	}

	// Test without team env
	config.TeamEnvFile = ""
	got, err = MergeEnvironmentVariables(config)
	if err != nil {
		t.Fatalf("MergeEnvironmentVariables() error = %v", err)
	}

	// Role value should be used
	if got["PRIORITY_TEST"] != "role" {
		t.Errorf("Expected PRIORITY_TEST=role, got %s", got["PRIORITY_TEST"])
	}
}