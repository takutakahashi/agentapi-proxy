package stock_inventory

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"k8s.io/client-go/kubernetes"
)

// StockRepository manages the creation and counting of stock sessions.
type StockRepository interface {
	CreateStockSession(ctx context.Context) error
	CountStockSessions(ctx context.Context) (int, error)
	// PurgeStockSessions deletes all pre-warmed stock sessions. Called on
	// worker startup so that stale sessions (e.g. built from an old image)
	// are replaced with fresh ones.
	PurgeStockSessions(ctx context.Context) error
}

// WorkerConfig contains configuration for the stock inventory worker.
type WorkerConfig struct {
	// CheckInterval is how often to check and replenish stock sessions.
	CheckInterval time.Duration
	// TargetCount is the desired number of available stock sessions.
	TargetCount int
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

	log.Printf("[STOCK_INVENTORY] Started with check interval %v, target count %d",
		w.config.CheckInterval, w.config.TargetCount)
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

	// On startup, purge all existing stock sessions so that stale pods
	// (built from an old image) are replaced with fresh ones.
	log.Printf("[STOCK_INVENTORY] Purging existing stock sessions on startup")
	if err := w.repo.PurgeStockSessions(ctx); err != nil {
		log.Printf("[STOCK_INVENTORY] Warning: failed to purge stock sessions: %v", err)
	}

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
	count, err := w.repo.CountStockSessions(ctx)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Failed to count stock sessions: %v", err)
		return
	}

	needed := w.config.TargetCount - count
	if needed <= 0 {
		return
	}

	log.Printf("[STOCK_INVENTORY] Replenishing %d stock session(s) (current: %d, target: %d)",
		needed, count, w.config.TargetCount)

	for i := 0; i < needed; i++ {
		if err := w.repo.CreateStockSession(ctx); err != nil {
			log.Printf("[STOCK_INVENTORY] Failed to create stock session: %v", err)
		}
	}
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
	k8sClient kubernetes.Interface,
	workerConfig WorkerConfig,
	electionConfig schedule.LeaderElectionConfig,
) *LeaderWorker {
	worker := NewWorker(repo, workerConfig)
	elector := schedule.NewLeaderElector(k8sClient, electionConfig)
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
