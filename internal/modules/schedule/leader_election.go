package schedule

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	ScheduleWorkerLeaseName        = "agentapi-schedule-worker"
	SlackbotCleanupWorkerLeaseName = "agentapi-slackbot-cleanup-worker"
	StockInventoryWorkerLeaseName  = "agentapi-stock-inventory-worker"
	SessionAllocatorLeaseName      = "agentapi-session-allocator"
)

type LeaderElectionConfig struct {
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
	LeaseName     string
	Namespace     string
}

func DefaultLeaderElectionConfig(namespace string) LeaderElectionConfig {
	return LeaderElectionConfig{LeaseDuration: 15 * time.Second, RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second, LeaseName: ScheduleWorkerLeaseName, Namespace: namespace}
}

// LeaderElector uses a Redis lease. The compare-and-renew/release scripts ensure
// a candidate can only mutate a lease it owns.
type LeaderElector struct {
	client   redis.UniversalClient
	config   LeaderElectionConfig
	identity string
}

func NewLeaderElector(client redis.UniversalClient, config LeaderElectionConfig) *LeaderElector {
	hostname, _ := os.Hostname()
	return &LeaderElector{client: client, config: config, identity: hostname + "_" + uuid.NewString()[:8]}
}

var renewLease = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`)

var releaseLease = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0`)

func (l *LeaderElector) Run(ctx context.Context, onStartedLeading func(context.Context), onStoppedLeading func()) {
	key := "agentapi:leader:" + l.config.Namespace + ":" + l.config.LeaseName
	for ctx.Err() == nil {
		acquired, err := l.client.SetNX(ctx, key, l.identity, l.config.LeaseDuration).Result()
		if err != nil || !acquired {
			if err != nil {
				log.Printf("[LEADER_ELECTION] Redis acquire failed: %v", err)
			}
			if !waitFor(ctx, l.config.RetryPeriod) {
				return
			}
			continue
		}
		leaderCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); onStartedLeading(leaderCtx) }()
		lost := l.renew(ctx, key)
		cancel()
		<-done
		_, _ = releaseLease.Run(context.Background(), l.client, []string{key}, l.identity).Result()
		if onStoppedLeading != nil {
			onStoppedLeading()
		}
		if !lost || ctx.Err() != nil {
			return
		}
	}
}

func (l *LeaderElector) renew(ctx context.Context, key string) bool {
	interval := l.config.RenewDeadline / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			result, err := renewLease.Run(ctx, l.client, []string{key}, l.identity, l.config.LeaseDuration.Milliseconds()).Int64()
			if err != nil || result == 0 {
				return true
			}
		}
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *LeaderElector) Identity() string { return l.identity }

type LeaderWorker struct {
	worker  *Worker
	elector *LeaderElector
}

func NewLeaderWorker(manager Manager, sessionManager portrepos.SessionManager, client redis.UniversalClient, workerConfig WorkerConfig, electionConfig LeaderElectionConfig, memoryRepo portrepos.MemoryRepository, sessionProfileRepo portrepos.SessionProfileRepository) *LeaderWorker {
	return &LeaderWorker{worker: NewWorker(manager, sessionManager, memoryRepo, workerConfig, sessionProfileRepo), elector: NewLeaderElector(client, electionConfig)}
}

func (lw *LeaderWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx, func(leaderCtx context.Context) { _ = lw.worker.Start(leaderCtx); <-leaderCtx.Done() }, lw.worker.Stop)
}

func (lw *LeaderWorker) Stop() { lw.worker.Stop() }

var ErrRedisRequired = errors.New("worker leader election requires Redis")
