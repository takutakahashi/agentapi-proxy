package client

import (
	"fmt"
	"os"
)

// Config holds the client configuration
type Config struct {
	Endpoint  string
	SessionID string
	APIKey    string
}

// ConfigFromEnv creates a client configuration from environment variables
func ConfigFromEnv() (*Config, error) {
	sessionID := os.Getenv("AGENTAPI_SESSION_ID")
	if sessionID == "" {
		return nil, fmt.Errorf("AGENTAPI_SESSION_ID environment variable is not set")
	}

	apiKey := os.Getenv("AGENTAPI_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AGENTAPI_KEY environment variable is not set")
	}

	endpoint, err := buildEndpointFromEnv()
	if err != nil {
		return nil, err
	}

	return &Config{
		Endpoint:  endpoint,
		SessionID: sessionID,
		APIKey:    apiKey,
	}, nil
}

// EndpointFromEnv builds the endpoint URL from environment variables.
// It reads AGENTAPI_PROXY_SERVICE_HOST and AGENTAPI_PROXY_SERVICE_PORT_HTTP.
func EndpointFromEnv() (string, error) {
	return buildEndpointFromEnv()
}

// buildEndpointFromEnv builds the endpoint URL from environment variables
func buildEndpointFromEnv() (string, error) {
	proxyHost := os.Getenv("AGENTAPI_PROXY_SERVICE_HOST")
	proxyPort := os.Getenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP")

	if proxyHost == "" || proxyPort == "" {
		return "", fmt.Errorf("AGENTAPI_PROXY_SERVICE_HOST or AGENTAPI_PROXY_SERVICE_PORT_HTTP environment variable is not set")
	}

	return fmt.Sprintf("http://%s:%s", proxyHost, proxyPort), nil
}

// NewClientFromEnv creates a new client from environment variables
func NewClientFromEnv() (*Client, *Config, error) {
	config, err := ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}

	client := NewClient(config.Endpoint, WithAPIKeyAuth(config.APIKey))
	return client, config, nil
}
