package config

import (
	"encoding/json"
	"log"
	"os"
)

// Config represents the proxy configuration
type Config struct {
	// DefaultBackend is used when no route matches
	DefaultBackend string `json:"DefaultBackend" mapstructure:"default_backend"`

	// Routes maps path patterns to backend URLs
	// Pattern can include variables like {org} and {repo}
	Routes map[string]string `json:"Routes" mapstructure:"routes"`
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close config file: %v", err)
		}
	}()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
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
