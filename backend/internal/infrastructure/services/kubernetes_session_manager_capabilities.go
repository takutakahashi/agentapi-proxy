package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (m *KubernetesSessionManager) TouchSession(ctx context.Context, id string, at time.Time) error {
	session := m.GetSession(id)
	if session == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	if ks, ok := session.(*KubernetesSession); ok {
		ks.TouchUpdatedAt()
		ks.SetLastMessageAt(at)
	}
	if err := m.UpdateServiceAnnotation(ctx, id, "agentapi.proxy/updated-at", at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return m.UpdateServiceAnnotation(ctx, id, "agentapi.proxy/last-message-at", at.UTC().Format(time.RFC3339))
}

func (m *KubernetesSessionManager) GetSessionSandboxDomains(ctx context.Context, id string) (*portrepos.SandboxDomains, error) {
	session := m.GetSession(id)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	ks, ok := session.(*KubernetesSession)
	if !ok {
		return nil, fmt.Errorf("sandbox domains unavailable for session type")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%d/sandbox-domains", ks.ServiceDNS(), ProvisionerPort), nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("query session network filter: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session network filter returned %s", response.Status)
	}
	var domains portrepos.SandboxDomains
	if err := json.NewDecoder(response.Body).Decode(&domains); err != nil {
		return nil, fmt.Errorf("decode session network filter response: %w", err)
	}
	if domains.Allowed == nil {
		domains.Allowed = []string{}
	}
	if domains.Denied == nil {
		domains.Denied = []string{}
	}
	return &domains, nil
}
