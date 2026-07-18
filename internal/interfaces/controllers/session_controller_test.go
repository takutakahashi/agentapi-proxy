package controllers

import (
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type sessionListTestSession struct {
	id     string
	status string
}

func (s *sessionListTestSession) ID() string                    { return s.id }
func (s *sessionListTestSession) Addr() string                  { return "" }
func (s *sessionListTestSession) UserID() string                { return "user-1" }
func (s *sessionListTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *sessionListTestSession) TeamID() string                { return "" }
func (s *sessionListTestSession) Tags() map[string]string       { return nil }
func (s *sessionListTestSession) Status() string {
	if s.status == "" {
		return "running"
	}
	return s.status
}
func (s *sessionListTestSession) StartedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) UpdatedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) LastMessageAt() time.Time { return time.Time{} }
func (s *sessionListTestSession) Description() string      { return "" }
func (s *sessionListTestSession) Cancel()                  {}

func TestExcludeAllocatedSessions(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "public-id"},
		&sessionListTestSession{id: "allocated-id"},
		&sessionListTestSession{id: "local-id"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-id", RemoteSessionID: "allocated-id"},
	}

	got := excludeAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("excludeAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got[0].ID() != "public-id" || got[1].ID() != "local-id" {
		t.Fatalf("excludeAllocatedSessions() returned IDs %q and %q, want public-id and local-id", got[0].ID(), got[1].ID())
	}
}

func TestIndexAllocatedSessionsPreservesRuntimeStatus(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "allocated-running", status: "running"},
		&sessionListTestSession{id: "allocated-stable", status: "stable"},
		&sessionListTestSession{id: "local-id", status: "active"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-running", RemoteSessionID: "allocated-running"},
		{SessionID: "public-stable", RemoteSessionID: "allocated-stable"},
	}

	got := indexAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("indexAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got["allocated-running"].Status() != "running" {
		t.Fatalf("running session status = %q, want running", got["allocated-running"].Status())
	}
	if got["allocated-stable"].Status() != "stable" {
		t.Fatalf("stable session status = %q, want stable", got["allocated-stable"].Status())
	}
	if status := routedSessionStatus(routes[0], got); status != "running" {
		t.Fatalf("public running session status = %q, want running", status)
	}
	if status := routedSessionStatus(routes[1], got); status != "stable" {
		t.Fatalf("public stable session status = %q, want stable", status)
	}
}

func TestRoutedSessionStatusFallbacks(t *testing.T) {
	tests := []struct {
		name  string
		route *repositories.SessionRoute
		want  string
	}{
		{name: "allocation pending", route: &repositories.SessionRoute{SessionID: "public-id"}, want: "creating"},
		{name: "remote session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id"}, want: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedSessionStatus(tt.route, nil); got != tt.want {
				t.Fatalf("routedSessionStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
