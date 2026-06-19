package sessionallocation

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type InternalAllocatorClient interface {
	Next(ctx context.Context, wait time.Duration) (*AllocationRequest, bool, error)
	Complete(ctx context.Context, sessionID string, result AllocationResult) error
}

type ExternalAllocatorClient interface {
	NextExternal(ctx context.Context, wait time.Duration) (*AllocationRequest, bool, error)
	CompleteExternal(ctx context.Context, sessionID string, result AllocationResult) error
}

type Queue interface {
	SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, req *entities.RunServerRequest) error
	NextSessionAllocation(ctx context.Context, wait time.Duration) (*AllocationRequest, bool, error)
	CompleteSessionAllocation(ctx context.Context, sessionID string, result AllocationResult) error
	NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*AllocationRequest, bool, error)
	CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result AllocationResult) (*AllocationRequest, error)
}

type Notifier interface {
	Notify(ctx context.Context) error
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}
