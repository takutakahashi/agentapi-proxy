package repositories

import (
	"context"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Compile-time assertions: ensure NoopStatusRepository satisfies both interfaces.
var _ portrepos.StatusEventRepository = (*NoopStatusRepository)(nil)
var _ portrepos.SessionListCacheRepository = (*NoopStatusRepository)(nil)

// NoopStatusRepository is a no-op implementation of StatusEventRepository.
// It is used when Redis is not configured so that the rest of the code can
// remain unaware of whether a real backend is available.
type NoopStatusRepository struct{}

// NewNoopStatusRepository returns a new NoopStatusRepository.
func NewNoopStatusRepository() *NoopStatusRepository {
	return &NoopStatusRepository{}
}

// SetStatus does nothing and returns nil.
func (n *NoopStatusRepository) SetStatus(_ context.Context, _, _, _ string) error {
	return nil
}

// GetStatus always reports "not found" (empty status).
func (n *NoopStatusRepository) GetStatus(_ context.Context, _ string) (string, time.Time, error) {
	return "", time.Time{}, nil
}

// PublishStatusChange does nothing and returns nil.
func (n *NoopStatusRepository) PublishStatusChange(_ context.Context, _ portrepos.StatusChangeEvent) error {
	return nil
}

// SubscribeGlobal returns a channel that is immediately closed when ctx is done.
func (n *NoopStatusRepository) SubscribeGlobal(ctx context.Context) (<-chan portrepos.StatusChangeEvent, error) {
	ch := make(chan portrepos.StatusChangeEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// DeleteStatus does nothing and returns nil.
func (n *NoopStatusRepository) DeleteStatus(_ context.Context, _ string) error {
	return nil
}

// --------------------------------------------------------------------------
// SessionListCacheRepository (noop) implementation
// --------------------------------------------------------------------------

// SetSessionListCache does nothing (no-op).
func (n *NoopStatusRepository) SetSessionListCache(_ context.Context, _ string, _ []portrepos.CachedSessionDTO, _ time.Duration) error {
	return nil
}

// GetSessionListCache always returns a cache miss.
func (n *NoopStatusRepository) GetSessionListCache(_ context.Context, _ string) ([]portrepos.CachedSessionDTO, error) {
	return nil, nil
}

// InvalidateSessionListCache does nothing.
func (n *NoopStatusRepository) InvalidateSessionListCache(_ context.Context, _ string) error {
	return nil
}
