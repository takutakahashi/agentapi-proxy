package sessionsettings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultOllamaCloudBaseURL = "https://ollama.com"
	ollamaCloudRefreshWorkers = 8
)

type ollamaCloudModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaCloudModelCache struct {
	Timestamp int64                      `json:"timestamp"`
	Models    map[string]json.RawMessage `json:"models"`
}

func shouldRefreshPiOllamaCloud(settings *SessionSettings) bool {
	if settings == nil {
		return false
	}
	if settings.Session.AgentType == "pi-ollama" {
		return true
	}
	for key, value := range settings.Env {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(key)), "PI_") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func refreshPiOllamaCloudCache(env map[string]string, outputDir string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(env["OLLAMA_API_BASE"]), "/")
	if baseURL == "" {
		baseURL = defaultOllamaCloudBaseURL
	}
	client := &http.Client{Timeout: 15 * time.Second}
	apiKey := env["OLLAMA_API_KEY"]

	var list ollamaCloudModelList
	if err := ollamaCloudRequest(client, http.MethodGet, baseURL+"/v1/models", apiKey, nil, &list); err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	models := make(map[string]json.RawMessage)
	var mu sync.Mutex
	jobs := make(chan string)
	workers := min(ollamaCloudRefreshWorkers, len(list.Data))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				body, err := json.Marshal(map[string]string{"model": id})
				if err != nil {
					continue
				}
				var detail json.RawMessage
				if err := ollamaCloudRequest(client, http.MethodPost, baseURL+"/api/show", apiKey, body, &detail); err != nil {
					continue
				}
				mu.Lock()
				models[id] = detail
				mu.Unlock()
			}
		}()
	}
	for _, model := range list.Data {
		if model.ID != "" {
			jobs <- model.ID
		}
	}
	close(jobs)
	wg.Wait()

	if len(models) == 0 {
		return fmt.Errorf("no model details were retrieved")
	}
	cache, err := json.MarshalIndent(ollamaCloudModelCache{Timestamp: time.Now().UnixMilli(), Models: models}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	cacheDir := filepath.Join(outputDir, ".pi", "agent", "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(cacheDir, ".ollama-cloud-models-*")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set cache permissions: %w", err)
	}
	if _, err := tmp.Write(cache); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cache: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(cacheDir, "ollama-cloud-models.json")); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}

func ollamaCloudRequest(client *http.Client, method, url, apiKey string, body []byte, target any) error {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
