package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// RunnerWorker exposes native process capacity through the same runner_claim_v1
// protocol used by pre-warmed Kubernetes Pods. Each goroutine represents one
// idle slot, so the parent store's compare-and-swap claim remains the sole
// allocator when multiple managers supply the same pool.
type RunnerWorker struct {
	manager                    repositories.SessionManager
	upstream, managerID, token string
	client                     *http.Client
	mu                         sync.Mutex
	idle                       map[string]int
}

type directSessionManager interface {
	CreateSessionDirect(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error)
}

type runnerPool struct {
	Pool       string `json:"pool"`
	MinIdle    int    `json:"min_idle"`
	MaxRunners int    `json:"max_runners"`
	Enabled    bool   `json:"enabled"`
	Draining   bool   `json:"draining"`
}
type nativeClaim struct {
	Allocation struct {
		SessionID  string `json:"session_id"`
		ManagerID  string `json:"manager_id"`
		Generation int64  `json:"generation"`
	} `json:"allocation"`
	LeaseID      string                           `json:"lease_id"`
	RuntimeToken string                           `json:"runtime_token"`
	Settings     *sessionsettings.SessionSettings `json:"settings"`
}

func NewRunnerWorker(manager repositories.SessionManager, upstream, managerID, token string) *RunnerWorker {
	return &RunnerWorker{manager: manager, upstream: strings.TrimRight(upstream, "/"), managerID: managerID, token: token, client: &http.Client{Timeout: 35 * time.Second}, idle: map[string]int{}}
}

func (w *RunnerWorker) Start(ctx context.Context) {
	if w.managerID == "" {
		log.Printf("[SESSION_RUNNER] manager ID is required")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		w.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *RunnerWorker) reconcile(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.upstream+"/internal/session-managers/"+url.PathEscape(w.managerID)+"/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+w.token)
	resp, err := w.client.Do(req)
	if err != nil {
		log.Printf("[SESSION_RUNNER] heartbeat failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		log.Printf("[SESSION_RUNNER] heartbeat returned HTTP %d", resp.StatusCode)
		return
	}
	var result struct {
		Pools []runnerPool `json:"pools"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return
	}
	active := len(w.manager.ListSessions(entities.SessionFilter{}))
	for _, p := range result.Pools {
		if !p.Enabled || p.Draining {
			continue
		}
		desired := p.MinIdle
		if desired < 1 {
			desired = 1
		}
		if p.MaxRunners > 0 && desired > p.MaxRunners-active {
			desired = p.MaxRunners - active
		}
		if desired < 0 {
			desired = 0
		}
		w.mu.Lock()
		missing := desired - w.idle[p.Pool]
		w.idle[p.Pool] += max(0, missing)
		w.mu.Unlock()
		for range max(0, missing) {
			go w.runSlot(ctx, p.Pool)
		}
	}
}

func (w *RunnerWorker) runSlot(ctx context.Context, pool string) {
	defer func() { w.mu.Lock(); w.idle[pool]--; w.mu.Unlock() }()
	runnerID := uuid.NewString()
	runnerToken, err := w.register(ctx, runnerID, pool)
	if err != nil {
		log.Printf("[SESSION_RUNNER] register %s: %v", runnerID, err)
		return
	}
	for ctx.Err() == nil {
		claim, ok, err := w.claim(ctx, runnerID, runnerToken, pool)
		if err != nil {
			log.Printf("[SESSION_RUNNER] claim pool %s: %v", pool, err)
			sleepOrDone(ctx, 3*time.Second)
			continue
		}
		if !ok {
			continue
		}
		if claim.Settings == nil || claim.RuntimeToken == "" || claim.Allocation.SessionID == "" {
			_ = w.complete(ctx, runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail")
			continue
		}
		claim.Settings.ParentRuntime = &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: w.upstream, SessionID: claim.Allocation.SessionID, ManagerID: claim.Allocation.ManagerID, Token: claim.RuntimeToken, Generation: claim.Allocation.Generation}
		req := runRequestFromSettings(claim.Settings)
		creator, ok := w.manager.(directSessionManager)
		if !ok {
			_ = w.complete(ctx, runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail")
			return
		}
		if _, err = creator.CreateSessionDirect(ctx, claim.Allocation.SessionID, req, nil); err != nil {
			log.Printf("[SESSION_RUNNER] create %s: %v", claim.Allocation.SessionID, err)
			_ = w.complete(ctx, runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail")
			continue
		}
		if err = w.complete(ctx, runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "ack"); err != nil {
			log.Printf("[SESSION_RUNNER] ack %s: %v", claim.Allocation.SessionID, err)
		}
		return
	}
}

func (w *RunnerWorker) register(ctx context.Context, id, pool string) (string, error) {
	var out struct {
		RunnerToken string `json:"runner_token"`
	}
	err := w.managerRequest(ctx, http.MethodPost, "/internal/session-runners/register", map[string]string{"runner_id": id, "pool": pool}, &out)
	return out.RunnerToken, err
}
func (w *RunnerWorker) claim(ctx context.Context, id, token, pool string) (*nativeClaim, bool, error) {
	u := w.upstream + "/internal/session-runners/allocations/next?wait=30s&pool=" + url.QueryEscape(pool)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Session-Runner-ID", id)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out nativeClaim
	err = json.NewDecoder(resp.Body).Decode(&out)
	return &out, true, err
}
func (w *RunnerWorker) complete(ctx context.Context, id, token, sessionID, leaseID, action string) error {
	raw, _ := json.Marshal(map[string]string{"lease_id": leaseID})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.upstream+"/internal/session-runners/allocations/"+url.PathEscape(sessionID)+"/"+action, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Session-Runner-ID", id)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func (w *RunnerWorker) managerRequest(ctx context.Context, method, path string, in, out any) error {
	raw, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, method, w.upstream+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("X-Session-Manager-ID", w.managerID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func runRequestFromSettings(settings *sessionsettings.SessionSettings) *entities.RunServerRequest {
	req := &entities.RunServerRequest{UserID: settings.Session.UserID, Scope: entities.ResourceScope(settings.Session.Scope), TeamID: settings.Session.TeamID, AgentType: settings.Session.AgentType, Oneshot: settings.Session.Oneshot, Teams: settings.Session.Teams, InitialMessage: settings.InitialMessage, ProvisionSettings: settings}
	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{FullName: settings.Repository.FullName, CloneDir: settings.Repository.CloneDir, Branch: settings.Repository.Branch, PR: settings.Repository.PR}
	}
	return req
}

func sleepOrDone(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
