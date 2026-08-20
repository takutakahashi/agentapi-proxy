package app

import (
	"context"
	"errors"
	"testing"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type durableAllocationQueue struct {
	allocation *coreallocation.AllocationRequest
	getErr     error
	completed  bool
}

func (q *durableAllocationQueue) SubmitExternalSessionAllocation(context.Context, string, string, *sessionsettings.SessionSettings, *entities.RunServerRequest, *coreallocation.RuntimeBootstrap) error {
	return nil
}

func (q *durableAllocationQueue) GetSessionAllocation(context.Context, string) (*coreallocation.AllocationRequest, error) {
	return q.allocation, q.getErr
}

func (q *durableAllocationQueue) NextSessionAllocation(context.Context, time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return q.allocation, q.allocation != nil, nil
}

func (q *durableAllocationQueue) CompleteSessionAllocation(context.Context, string, coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	q.completed = true
	return q.allocation, nil
}

func (q *durableAllocationQueue) NextExternalSessionAllocation(context.Context, string, time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return nil, false, nil
}

func (q *durableAllocationQueue) CompleteExternalSessionAllocation(context.Context, string, coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	return nil, nil
}

type recordingSessionRouteRepository struct {
	route   *portrepos.SessionRoute
	saveErr error
}

func (r *recordingSessionRouteRepository) Save(_ context.Context, route *portrepos.SessionRoute) error {
	r.route = route
	return r.saveErr
}

func (r *recordingSessionRouteRepository) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return nil, nil
}

func (r *recordingSessionRouteRepository) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return nil, nil
}

func (r *recordingSessionRouteRepository) Delete(context.Context, string) error { return nil }

func TestLocalAllocationClientCompleteRestoresRouteFromDurableAllocation(t *testing.T) {
	queue := &durableAllocationQueue{allocation: &coreallocation.AllocationRequest{
		SessionID: "public-id",
		Request: &entities.RunServerRequest{
			UserID:         "user-id",
			Scope:          entities.ScopeTeam,
			TeamID:         "org/team",
			Tags:           map[string]string{"source": "webhook"},
			InitialMessage: "review this",
		},
	}}
	routes := &recordingSessionRouteRepository{}
	client := newLocalAllocationClient(queue, routes)

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if routes.route == nil {
		t.Fatal("Complete() did not persist a session route")
	}
	if routes.route.SessionID != "public-id" || routes.route.RemoteSessionID != "runtime-id" {
		t.Fatalf("route IDs = %q -> %q", routes.route.SessionID, routes.route.RemoteSessionID)
	}
	if routes.route.UserID != "user-id" || routes.route.Scope != string(entities.ScopeTeam) || routes.route.TeamID != "org/team" {
		t.Fatalf("route owner metadata = %#v", routes.route)
	}
	if routes.route.InitialMessage != "review this" || routes.route.Tags["source"] != "webhook" {
		t.Fatalf("route session metadata = %#v", routes.route)
	}
	if !queue.completed {
		t.Fatal("Complete() did not acknowledge the allocation")
	}
}

func TestLocalAllocationClientCompleteKeepsAllocationWhenRouteSaveFails(t *testing.T) {
	queue := &durableAllocationQueue{allocation: &coreallocation.AllocationRequest{SessionID: "public-id"}}
	routes := &recordingSessionRouteRepository{saveErr: errors.New("route unavailable")}
	client := newLocalAllocationClient(queue, routes)

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want route save error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged allocation after route save failure")
	}
}

func TestLocalAllocationClientCompleteFailsWhenDurableAllocationIsMissing(t *testing.T) {
	queue := &durableAllocationQueue{getErr: errors.New("not found")}
	client := newLocalAllocationClient(queue, &recordingSessionRouteRepository{})

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want durable allocation error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged missing allocation")
	}
}
