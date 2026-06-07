package sessionmanager

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type AllocatorWorker struct {
	sessionManager repositories.SessionManager
	client         *services.SessionAllocatorClient
	publicURL      string
}

func NewAllocatorWorker(sessionManager repositories.SessionManager, upstreamURL, token, publicURL string) *AllocatorWorker {
	return &AllocatorWorker{
		sessionManager: sessionManager,
		client:         services.NewSessionAllocatorClient(upstreamURL, token),
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

func (w *AllocatorWorker) process(ctx context.Context, allocation *services.SessionAllocationRequest) {
	settings := allocation.ProvisionSettings
	if settings == nil && allocation.Request != nil {
		settings = allocation.Request.ProvisionSettings
	}
	if settings == nil {
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, services.SessionAllocationResult{
			Status:  "error",
			Message: "provision_settings is required",
		})
		return
	}

	sessionID := uuid.New().String()
	req := runRequestFromSettings(settings)
	session, err := w.sessionManager.CreateSession(ctx, sessionID, req, nil)
	if err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to create session for allocation %s: %v", allocation.SessionID, err)
		_ = w.client.CompleteExternal(context.Background(), allocation.SessionID, services.SessionAllocationResult{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	if err := w.client.CompleteExternal(context.Background(), allocation.SessionID, services.SessionAllocationResult{
		Status:             "assigned",
		AllocatedSessionID: session.ID(),
		ProxyURL:           w.publicURL,
	}); err != nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Failed to complete allocation %s: %v", allocation.SessionID, err)
	}
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
