package app

import (
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildApplicationKVStoreLegacyKubernetes(t *testing.T) {
	store, wrap, err := buildApplicationKVStore(config.KVStoreConfig{Backend: "kubernetes"}, fake.NewSimpleClientset())
	if err != nil || store != nil || wrap {
		t.Fatalf("store=%v wrap=%v err=%v", store, wrap, err)
	}
}

func TestBuildApplicationKVStoreReplicated(t *testing.T) {
	cfg := config.KVStoreConfig{
		Primary:   &config.KVStoreBackendConfig{Backend: "kubernetes"},
		Secondary: &config.KVStoreBackendConfig{Backend: "libsql", DatabaseURL: "file://" + filepath.Join(t.TempDir(), "secondary.db")},
	}
	store, wrap, err := buildApplicationKVStore(cfg, fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || !wrap {
		t.Fatalf("store=%v wrap=%v", store, wrap)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildApplicationKVStoreRejectsMixedConfig(t *testing.T) {
	cfg := config.KVStoreConfig{
		Backend: "libsql",
		Primary: &config.KVStoreBackendConfig{Backend: "kubernetes"},
	}
	if _, _, err := buildApplicationKVStore(cfg, fake.NewSimpleClientset()); err == nil {
		t.Fatal("mixed legacy and nested config succeeded")
	}
}
