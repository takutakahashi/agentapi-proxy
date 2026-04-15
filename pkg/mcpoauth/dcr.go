package mcpoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// DCRRequest is the RFC 7591 client registration request body.
type DCRRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// DCRResponse is the RFC 7591 client registration response.
type DCRResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// Register performs Dynamic Client Registration against the given endpoint.
func Register(ctx context.Context, client *http.Client, registrationEndpoint string, req DCRRequest) (*DCRResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("dcr: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dcr: POST %s: %w", registrationEndpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dcr: POST %s returned %d", registrationEndpoint, resp.StatusCode)
	}

	var res DCRResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("dcr: decode response: %w", err)
	}
	if res.ClientID == "" {
		return nil, fmt.Errorf("dcr: response missing client_id")
	}
	return &res, nil
}
