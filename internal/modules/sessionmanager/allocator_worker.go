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
	sessionManager repositories.SessionManager
	client         sessionallocation.ExternalAllocatorClient
	publicURL      string
}

type directSessionManager interface {
	CreateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error)
}

func NewAllocatorWorker(sessionManager repositories.SessionManager, upstreamURL, token, publicURL string) *AllocatorWorker {
	return NewAllocatorWorkerWithClient(sessionManager, infrasessionallocation.NewClient(upstreamURL, token), publicURL)
}

func NewAllocatorWorkerWithClient(sessionManager repositories.SessionManager, client sessionallocation.ExternalAllocatorClient, publicURL string) *AllocatorWorker {
	return &AllocatorWorker{
		sessionManager: sessionManager,
		client:         client,
		publicURL:      publicURL,
	}
}

func (w *AllocatorWorker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		allocation, ok, err := w.client.NextExternal(ctx, 30*time.Second)
		if err != nil {
			log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to poll allocation: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		if !ok {
			continue
		}
		w.process(ctx, allocation)
	}
}

func (w *AllocatorWorker) process(ctx context.Context, allocation *sessionallocation.AllocationRequest) {
	settings := allocation.ProvisionSettings
	if settings == nil && allocation.Request != nil {
		settings = allocation.Request.ProvisionSettings
	}
	if settings == nil {
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, sessionallocation.AllocationResult{
			Status:  sessionallocation.StatusError,
			Message: "provision_settings is required",
		})
		return
	}

	sessionID := uuid.New().String()
	req := runRequestFromSettings(settings)
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
