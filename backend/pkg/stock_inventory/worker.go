package stock_inventory

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
)

// StockRepository manages the creation and counting of stock sessions.
type StockRepository interface {
	CreateStockSession(ctx context.Context, dind bool) error
	CountStockSessions(ctx context.Context, dind bool) (int, error)
	// PurgeStaleStockSessions deletes pre-warmed sessions created by a
	// different proxy Pod template revision.
	PurgeStaleStockSessions(ctx context.Context) error
}

type PoolStockRepository interface {
	CreateStockSessionForPool(ctx context.Context, pool string, dind bool) error
	CountStockSessionsForPool(ctx context.Context, pool string, dind bool) (int, error)
}

// StockRequirements captures the pod capabilities a stock session is prepared for.
// Note: Sandbox (network filter) and scia sidecar are now always enabled and cannot be opted out.
// Only DinD (Docker-in-Docker) remains configurable.
type StockRequirements struct {
	DinD bool
}

// StockPool captures one stock inventory target for a capability set.
type StockPool struct {
	Name         string
	TargetCount  int
	Requirements StockRequirements
}

// WorkerConfig contains configuration for the stock inventory worker.
type WorkerConfig struct {
	// CheckInterval is how often to check and replenish stock sessions.
	CheckInterval time.Duration
	// TargetCount is the desired number of available stock sessions.
	TargetCount int
	// Requirements is the pod capability template for stock sessions.
	Requirements StockRequirements
	// Pools optionally configures multiple stock inventory targets. When set,
	// TargetCount and Requirements are ignored.
	Pools []StockPool
	// Enabled indicates whether the worker should run.
	Enabled bool
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		CheckInterval: 30 * time.Second,
		TargetCount:   2,
		Enabled:       true,
	}
}

// Worker periodically checks the number of available stock sessions and
// creates new ones when the count falls below TargetCount.
type Worker struct {
	repo   StockRepository
	config WorkerConfig

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewWorker creates a new stock inventory worker.
func NewWorker(repo StockRepository, config WorkerConfig) *Worker {
	return &Worker{
		repo:   repo,
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start begins the worker loop in a background goroutine.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run(ctx)

	log.Printf("[STOCK_INVENTORY] Started with check interval %v, pools %d",
		w.config.CheckInterval, len(w.effectivePools()))
	return nil
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.stopCh)
	w.wg.Wait()
	log.Printf("[STOCK_INVENTORY] Stopped")
}

// run is the main worker loop.
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start.
	w.replenishStock(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[STOCK_INVENTORY] Context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Printf("[STOCK_INVENTORY] Stop signal received")
			return
		case <-ticker.C:
			w.replenishStock(ctx)
		}
	}
}

// replenishStock checks the current stock count and creates sessions to reach TargetCount.
func (w *Worker) replenishStock(ctx context.Context) {
	if err := w.repo.PurgeStaleStockSessions(ctx); err != nil {
		log.Printf("[STOCK_INVENTORY] Warning: failed to purge stale stock sessions: %v", err)
		return
	}
	for _, pool := range w.effectivePools() {
		w.replenishPool(ctx, pool)
	}
}

func (w *Worker) replenishPool(ctx context.Context, pool StockPool) {
	count, err := w.countPool(ctx, pool)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Failed to count stock sessions: %v", err)
		return
	}

	needed := pool.TargetCount - count
	if needed <= 0 {
		return
	}

	log.Printf("[STOCK_INVENTORY] Replenishing %d stock session(s) (current: %d, target: %d, dind=%t)",
		needed, count, pool.TargetCount, pool.Requirements.DinD)

	for i := 0; i < needed; i++ {
		if err := w.createPool(ctx, pool); err != nil {
			log.Printf("[STOCK_INVENTORY] Failed to create stock session: %v", err)
		}
	}
}

func (w *Worker) countPool(ctx context.Context, pool StockPool) (int, error) {
	if named, ok := w.repo.(PoolStockRepository); ok && pool.Name != "" {
		return named.CountStockSessionsForPool(ctx, pool.Name, pool.Requirements.DinD)
	}
	return w.repo.CountStockSessions(ctx, pool.Requirements.DinD)
}

func (w *Worker) createPool(ctx context.Context, pool StockPool) error {
	if named, ok := w.repo.(PoolStockRepository); ok && pool.Name != "" {
		return named.CreateStockSessionForPool(ctx, pool.Name, pool.Requirements.DinD)
	}
	return w.repo.CreateStockSession(ctx, pool.Requirements.DinD)
}

func (w *Worker) effectivePools() []StockPool {
	if len(w.config.Pools) > 0 {
		return w.config.Pools
	}
	return []StockPool{{
		TargetCount:  w.config.TargetCount,
		Requirements: w.config.Requirements,
	}}
}

// LeaderWorker wraps Worker with Kubernetes leader election so that only one
// replica runs the inventory loop at a time.
type LeaderWorker struct {
	worker  *Worker
	elector *schedule.LeaderElector
}

// NewLeaderWorker creates a LeaderWorker using the provided Kubernetes client and
// leader election configuration.
func NewLeaderWorker(
	repo StockRepository,
	redisClient redis.UniversalClient,
	workerConfig WorkerConfig,
	electionConfig schedule.LeaderElectionConfig,
) *LeaderWorker {
	worker := NewWorker(repo, workerConfig)
	elector := schedule.NewLeaderElector(redisClient, electionConfig)
	return &LeaderWorker{
		worker:  worker,
		elector: elector,
	}
}

// Run starts the leader election loop. This blocks until ctx is cancelled.
func (lw *LeaderWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx,
		func(leaderCtx context.Context) {
			log.Printf("[STOCK_INVENTORY] Became leader, starting worker")
			if err := lw.worker.Start(leaderCtx); err != nil {
				log.Printf("[STOCK_INVENTORY] Failed to start worker: %v", err)
			}
		},
		func() {
			log.Printf("[STOCK_INVENTORY] Lost leadership, stopping worker")
			lw.worker.Stop()
		},
	)
}

// Stop stops the leader worker.
func (lw *LeaderWorker) Stop() {
	lw.worker.Stop()
}
