package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ProtectedResourceMetadata is the RFC 9728 oauth-protected-resource metadata.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// AuthorizationServerMetadata is the RFC 8414 oauth-authorization-server metadata.
type AuthorizationServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
}

// SupportsDCR returns true when the metadata includes a registration endpoint.
func (m *AuthorizationServerMetadata) SupportsDCR() bool {
	return m.RegistrationEndpoint != ""
}

// Discover resolves the OAuth authorization server metadata for a given MCP server URL.
//
// Discovery order:
//  1. GET {mcpURL} → expect 401 with WWW-Authenticate header containing resource_metadata URL
//  2. GET {resourceMetadataURL} → ProtectedResourceMetadata → auth server base URL
//  3. GET {authServerBase}/.well-known/oauth-authorization-server → AuthorizationServerMetadata
//
// Falls back to well-known paths on the MCP server origin when step 1 does not
// provide a resource_metadata URL.
func Discover(ctx context.Context, client *http.Client, mcpServerURL string) (*AuthorizationServerMetadata, error) {
	if client == nil {
		client = http.DefaultClient
	}

	// Step 1: probe the MCP server to find the resource_metadata URL.
	resourceMetadataURL, err := probeResourceMetadataURL(ctx, client, mcpServerURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: probe failed: %w", err)
	}

	// Step 2: fetch ProtectedResourceMetadata.
	prm, err := fetchProtectedResourceMetadata(ctx, client, resourceMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: fetch protected resource metadata: %w", err)
	}
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("mcpoauth: protected resource metadata has no authorization_servers")
	}
	authServerBase := strings.TrimSuffix(prm.AuthorizationServers[0], "/")

	// Step 3: fetch AuthorizationServerMetadata.
	asmd, err := fetchAuthorizationServerMetadata(ctx, client, authServerBase)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: fetch authorization server metadata: %w", err)
	}
	return asmd, nil
}

// probeResourceMetadataURL sends a GET to mcpURL and extracts the resource_metadata
// URL from the WWW-Authenticate header of the expected 401 response.
// When the header is absent or the URL cannot be parsed, it falls back to
// {origin}/.well-known/oauth-protected-resource.
func probeResourceMetadataURL(ctx context.Context, client *http.Client, mcpURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcpURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	// Try to extract resource_metadata from WWW-Authenticate.
	if wwwAuth := resp.Header.Get("WWW-Authenticate"); wwwAuth != "" {
		if u := extractParam(wwwAuth, "resource_metadata"); u != "" {
			return u, nil
		}
	}

	// Fall back to well-known path on the MCP server origin.
	origin, err := originOf(mcpURL)
	if err != nil {
		return "", fmt.Errorf("cannot determine origin of %q: %w", mcpURL, err)
	}
	return origin + "/.well-known/oauth-protected-resource", nil
}

// fetchProtectedResourceMetadata GETs the given URL and decodes the JSON response.
func fetchProtectedResourceMetadata(ctx context.Context, client *http.Client, metadataURL string) (*ProtectedResourceMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", metadataURL, resp.StatusCode)
	}

	var prm ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&prm); err != nil {
		return nil, fmt.Errorf("decode protected resource metadata: %w", err)
	}
	return &prm, nil
}

// fetchAuthorizationServerMetadata GETs {base}/.well-known/oauth-authorization-server.
func fetchAuthorizationServerMetadata(ctx context.Context, client *http.Client, authServerBase string) (*AuthorizationServerMetadata, error) {
	metaURL := authServerBase + "/.well-known/oauth-authorization-server"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", metaURL, resp.StatusCode)
	}

	var md AuthorizationServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&md); err != nil {
		return nil, fmt.Errorf("decode authorization server metadata: %w", err)
	}
	return &md, nil
}

// extractParam parses a simple Bearer challenge and returns the value of the
// named parameter (e.g. resource_metadata="https://...").
func extractParam(header, name string) string {
	// e.g.: Bearer realm="MCP", resource_metadata="https://..."
	target := name + "="
	idx := strings.Index(header, target)
	if idx < 0 {
		return ""
	}
	rest := header[idx+len(target):]
	if strings.HasPrefix(rest, `"`) {
		rest = rest[1:]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	// unquoted value
	end := strings.IndexAny(rest, " ,\t")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// originOf returns the scheme+host portion of rawURL.
func originOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}
