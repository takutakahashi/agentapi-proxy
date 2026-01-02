package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestIsAWSAccessKeyID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid permanent key AKIA",
			input:    "AKIAIOSFODNN7EXAMPLE",
			expected: true,
		},
		{
			name:     "valid temporary key ASIA",
			input:    "ASIAIOSFODNN7EXAMPLE",
			expected: true,
		},
		{
			name:     "too short",
			input:    "AKIA1234",
			expected: false,
		},
		{
			name:     "too long",
			input:    "AKIAIOSFODNN7EXAMPLEEXTRA",
			expected: false,
		},
		{
			name:     "wrong prefix",
			input:    "ABCDIOSFODNN7EXAMPLE",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "github token",
			input:    "ghp_xxxxxxxxxxxxxxxxxxxx",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAWSAccessKeyID(tt.input)
			if result != tt.expected {
				t.Errorf("IsAWSAccessKeyID(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractAWSCredentialsFromBasicAuth(t *testing.T) {
	tests := []struct {
		name            string
		username        string
		password        string
		sessionToken    string
		expectFound     bool
		expectAccessKey string
		expectSecret    string
		expectSession   string
	}{
		{
			name:            "valid permanent credentials",
			username:        "AKIAIOSFODNN7EXAMPLE",
			password:        "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			sessionToken:    "",
			expectFound:     true,
			expectAccessKey: "AKIAIOSFODNN7EXAMPLE",
			expectSecret:    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectSession:   "",
		},
		{
			name:            "valid temporary credentials with session token",
			username:        "ASIAIOSFODNN7EXAMPLE",
			password:        "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			sessionToken:    "FwoGZXIvYXdzEBYaDEjK...",
			expectFound:     true,
			expectAccessKey: "ASIAIOSFODNN7EXAMPLE",
			expectSecret:    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectSession:   "FwoGZXIvYXdzEBYaDEjK...",
		},
		{
			name:        "not AWS credentials (GitHub token)",
			username:    "ghp_xxxxxxxxxxxxxxxxxxxx",
			password:    "password",
			expectFound: false,
		},
		{
			name:        "not AWS credentials (short key)",
			username:    "AKIA1234",
			password:    "password",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.SetBasicAuth(tt.username, tt.password)
			if tt.sessionToken != "" {
				req.Header.Set("X-AWS-Session-Token", tt.sessionToken)
			}

			creds, found := ExtractAWSCredentialsFromBasicAuth(req)

			if found != tt.expectFound {
				t.Errorf("ExtractAWSCredentialsFromBasicAuth() found = %v, want %v", found, tt.expectFound)
				return
			}

			if !tt.expectFound {
				return
			}

			if creds.AccessKeyID != tt.expectAccessKey {
				t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, tt.expectAccessKey)
			}
			if creds.SecretAccessKey != tt.expectSecret {
				t.Errorf("SecretAccessKey = %q, want %q", creds.SecretAccessKey, tt.expectSecret)
			}
			if creds.SessionToken != tt.expectSession {
				t.Errorf("SessionToken = %q, want %q", creds.SessionToken, tt.expectSession)
			}
		})
	}
}

func TestExtractAWSCredentialsFromBasicAuth_NoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	// No Basic Auth header

	creds, found := ExtractAWSCredentialsFromBasicAuth(req)

	if found {
		t.Error("Expected found = false when no Basic Auth header is present")
	}
	if creds != nil {
		t.Error("Expected creds = nil when no Basic Auth header is present")
	}
}

func TestNewAWSAuthProvider(t *testing.T) {
	cfg := &config.AWSAuthConfig{
		Enabled:        true,
		Region:         "us-west-2",
		AccountID:      "123456789012",
		TeamTagKey:     "Team",
		RequiredTagKey: "agentapi-proxy",
		RequiredTagVal: "enabled",
		CacheTTL:       "30m",
		UserMapping: config.AWSUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"platform": {
					Role:        "admin",
					Permissions: []string{"*"},
				},
			},
		},
	}

	// Note: NewAWSAuthProvider may fail in test environment without AWS credentials
	// This test just verifies the configuration is properly passed
	provider, err := NewAWSAuthProvider(cfg)

	// In CI/test environments without AWS credentials, this may fail
	// We skip the test in that case
	if err != nil {
		t.Skipf("Skipping test: AWS credentials not available: %v", err)
	}

	if provider == nil {
		t.Fatal("NewAWSAuthProvider returned nil")
	}
	if provider.config != cfg {
		t.Error("provider.config does not match input config")
	}
	if provider.userCache == nil {
		t.Error("provider.userCache is nil")
	}
}

func TestAWSAuthProvider_extractTeamsFromTags(t *testing.T) {
	cfg := &config.AWSAuthConfig{
		TeamTagKey: "Team",
	}
	// Create provider directly for unit testing (bypassing AWS connection)
	provider := &AWSAuthProvider{
		config: cfg,
	}

	tests := []struct {
		name     string
		tags     map[string]string
		expected []string
	}{
		{
			name:     "single team",
			tags:     map[string]string{"Team": "platform"},
			expected: []string{"platform"},
		},
		{
			name:     "multiple teams comma separated",
			tags:     map[string]string{"Team": "platform,backend,frontend"},
			expected: []string{"platform", "backend", "frontend"},
		},
		{
			name:     "teams with spaces",
			tags:     map[string]string{"Team": "platform , backend , frontend"},
			expected: []string{"platform", "backend", "frontend"},
		},
		{
			name:     "no team tag",
			tags:     map[string]string{"Department": "engineering"},
			expected: nil,
		},
		{
			name:     "empty team tag",
			tags:     map[string]string{"Team": ""},
			expected: nil,
		},
		{
			name:     "empty tags",
			tags:     map[string]string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.extractTeamsFromTags(tt.tags)

			if len(result) != len(tt.expected) {
				t.Errorf("extractTeamsFromTags() = %v, want %v", result, tt.expected)
				return
			}

			for i, team := range result {
				if team != tt.expected[i] {
					t.Errorf("extractTeamsFromTags()[%d] = %q, want %q", i, team, tt.expected[i])
				}
			}
		})
	}
}

func TestAWSAuthProvider_mapUserPermissions(t *testing.T) {
	cfg := &config.AWSAuthConfig{
		UserMapping: config.AWSUserMapping{
			DefaultRole:        "guest",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"platform": {
					Role:        "admin",
					Permissions: []string{"*"},
					EnvFile:     "/etc/env/platform.env",
				},
				"backend": {
					Role:        "developer",
					Permissions: []string{"read", "write", "execute"},
				},
				"frontend": {
					Role:        "member",
					Permissions: []string{"read", "write"},
				},
			},
		},
	}
	// Create provider directly for unit testing (bypassing AWS connection)
	provider := &AWSAuthProvider{
		config: cfg,
	}

	tests := []struct {
		name            string
		teams           []string
		expectedRole    string
		expectedEnvFile string
		minPermissions  []string
	}{
		{
			name:            "no teams - use defaults",
			teams:           []string{},
			expectedRole:    "guest",
			expectedEnvFile: "",
			minPermissions:  []string{"read"},
		},
		{
			name:            "platform team - admin",
			teams:           []string{"platform"},
			expectedRole:    "admin",
			expectedEnvFile: "/etc/env/platform.env",
			minPermissions:  []string{"*"},
		},
		{
			name:            "backend team - developer",
			teams:           []string{"backend"},
			expectedRole:    "developer",
			expectedEnvFile: "",
			minPermissions:  []string{"read", "write", "execute"},
		},
		{
			name:            "multiple teams - highest role wins",
			teams:           []string{"frontend", "backend"},
			expectedRole:    "developer",
			expectedEnvFile: "",
			minPermissions:  []string{"read", "write"},
		},
		{
			name:            "unknown team - use defaults",
			teams:           []string{"unknown"},
			expectedRole:    "guest",
			expectedEnvFile: "",
			minPermissions:  []string{"read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, permissions, envFile := provider.mapUserPermissions(tt.teams)

			if role != tt.expectedRole {
				t.Errorf("role = %q, want %q", role, tt.expectedRole)
			}

			if envFile != tt.expectedEnvFile {
				t.Errorf("envFile = %q, want %q", envFile, tt.expectedEnvFile)
			}

			// Check that all expected permissions are present
			permMap := make(map[string]bool)
			for _, p := range permissions {
				permMap[p] = true
			}
			for _, expected := range tt.minPermissions {
				if !permMap[expected] {
					t.Errorf("missing expected permission %q, got %v", expected, permissions)
				}
			}
		})
	}
}

func TestIsHigherRole(t *testing.T) {
	tests := []struct {
		role1    string
		role2    string
		expected bool
	}{
		{"admin", "developer", true},
		{"developer", "member", true},
		{"member", "user", true},
		{"user", "guest", true},
		{"admin", "admin", false},
		{"guest", "admin", false},
		{"developer", "admin", false},
		{"unknown", "admin", false},
		{"admin", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.role1+"_vs_"+tt.role2, func(t *testing.T) {
			result := isHigherRole(tt.role1, tt.role2)
			if result != tt.expected {
				t.Errorf("isHigherRole(%q, %q) = %v, want %v", tt.role1, tt.role2, result, tt.expected)
			}
		})
	}
}

func TestParseARN(t *testing.T) {
	tests := []struct {
		name         string
		arn          string
		expectedType entities.AWSEntityType
		expectedName string
		expectError  bool
	}{
		{
			name:         "IAM user",
			arn:          "arn:aws:iam::123456789012:user/johndoe",
			expectedType: entities.AWSEntityTypeUser,
			expectedName: "johndoe",
			expectError:  false,
		},
		{
			name:         "IAM role",
			arn:          "arn:aws:iam::123456789012:role/MyRole",
			expectedType: entities.AWSEntityTypeRole,
			expectedName: "MyRole",
			expectError:  false,
		},
		{
			name:         "assumed role",
			arn:          "arn:aws:sts::123456789012:assumed-role/MyRole/session-name",
			expectedType: entities.AWSEntityTypeRole,
			expectedName: "MyRole",
			expectError:  false,
		},
		{
			name:        "invalid ARN - too few parts",
			arn:         "arn:aws:iam",
			expectError: true,
		},
		{
			name:        "unsupported resource type",
			arn:         "arn:aws:iam::123456789012:group/MyGroup",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityType, entityName, err := entities.ParseARN(tt.arn)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseARN(%q) expected error, got nil", tt.arn)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseARN(%q) unexpected error: %v", tt.arn, err)
				return
			}

			if entityType != tt.expectedType {
				t.Errorf("entityType = %q, want %q", entityType, tt.expectedType)
			}
			if entityName != tt.expectedName {
				t.Errorf("entityName = %q, want %q", entityName, tt.expectedName)
			}
		})
	}
}

func TestExtractAccountID(t *testing.T) {
	tests := []struct {
		name        string
		arn         string
		expected    string
		expectError bool
	}{
		{
			name:     "valid ARN",
			arn:      "arn:aws:iam::123456789012:user/johndoe",
			expected: "123456789012",
		},
		{
			name:        "invalid ARN",
			arn:         "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := entities.ExtractAccountID(tt.arn)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("ExtractAccountID(%q) = %q, want %q", tt.arn, result, tt.expected)
			}
		})
	}
}

func TestAWSUserInfo(t *testing.T) {
	tags := map[string]string{
		"Team":       "platform",
		"Department": "engineering",
	}
	teams := []string{"platform", "backend"}

	info := entities.NewAWSUserInfo(
		"arn:aws:iam::123456789012:user/johndoe",
		"AIDAIOSFODNN7EXAMPLE",
		"123456789012",
		entities.AWSEntityTypeUser,
		"johndoe",
		tags,
		teams,
	)

	if info.ARN() != "arn:aws:iam::123456789012:user/johndoe" {
		t.Errorf("ARN() = %q, want %q", info.ARN(), "arn:aws:iam::123456789012:user/johndoe")
	}
	if info.UserID() != "AIDAIOSFODNN7EXAMPLE" {
		t.Errorf("UserID() = %q, want %q", info.UserID(), "AIDAIOSFODNN7EXAMPLE")
	}
	if info.AccountID() != "123456789012" {
		t.Errorf("AccountID() = %q, want %q", info.AccountID(), "123456789012")
	}
	if info.EntityType() != entities.AWSEntityTypeUser {
		t.Errorf("EntityType() = %q, want %q", info.EntityType(), entities.AWSEntityTypeUser)
	}
	if info.EntityName() != "johndoe" {
		t.Errorf("EntityName() = %q, want %q", info.EntityName(), "johndoe")
	}
	if !info.IsUser() {
		t.Error("IsUser() = false, want true")
	}
	if info.IsRole() {
		t.Error("IsRole() = true, want false")
	}

	// Check tags
	gotTags := info.Tags()
	if gotTags["Team"] != "platform" {
		t.Errorf("Tags()[Team] = %q, want %q", gotTags["Team"], "platform")
	}

	// Check GetTag
	teamValue, ok := info.GetTag("Team")
	if !ok || teamValue != "platform" {
		t.Errorf("GetTag(Team) = %q, %v, want %q, true", teamValue, ok, "platform")
	}

	// Check teams
	gotTeams := info.Teams()
	if len(gotTeams) != 2 {
		t.Errorf("len(Teams()) = %d, want 2", len(gotTeams))
	}
}
