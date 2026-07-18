package client

import "testing"

func TestEndpointFromEnvPrefersExplicitEndpoint(t *testing.T) {
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "https://proxy.example/base")
	t.Setenv("AGENTAPI_PROXY_SERVICE_HOST", "local-proxy")
	t.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "8080")

	endpoint, err := EndpointFromEnv()
	if err != nil {
		t.Fatalf("EndpointFromEnv() error = %v", err)
	}
	if endpoint != "https://proxy.example/base" {
		t.Fatalf("EndpointFromEnv() = %q, want explicit endpoint", endpoint)
	}
}

func TestEndpointFromEnvFallsBackToServiceEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "")
	t.Setenv("AGENTAPI_PROXY_SERVICE_HOST", "local-proxy")
	t.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "8080")

	endpoint, err := EndpointFromEnv()
	if err != nil {
		t.Fatalf("EndpointFromEnv() error = %v", err)
	}
	if endpoint != "http://local-proxy:8080" {
		t.Fatalf("EndpointFromEnv() = %q, want service endpoint", endpoint)
	}
}
