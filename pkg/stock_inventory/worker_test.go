package stock_inventory

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestReplenishStockUsesConfiguredRequirements(t *testing.T) {
	repo := &recordingStockRepo{count: 1}
	requirements := StockRequirements{
		Sandbox: true,
		DinD:    true,
	}
	worker := NewWorker(repo, WorkerConfig{
		CheckInterval: time.Minute,
		TargetCount:   3,
		Requirements:  requirements,
		Enabled:       true,
	})

	worker.replenishStock(context.Background())

	if !reflect.DeepEqual(repo.countRequirements, requirements) {
		t.Fatalf("CountStockSessions requirements = %+v, want %+v", repo.countRequirements, requirements)
	}
	if len(repo.createRequirements) != 2 {
		t.Fatalf("CreateStockSession called %d times, want 2", len(repo.createRequirements))
	}
	for i, got := range repo.createRequirements {
		if !reflect.DeepEqual(got, requirements) {
			t.Fatalf("CreateStockSession[%d] requirements = %+v, want %+v", i, got, requirements)
		}
	}
}

type recordingStockRepo struct {
	count              int
	countRequirements StockRequirements
	createRequirements []StockRequirements
}

func (r *recordingStockRepo) CreateStockSession(_ context.Context, sandbox, dind bool) error {
	r.createRequirements = append(r.createRequirements, StockRequirements{Sandbox: sandbox, DinD: dind})
	return nil
}

func (r *recordingStockRepo) CountStockSessions(_ context.Context, sandbox, dind bool) (int, error) {
	r.countRequirements = StockRequirements{Sandbox: sandbox, DinD: dind}
	return r.count, nil
}

func (r *recordingStockRepo) PurgeStockSessions(context.Context) error {
	return nil
}
