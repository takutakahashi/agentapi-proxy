package config

import "testing"

func TestLoadUsageConfigFromEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_USAGE_ENABLED", "true")
	t.Setenv("AGENTAPI_USAGE_DATABASE_URL", "libsql://usage.example")
	t.Setenv("AGENTAPI_USAGE_AUTH_TOKEN", "usage-token")
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Usage.Enabled || cfg.Usage.DatabaseURL != "libsql://usage.example" || cfg.Usage.AuthToken != "usage-token" {
		t.Fatalf("usage config = %#v", cfg.Usage)
	}
}
