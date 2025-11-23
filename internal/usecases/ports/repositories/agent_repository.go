package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type AgentRepository interface {
	Save(ctx context.Context, agent *entities.Agent) error
	Update(ctx context.Context, agent *entities.Agent) error
	FindByID(ctx context.Context, id entities.AgentID) (*entities.Agent, error)
	FindBySessionID(ctx context.Context, sessionID entities.SessionID) ([]*entities.Agent, error)
	FindAll(ctx context.Context) ([]*entities.Agent, error)
	Delete(ctx context.Context, id entities.AgentID) error
	DeleteBySessionID(ctx context.Context, sessionID entities.SessionID) error
}
