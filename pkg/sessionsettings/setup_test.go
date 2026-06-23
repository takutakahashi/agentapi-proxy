package sessionsettings

import "testing"

func TestRepositoryCheckoutTarget(t *testing.T) {
	tests := []struct {
		name      string
		repo      *RepositoryConfig
		wantType  string
		wantValue string
	}{
		{
			name:      "none",
			repo:      &RepositoryConfig{},
			wantType:  "",
			wantValue: "",
		},
		{
			name: "branch",
			repo: &RepositoryConfig{
				Branch: " feature/test ",
			},
			wantType:  "branch",
			wantValue: "feature/test",
		},
		{
			name: "pr",
			repo: &RepositoryConfig{
				PR: " 123 ",
			},
			wantType:  "pr",
			wantValue: "123",
		},
		{
			name: "pr takes precedence over branch",
			repo: &RepositoryConfig{
				Branch: "feature/test",
				PR:     "123",
			},
			wantType:  "pr",
			wantValue: "123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotValue := repositoryCheckoutTarget(tt.repo)
			if gotType != tt.wantType || gotValue != tt.wantValue {
				t.Fatalf("repositoryCheckoutTarget() = (%q, %q), want (%q, %q)", gotType, gotValue, tt.wantType, tt.wantValue)
			}
		})
	}
}
