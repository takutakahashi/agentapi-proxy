package app

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestValidateAPIKVStoreSupportsCustomerBackends(t *testing.T) {
	for _, backend := range []string{"kubernetes", "libsql"} {
		t.Run(backend, func(t *testing.T) {
			if err := validateAPIKVStore(config.KVStoreConfig{Backend: backend}); err != nil {
				t.Fatalf("validateAPIKVStore(%q): %v", backend, err)
			}
		})
	}
}

func TestValidateAPIKVStoreRejectsUnknownBackend(t *testing.T) {
	if err := validateAPIKVStore(config.KVStoreConfig{Backend: "unknown"}); err == nil {
		t.Fatal("validateAPIKVStore accepted an unknown backend")
	}
}
