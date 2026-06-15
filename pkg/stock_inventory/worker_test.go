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

func TestReplenishStockUsesAllConfiguredPools(t *testing.T) {
	repo := &recordingStockRepo{
		counts: map[StockRequirements]int{
			{Sandbox: false, DinD: false}: 1,
			{Sandbox: false, DinD: true}:  0,
			{Sandbox: true, DinD: false}:  2,
			{Sandbox: true, DinD: true}:   1,
		},
	}
	pools := []StockPool{
		{TargetCount: 2, Requirements: StockRequirements{Sandbox: false, DinD: false}},
		{TargetCount: 2, Requirements: StockRequirements{Sandbox: false, DinD: true}},
		{TargetCount: 2, Requirements: StockRequirements{Sandbox: true, DinD: false}},
		{TargetCount: 2, Requirements: StockRequirements{Sandbox: true, DinD: true}},
	}
	worker := NewWorker(repo, WorkerConfig{
		CheckInterval: time.Minute,
		Pools:         pools,
		Enabled:       true,
	})

	worker.replenishStock(context.Background())

	if !reflect.DeepEqual(repo.countedRequirements, []StockRequirements{
		{Sandbox: false, DinD: false},
		{Sandbox: false, DinD: true},
		{Sandbox: true, DinD: false},
		{Sandbox: true, DinD: true},
	}) {
		t.Fatalf("CountStockSessions requirements = %+v", repo.countedRequirements)
	}

	wantCreates := []StockRequirements{
		{Sandbox: false, DinD: false},
		{Sandbox: false, DinD: true},
		{Sandbox: false, DinD: true},
		{Sandbox: true, DinD: true},
	}
	if !reflect.DeepEqual(repo.createRequirements, wantCreates) {
		t.Fatalf("CreateStockSession requirements = %+v, want %+v", repo.createRequirements, wantCreates)
	}
}

type recordingStockRepo struct {
	count               int
	counts              map[StockRequirements]int
	countRequirements   StockRequirements
	countedRequirements []StockRequirements
	createRequirements  []StockRequirements
}

func (r *recordingStockRepo) CreateStockSession(_ context.Context, sandbox, dind bool) error {
	r.createRequirements = append(r.createRequirements, StockRequirements{Sandbox: sandbox, DinD: dind})
	return nil
}

func (r *recordingStockRepo) CountStockSessions(_ context.Context, sandbox, dind bool) (int, error) {
	requirements := StockRequirements{Sandbox: sandbox, DinD: dind}
	r.countRequirements = requirements
	r.countedRequirements = append(r.countedRequirements, requirements)
	if r.counts != nil {
		return r.counts[requirements], nil
	}
	return r.count, nil
}

func (r *recordingStockRepo) PurgeStockSessions(context.Context) error {
	return nil
}
