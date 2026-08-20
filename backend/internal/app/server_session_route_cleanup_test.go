package app

import (
	"context"
	"testing"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type cleanupRouteRepository struct {
	routes  []*portrepos.SessionRoute
	deleted []string
}

func (r *cleanupRouteRepository) Save(context.Context, *portrepos.SessionRoute) error { return nil }
func (r *cleanupRouteRepository) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return nil, nil
}
func (r *cleanupRouteRepository) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return r.routes, nil
}
func (r *cleanupRouteRepository) Delete(_ context.Context, sessionID string) error {
	r.deleted = append(r.deleted, sessionID)
	return nil
}

func TestCleanupLocalSessionRoutes(t *testing.T) {
	repo := &cleanupRouteRepository{routes: []*portrepos.SessionRoute{
		{SessionID: "public-local", RemoteSessionID: "runtime-id"},
		{SessionID: "other-local", RemoteSessionID: "other-runtime"},
		{SessionID: "public-external", RemoteSessionID: "runtime-id", ProxyURL: "https://esm.example"},
	}}

	cleanupLocalSessionRoutes(context.Background(), repo, "runtime-id")

	if len(repo.deleted) != 1 || repo.deleted[0] != "public-local" {
		t.Fatalf("deleted routes = %v, want [public-local]", repo.deleted)
	}
}
