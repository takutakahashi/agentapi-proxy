package config

import (
	"encoding/json"
	"log"
	"os"
)

// Config represents the proxy configuration
type Config struct {
	// StartPort is the starting port for agentapi servers
	StartPort int `json:"start_port" mapstructure:"start_port"`
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

	// Set default values if not specified
	if config.StartPort == 0 {
		config.StartPort = 9000
	}

	return &config, nil
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		StartPort: 9000,
	}
}
