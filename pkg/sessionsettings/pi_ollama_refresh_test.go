package sessionsettings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestShouldRefreshPiOllamaCloud(t *testing.T) {
	tests := []struct {
		name     string
		settings *SessionSettings
		want     bool
	}{
		{name: "nil", settings: nil, want: false},
		{name: "pi ollama agent", settings: &SessionSettings{Session: SessionMeta{AgentType: "pi-ollama"}}, want: true},
		{name: "default provider", settings: &SessionSettings{Env: map[string]string{"PI_DEFAULT_PROVIDER": "ollama-cloud"}}, want: true},
		{name: "custom provider alias", settings: &SessionSettings{Env: map[string]string{"PI_CUSTOM_PROVIDER": "OLLAMA-CLOUD"}}, want: true},
		{name: "other provider", settings: &SessionSettings{Env: map[string]string{"PI_DEFAULT_PROVIDER": "openai"}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRefreshPiOllamaCloud(tt.settings); got != tt.want {
				t.Fatalf("shouldRefreshPiOllamaCloud() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefreshPiOllamaCloudCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/models":
			if err := json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "glm-5.2:cloud"}}}); err != nil {
				t.Errorf("encode model list: %v", err)
			}
		case "/api/show":
			if err := json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"tools"}, "model_info": map[string]any{"family": "glm"}}); err != nil {
				t.Errorf("encode model details: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	outputDir := t.TempDir()
	err := refreshPiOllamaCloudCache(map[string]string{"OLLAMA_API_BASE": server.URL, "OLLAMA_API_KEY": "test-key"}, outputDir)
	if err != nil {
		t.Fatalf("refreshPiOllamaCloudCache() error = %v", err)
	}
	cachePath := filepath.Join(outputDir, ".pi", "agent", "cache", "ollama-cloud-models.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache ollamaCloudModelCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Models["glm-5.2:cloud"]; !ok {
		t.Fatalf("cache does not contain glm-5.2:cloud: %s", data)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRefreshPiOllamaCloudCachePreservesExistingCacheOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	outputDir := t.TempDir()
	cachePath := filepath.Join(outputDir, ".pi", "agent", "cache", "ollama-cloud-models.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := refreshPiOllamaCloudCache(map[string]string{"OLLAMA_API_BASE": server.URL}, outputDir); err == nil {
		t.Fatal("refreshPiOllamaCloudCache() error = nil, want error")
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Fatalf("cache was replaced: %q", data)
	}
}
