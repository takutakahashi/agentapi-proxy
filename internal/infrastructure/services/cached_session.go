package services

import (
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// cachedSession is a read-only implementation of entities.Session that is
// reconstructed from a CachedSessionDTO when a session is not present in this
// pod's in-memory map.  It allows ListSessions to serve results from the Redis
// cache without hitting the Kubernetes API.
type cachedSession struct {
	dto portrepos.CachedSessionDTO
}

// newCachedSession wraps a CachedSessionDTO as an entities.Session.
func newCachedSession(dto portrepos.CachedSessionDTO) *cachedSession {
	return &cachedSession{dto: dto}
}

// Compile-time check.
var _ entities.Session = (*cachedSession)(nil)

func (s *cachedSession) ID() string {
	return s.dto.ID
}

func (s *cachedSession) Addr() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d",
		s.dto.ServiceName, s.dto.Namespace, s.dto.ServicePort)
}

func (s *cachedSession) UserID() string {
	return s.dto.UserID
}

func (s *cachedSession) Scope() entities.ResourceScope {
	if s.dto.Scope == "" {
		return entities.ScopeUser
	}
	return entities.ResourceScope(s.dto.Scope)
}

func (s *cachedSession) TeamID() string {
	return s.dto.TeamID
}

func (s *cachedSession) Tags() map[string]string {
	return s.dto.Tags
}

func (s *cachedSession) Status() string {
	return s.dto.Status
}

func (s *cachedSession) StartedAt() time.Time {
	return s.dto.StartedAt
}

func (s *cachedSession) UpdatedAt() time.Time {
	return s.dto.UpdatedAt
}

func (s *cachedSession) LastMessageAt() time.Time {
	return s.dto.LastMessageAt
}

func (s *cachedSession) Description() string {
	return s.dto.Description
}

// Cancel is a no-op for cached sessions (they are read-only snapshots).
func (s *cachedSession) Cancel() {}
