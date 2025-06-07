package main

import (
	"github.com/spf13/viper"
)

// Config represents the proxy configuration
type Config struct {
	// DefaultBackend is used when no route matches
	DefaultBackend string `mapstructure:"default_backend"`
	
	// Routes maps path patterns to backend URLs
	// Pattern can include variables like {org} and {repo}
	Routes map[string]string `mapstructure:"routes"`
}

// LoadConfig loads configuration using Viper
func LoadConfig(filename string) (*Config, error) {
	viper.SetConfigFile(filename)
	viper.SetConfigType("json")
	
	// Set default values
	viper.SetDefault("default_backend", "http://localhost:3000")
	viper.SetDefault("routes", map[string]string{
		"/api/{org}/{repo}": "http://localhost:3000",
		"/health":           "http://localhost:3000",
	})
	
	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		// If file doesn't exist, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
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