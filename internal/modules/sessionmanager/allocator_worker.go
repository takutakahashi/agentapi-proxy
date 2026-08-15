package sessionmanager

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	infrasessionallocation "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type AllocatorWorker struct {
	sessionManager         repositories.SessionManager
	client                 sessionallocation.ExternalAllocatorClient
	upstreamURL            string
	publicURL              string
	applyRuntimeProfile    bool
	appliedProfileRevision string
}

type directSessionManager interface {
	CreateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error)
}

type runtimeProfileApplier interface {
	ApplyRuntimeProfile(ctx context.Context, profile *sessionsettings.RuntimeProfile) error
}

func NewAllocatorWorker(sessionManager repositories.SessionManager, upstreamURL, token, publicURL string) *AllocatorWorker {
	return newAllocatorWorkerWithClient(sessionManager, infrasessionallocation.NewClient(upstreamURL, token), upstreamURL, publicURL)
}

func NewAllocatorWorkerWithUpstreamAuth(sessionManager repositories.SessionManager, upstreamURL, token, upstreamAuthToken, publicURL string) *AllocatorWorker {
	return NewAllocatorWorkerWithUpstreamAuthAndRuntimeProfile(sessionManager, upstreamURL, token, upstreamAuthToken, publicURL, false)
}

func NewAllocatorWorkerWithUpstreamAuthAndRuntimeProfile(sessionManager repositories.SessionManager, upstreamURL, token, upstreamAuthToken, publicURL string, applyRuntimeProfile bool) *AllocatorWorker {
	worker := newAllocatorWorkerWithClient(sessionManager, infrasessionallocation.NewNativeAllocatorClient(upstreamURL, token, upstreamAuthToken), upstreamURL, publicURL)
	worker.applyRuntimeProfile = applyRuntimeProfile
	return worker
}

func NewAllocatorWorkerWithClient(sessionManager repositories.SessionManager, client sessionallocation.ExternalAllocatorClient, publicURL string) *AllocatorWorker {
	return newAllocatorWorkerWithClient(sessionManager, client, "", publicURL)
}

func newAllocatorWorkerWithClient(sessionManager repositories.SessionManager, client sessionallocation.ExternalAllocatorClient, upstreamURL, publicURL string) *AllocatorWorker {
	return &AllocatorWorker{
		sessionManager:      sessionManager,
		client:              client,
		upstreamURL:         upstreamURL,
		publicURL:           publicURL,
		applyRuntimeProfile: true,
	}
}

func (w *AllocatorWorker) Start(ctx context.Context) {
	hadPollError := false
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		allocation, ok, err := w.client.NextExternal(ctx, 30*time.Second)
		if err != nil {
			log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to poll allocation: %v", err)
			hadPollError = true
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		w.syncRuntimeProfile(ctx, hadPollError)
		hadPollError = false
		if !ok {
			continue
		}
		w.process(ctx, allocation)
	}
}

func (w *AllocatorWorker) syncRuntimeProfile(ctx context.Context, force bool) {
	if !w.applyRuntimeProfile {
		return
	}
	applier, ok := w.sessionManager.(runtimeProfileApplier)
	if !ok {
		return
	}
	source, ok := w.client.(sessionallocation.RuntimeProfileClient)
	if !ok {
		return
	}
	revision := source.RuntimeProfileRevision()
	if revision == "" || (!force && revision == w.appliedProfileRevision) {
		return
	}
	snapshot, err := source.GetRuntimeProfile(ctx)
	if err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to synchronize parent runtime profile: %v", err)
		return
	}
	if snapshot == nil || snapshot.Profile == nil || snapshot.Revision == "" {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Parent returned an empty runtime profile snapshot")
		return
	}
	if err := applier.ApplyRuntimeProfile(ctx, snapshot.Profile); err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to apply synchronized parent runtime profile: %v", err)
		return
	}
	w.appliedProfileRevision = snapshot.Revision
	log.Printf("[SESSION_MANAGER_ALLOCATOR] Synchronized parent runtime profile revision %s", snapshot.Revision)
}

func (w *AllocatorWorker) process(ctx context.Context, allocation *sessionallocation.AllocationRequest) {
	if applier, ok := w.sessionManager.(runtimeProfileApplier); ok && w.applyRuntimeProfile && allocation.RuntimeProfile != nil {
		if err := applier.ApplyRuntimeProfile(ctx, allocation.RuntimeProfile); err != nil {
			log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to apply parent runtime profile: %v", err)
			_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
				Status:  sessionallocation.StatusError,
				Message: "failed to apply parent runtime profile: " + err.Error(),
			})
			return
		}
	}
	remoteProvisioner := false
	if manager, ok := w.sessionManager.(interface{ UsesRemoteProvisioner() bool }); ok {
		remoteProvisioner = manager.UsesRemoteProvisioner()
	}
	settings := allocation.ProvisionSettings
	if settings == nil && allocation.Request != nil {
		settings = allocation.Request.ProvisionSettings
	}
	if settings == nil && !remoteProvisioner {
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
			Status:  sessionallocation.StatusError,
			Message: "provision_settings is required",
		})
		return
	}
	if allocation.Runtime != nil {
		parentRuntime := &sessionsettings.ParentRuntimeConfig{
			Enabled:    true,
			Endpoint:   w.upstreamURL,
			SessionID:  allocation.SessionID,
			ManagerID:  allocation.ManagerID,
			Token:      allocation.Runtime.Token,
			Generation: allocation.Runtime.Generation,
		}
		if settings != nil {
			settings.ParentRuntime = parentRuntime
		}
		if allocation.Request != nil {
			allocation.Request.ParentRuntime = parentRuntime
		}
	}

	// The public session ID belongs to the upstream proxy. Keep the oneshot Stop
	// hook pointed at that proxy so it deletes both the allocated session and the
	// public route alias. Without this override, Kubernetes service discovery on
	// the session-manager cluster sends the public ID to the local proxy, where
	// only the allocated ID exists.
	if settings != nil && settings.Session.Oneshot && w.upstreamURL != "" {
		if settings.Env == nil {
			settings.Env = map[string]string{}
		}
		settings.Env["AGENTAPI_PROXY_ENDPOINT"] = w.upstreamURL
	}

	sessionID := uuid.New().String()
	var req *entities.RunServerRequest
	if remoteProvisioner {
		sessionID = allocation.SessionID
		req = allocation.Request
	} else {
		req = runRequestFromSettings(settings)
	}
	if req == nil {
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
			Status:  sessionallocation.StatusError,
			Message: "allocation metadata is required",
		})
		return
	}
	session, err := w.createLocalSession(ctx, sessionID, req)
	if err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to create session for allocation %s: %v", allocation.SessionID, err)
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
			Status:  sessionallocation.StatusError,
			Message: err.Error(),
		})
		return
	}

	if err := w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
		Status:             sessionallocation.StatusAssigned,
		AllocatedSessionID: session.ID(),
		ProxyURL:           w.publicURL,
	}); err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to complete allocation %s: %v", allocation.SessionID, err)
	}
}

func (w *AllocatorWorker) createLocalSession(ctx context.Context, sessionID string, req *entities.RunServerRequest) (entities.Session, error) {
	if manager, ok := w.sessionManager.(directSessionManager); ok {
		return manager.CreateSessionDirect(ctx, sessionID, req, nil)
	}
	return w.sessionManager.CreateSession(ctx, sessionID, req, nil)
}

func runRequestFromSettings(settings *sessionsettings.SessionSettings) *entities.RunServerRequest {
	req := &entities.RunServerRequest{
		UserID:            settings.Session.UserID,
		Scope:             entities.ResourceScope(settings.Session.Scope),
		TeamID:            settings.Session.TeamID,
		AgentType:         settings.Session.AgentType,
		Oneshot:           settings.Session.Oneshot,
		Teams:             settings.Session.Teams,
		InitialMessage:    settings.InitialMessage,
		ProvisionSettings: settings,
	}
	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{
			FullName: settings.Repository.FullName,
			CloneDir: settings.Repository.CloneDir,
			Branch:   settings.Repository.Branch,
			PR:       settings.Repository.PR,
		}
	}
	return req
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
