package app

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// routingSessionManager preserves all lifecycle operations of the local
// manager, but routes new sessions after profiles have been resolved.
type routingSessionManager struct {
	server   *Server
	delegate portrepos.SessionManager
}

func (m *routingSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.server.createRoutedSession(ctx, id, req, webhookPayload)
}

func (m *routingSessionManager) GetSession(id string) entities.Session {
	return m.delegate.GetSession(id)
}

func (m *routingSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	return m.delegate.ListSessions(filter)
}

func (m *routingSessionManager) DeleteSession(id string) error {
	return m.delegate.DeleteSession(id)
}

func (m *routingSessionManager) SendMessage(ctx context.Context, id, message string) error {
	return m.delegate.SendMessage(ctx, id, message)
}

func (m *routingSessionManager) StopAgent(ctx context.Context, id string) error {
	return m.delegate.StopAgent(ctx, id)
}

func (m *routingSessionManager) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	return m.delegate.GetMessages(ctx, id)
}

func (m *routingSessionManager) Shutdown(timeout time.Duration) error {
	return m.delegate.Shutdown(timeout)
}

// createRoutedSession resolves explicit manager IDs, allocator selectors, and
// tenant defaults for trigger-based launches. Profile settings have already
// been merged into req by LaunchUseCase at this point.
func (s *Server) createRoutedSession(ctx context.Context, sessionID string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	if req == nil {
		return nil, fmt.Errorf("run server request is required")
	}

	var selected *entities.ExternalSessionManagerEntry
	var err error
	if req.ManagerID != "" {
		selected, err = s.findESMByID(ctx, req.UserID, req.Teams, req.ManagerID)
		if err != nil {
			return nil, fmt.Errorf("find external session manager %s: %w", req.ManagerID, err)
		}
		if selected == nil {
			return nil, fmt.Errorf("external session manager not found: %s", req.ManagerID)
		}
	} else {
		selected, err = s.findDefaultESM(ctx, req.UserID, req.Teams, req.Tags)
		if err != nil {
			return nil, fmt.Errorf("select external session manager: %w", err)
		}
		if selected == nil && hasAllocatorSelector(req.Tags) {
			return nil, fmt.Errorf("no external session manager matches allocator.* tags")
		}
	}

	if selected == nil {
		return s.sessionManager.CreateSession(ctx, sessionID, req, webhookPayload)
	}
	if (req.Sandbox != nil && req.Sandbox.Enabled) || (req.Docker != nil && req.Docker.Enabled) {
		return nil, fmt.Errorf("external session manager routing does not support sandbox or Docker-in-Docker")
	}

	req.ManagerID = selected.ID
	return s.createRemoteRunSession(ctx, sessionID, selected, req)
}
