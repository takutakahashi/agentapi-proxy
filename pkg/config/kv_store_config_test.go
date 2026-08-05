package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigWithReplicatedKVStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`kv_store:
  primary:
    backend: kubernetes
  secondary:
    backend: libsql
    database_url: file:///tmp/secondary.db
    auth_token: token
  replication:
    mode: rollback
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KVStore.Primary == nil || cfg.KVStore.Primary.Backend != "kubernetes" {
		t.Fatalf("primary = %#v", cfg.KVStore.Primary)
	}
	if cfg.KVStore.Secondary == nil || cfg.KVStore.Secondary.Backend != "libsql" || cfg.KVStore.Secondary.AuthToken != "token" {
		t.Fatalf("secondary = %#v", cfg.KVStore.Secondary)
	}
	if cfg.KVStore.Replication.Mode != "rollback" {
		t.Fatalf("replication mode = %q", cfg.KVStore.Replication.Mode)
	}
}

func TestLoadConfigWithReplicatedKVStoreEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_KV_STORE_PRIMARY_BACKEND", "kubernetes")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_BACKEND", "libsql")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL", "file:///tmp/secondary.db")
	t.Setenv("AGENTAPI_KV_STORE_REPLICATION_MODE", "best_effort")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KVStore.Primary == nil || cfg.KVStore.Primary.Backend != "kubernetes" {
		t.Fatalf("primary = %#v", cfg.KVStore.Primary)
	}
	if cfg.KVStore.Secondary == nil || cfg.KVStore.Secondary.DatabaseURL != "file:///tmp/secondary.db" {
		t.Fatalf("secondary = %#v", cfg.KVStore.Secondary)
	}
	if cfg.KVStore.Replication.Mode != "best_effort" {
		t.Fatalf("replication mode = %q", cfg.KVStore.Replication.Mode)
	}
}
