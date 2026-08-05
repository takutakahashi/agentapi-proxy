package sessionallocation

import (
	"context"
	"log"
	"sync"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type SessionCreator interface {
	CreateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error)
}

// Worker is a leader-only loop that binds queued allocation requests to sessions.
type Worker struct {
	creator SessionCreator
	client  core.InternalAllocatorClient

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

func NewWorker(creator SessionCreator, client core.InternalAllocatorClient) *Worker {
	return &Worker{
		creator: creator,
		client:  client,
		stopCh:  make(chan struct{}),
	}
}

func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	go w.run(ctx)
	log.Printf("[SESSION_ALLOCATOR] Started")
	return nil
}

func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	w.running = false
	w.mu.Unlock()
	log.Printf("[SESSION_ALLOCATOR] Stopped")
}

func (w *Worker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}
		req, ok, err := w.client.Next(ctx, 30*time.Second)
		if err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to receive allocation request: %v", err)
			sleepOrContextDone(ctx, 2*time.Second)
			continue
		}
		if !ok {
			continue
		}
		w.processOne(ctx, req)
	}
}

func (w *Worker) processOne(ctx context.Context, req *core.AllocationRequest) {
	log.Printf("[SESSION_ALLOCATOR] Allocating session %s (sandbox=%t dind=%t agent_type=%s)",
		req.SessionID, req.Requirements.Sandbox, req.Requirements.DinD, req.Requirements.AgentType)
	sess, err := w.creator.CreateSessionDirect(ctx, req.SessionID, req.Request, req.WebhookPayload)
	if err != nil {
		_ = w.client.Complete(context.Background(), req.SessionID, core.AllocationResult{Status: core.StatusError, Message: err.Error()})
		log.Printf("[SESSION_ALLOCATOR] Allocation failed for %s: %v", req.SessionID, err)
		return
	}
	if err := w.client.Complete(context.Background(), req.SessionID, core.AllocationResult{Status: core.StatusAssigned, AllocatedSessionID: sess.ID()}); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to mark allocation %s assigned: %v", req.SessionID, err)
		return
	}
	log.Printf("[SESSION_ALLOCATOR] Allocated session %s as %s", req.SessionID, sess.ID())
}

func sleepOrContextDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
