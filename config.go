package main

import (
	"encoding/json"
	"os"
)

// Config represents the proxy configuration
type Config struct {
	// DefaultBackend is used when no route matches
	DefaultBackend string `json:"default_backend,omitempty"`
	
	// Routes maps path patterns to backend URLs
	// Pattern can include variables like {org} and {repo}
	Routes map[string]string `json:"routes"`
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultBackend: "http://localhost:3000",
		Routes: map[string]string{
			"/api/{org}/{repo}": "http://localhost:3000",
			"/health":           "http://localhost:3000",
		},
	}
}