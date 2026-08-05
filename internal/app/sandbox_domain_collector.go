package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

// SandboxDomainCollector runs as a background goroutine, periodically querying
// each sandboxed session's network filter domain log and aggregating the results
// into per-policy Kubernetes ConfigMaps.
type SandboxDomainCollector struct {
	sessionManager *services.KubernetesSessionManager
	domainRepo     *repositories.KubernetesSandboxDomainRepository
	httpClient     *http.Client
	interval       time.Duration
}

func newSandboxDomainCollector(
	sessionManager *services.KubernetesSessionManager,
	domainRepo *repositories.KubernetesSandboxDomainRepository,
	interval time.Duration,
) *SandboxDomainCollector {
	return &SandboxDomainCollector{
		sessionManager: sessionManager,
		domainRepo:     domainRepo,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		interval:       interval,
	}
}

// start runs the collector loop until ctx is cancelled.
func (c *SandboxDomainCollector) start(ctx context.Context) {
	log.Printf("[SANDBOX_DOMAIN_COLLECTOR] Starting (interval: %s)", c.interval)
	c.collect(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[SANDBOX_DOMAIN_COLLECTOR] Stopped")
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

// collect queries all active sandboxed sessions and writes aggregated domain
// data to the per-policy ConfigMaps.
func (c *SandboxDomainCollector) collect(ctx context.Context) {
	allSessions := c.sessionManager.ListSessions(entities.SessionFilter{})

	type domainSets struct {
		allowed map[string]struct{}
		denied  map[string]struct{}
	}
	policyData := make(map[string]*domainSets)

	for _, session := range allSessions {
		ks, ok := session.(*services.KubernetesSession)
		if !ok {
			continue
		}
		req := ks.Request()
		if req == nil || req.Sandbox == nil || req.Sandbox.PolicyID == "" {
			continue
		}
		policyID := req.Sandbox.PolicyID

		domains, err := c.fetchDomains(ks)
		if err != nil {
			log.Printf("[SANDBOX_DOMAIN_COLLECTOR] Failed to fetch domains for session %s: %v", ks.ID(), err)
			continue
		}

		if _, ok := policyData[policyID]; !ok {
			policyData[policyID] = &domainSets{
				allowed: make(map[string]struct{}),
				denied:  make(map[string]struct{}),
			}
		}
		for _, d := range domains.Allowed {
			policyData[policyID].allowed[d] = struct{}{}
		}
		for _, d := range domains.Denied {
			policyData[policyID].denied[d] = struct{}{}
		}
	}

	for policyID, sets := range policyData {
		allowed := make([]string, 0, len(sets.allowed))
		denied := make([]string, 0, len(sets.denied))
		for d := range sets.allowed {
			allowed = append(allowed, d)
		}
		for d := range sets.denied {
			denied = append(denied, d)
		}

		// Preserve the existing ignored list so user dismissals survive re-collection.
		var ignored []string
		if existing, err := c.domainRepo.Get(ctx, policyID); err == nil && existing != nil {
			ignored = existing.Ignored
		}

		data := &repositories.SandboxDomainData{
			Allowed:   allowed,
			Denied:    denied,
			Ignored:   ignored,
			UpdatedAt: time.Now(),
		}
		if err := c.domainRepo.Upsert(ctx, policyID, data); err != nil {
			log.Printf("[SANDBOX_DOMAIN_COLLECTOR] Failed to upsert domain data for policy %s: %v", policyID, err)
		} else {
			log.Printf("[SANDBOX_DOMAIN_COLLECTOR] Updated policy %s: %d allowed, %d denied domains", policyID, len(allowed), len(denied))
		}
	}
}

type sandboxDomainsResponse struct {
	Allowed []string `json:"allowed"`
	Denied  []string `json:"denied"`
}

func (c *SandboxDomainCollector) fetchDomains(ks *services.KubernetesSession) (*sandboxDomainsResponse, error) {
	url := fmt.Sprintf("http://%s:%d/sandbox-domains", ks.ServiceDNS(), services.ProvisionerPort)
	resp, err := c.httpClient.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusServiceUnavailable {
		return &sandboxDomainsResponse{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result sandboxDomainsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
