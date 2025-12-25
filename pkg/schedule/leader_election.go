package schedule

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderElectionConfig contains configuration for leader election
type LeaderElectionConfig struct {
	// LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting master will retry refreshing leadership before giving up
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait between tries of actions
	RetryPeriod time.Duration
	// LeaseName is the name of the lease resource
	LeaseName string
	// Namespace is the namespace for the lease resource
	Namespace string
}

// DefaultLeaderElectionConfig returns the default leader election configuration
func DefaultLeaderElectionConfig(namespace string) LeaderElectionConfig {
	return LeaderElectionConfig{
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		LeaseName:     "agentapi-schedule-worker",
		Namespace:     namespace,
	}
}

// LeaderElector manages leader election for the schedule worker
type LeaderElector struct {
	client   kubernetes.Interface
	config   LeaderElectionConfig
	identity string
}

// NewLeaderElector creates a new LeaderElector
func NewLeaderElector(client kubernetes.Interface, config LeaderElectionConfig) *LeaderElector {
	hostname, _ := os.Hostname()
	identity := hostname + "_" + uuid.New().String()[:8]

	return &LeaderElector{
		client:   client,
		config:   config,
		identity: identity,
	}
}

// Run starts the leader election loop
// onStartedLeading is called when this instance becomes the leader
// onStoppedLeading is called when this instance loses leadership
func (l *LeaderElector) Run(ctx context.Context, onStartedLeading func(ctx context.Context), onStoppedLeading func()) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      l.config.LeaseName,
			Namespace: l.config.Namespace,
		},
		Client: l.client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: l.identity,
		},
	}

	log.Printf("[LEADER_ELECTION] Starting leader election with identity %s", l.identity)

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   l.config.LeaseDuration,
		RenewDeadline:   l.config.RenewDeadline,
		RetryPeriod:     l.config.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Printf("[LEADER_ELECTION] Became leader")
				onStartedLeading(ctx)
			},
			OnStoppedLeading: func() {
				log.Printf("[LEADER_ELECTION] Lost leadership")
				if onStoppedLeading != nil {
					onStoppedLeading()
				}
			},
			OnNewLeader: func(identity string) {
				if identity != l.identity {
					log.Printf("[LEADER_ELECTION] New leader elected: %s", identity)
				}
			},
		},
	})
}

// Identity returns the identity of this elector
func (l *LeaderElector) Identity() string {
	return l.identity
}

// LeaderWorker combines leader election with the schedule worker
type LeaderWorker struct {
	worker  *Worker
	elector *LeaderElector
}

// NewLeaderWorker creates a new LeaderWorker
func NewLeaderWorker(
	manager Manager,
	sessionManager proxy.SessionManager,
	client kubernetes.Interface,
	workerConfig WorkerConfig,
	electionConfig LeaderElectionConfig,
) *LeaderWorker {
	worker := NewWorker(manager, sessionManager, workerConfig)
	elector := NewLeaderElector(client, electionConfig)

	return &LeaderWorker{
		worker:  worker,
		elector: elector,
	}
}

// Run starts the leader worker
// Only the leader will process schedules
func (lw *LeaderWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx,
		func(leaderCtx context.Context) {
			// We are the leader, start the worker
			if err := lw.worker.Start(leaderCtx); err != nil {
				log.Printf("[LEADER_WORKER] Failed to start worker: %v", err)
			}
		},
		func() {
			// We lost leadership, stop the worker
			lw.worker.Stop()
		},
	)
}

// Stop gracefully stops the leader worker
func (lw *LeaderWorker) Stop() {
	lw.worker.Stop()
}
