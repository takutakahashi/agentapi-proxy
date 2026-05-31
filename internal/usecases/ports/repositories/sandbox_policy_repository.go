package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SandboxPolicyFilter defines filter criteria for listing sandbox policies.
type SandboxPolicyFilter struct {
	Scope   entities.ResourceScope
	OwnerID string
	TeamID  string
	TeamIDs []string
	Name    string
}

// SandboxPolicyRepository defines the interface for sandbox policy persistence.
type SandboxPolicyRepository interface {
	Create(ctx context.Context, policy *entities.SandboxPolicy) error
	GetByID(ctx context.Context, id string) (*entities.SandboxPolicy, error)
	List(ctx context.Context, filter SandboxPolicyFilter) ([]*entities.SandboxPolicy, error)
	Update(ctx context.Context, policy *entities.SandboxPolicy) error
	Delete(ctx context.Context, id string) error
}
