package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	if config.DefaultBackend == "" {
		t.Error("DefaultBackend should not be empty")
	}

	if len(config.Routes) == 0 {
		t.Error("Routes should not be empty")
	}

	// Check if expected routes exist
	expectedRoutes := []string{"/api/{org}/{repo}", "/health"}
	for _, route := range expectedRoutes {
		if _, exists := config.Routes[route]; !exists {
			t.Errorf("Expected route %s not found in default config", route)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tempConfig := &Config{
		DefaultBackend: "http://example.com:8080",
		Routes: map[string]string{
			"/api/{org}/{repo}": "http://backend1.com",
			"/v1/{service}":     "http://backend2.com",
		},
	}

	configData, err := json.Marshal(tempConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write(configData); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Compare loaded config with original
	if !reflect.DeepEqual(tempConfig, loadedConfig) {
		t.Errorf("Loaded config doesn't match original.\nExpected: %+v\nGot: %+v", tempConfig, loadedConfig)
	}
}

func TestLoadConfigNonexistentFile(t *testing.T) {
	_, err := LoadConfig("nonexistent-file.json")
	if err == nil {
		t.Error("LoadConfig should return error for nonexistent file")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	// Create a temporary file with invalid JSON
	tmpfile, err := os.CreateTemp("", "invalid*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	invalidJSON := `{"invalid": json}`
	if _, err := tmpfile.WriteString(invalidJSON); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}
	tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("LoadConfig should return error for invalid JSON")
	}
}