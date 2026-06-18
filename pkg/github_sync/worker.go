package githubsync

import (
	"context"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
)

// Worker runs periodic bidirectional sync for all enabled git_sync configs.
//
// Each settings is synced on a per-tenant schedule: the first sync time is
// offset by hash(settingsName) % interval so tenants don't hit GitHub simultaneously.
// Subsequent syncs happen at lastSyncedAt + interval.
//
// Direction is determined by comparing the remote .sync-meta.yaml syncedAt
// timestamp against the locally stored LastPushedAt:
//
//	remoteSyncedAt > LastPushedAt  →  Pull  (GitHub is newer)
//	otherwise                      →  Push  (local is newer or equal)
type Worker struct {
	syncer       *Syncer
	settingsRepo portrepos.SettingsRepository
	interval     time.Duration

	mu         sync.Mutex
	running    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
	nextSyncAt map[string]time.Time // per-settings next sync time
}

// NewWorker creates a Worker. interval must be > 0.
func NewWorker(syncer *Syncer, settingsRepo portrepos.SettingsRepository, interval time.Duration) *Worker {
	return &Worker{
		syncer:       syncer,
		settingsRepo: settingsRepo,
		interval:     interval,
		nextSyncAt:   make(map[string]time.Time),
	}
}

// Start begins the periodic sync loop. It returns immediately; the loop runs in the background.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run(ctx)
	log.Printf("[SYNC_WORKER] Starting periodic sync worker (interval=%s)", w.interval)
	return nil
}

// Stop signals the sync loop to stop and waits for it to exit.
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()
	w.wg.Wait()
	log.Printf("[SYNC_WORKER] Stopped periodic sync worker")
}

func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	// Check at finer granularity than the sync interval so per-tenant offsets work.
	checkInterval := 30 * time.Second
	if w.interval < checkInterval {
		checkInterval = w.interval
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	allSettings, err := w.settingsRepo.List(ctx)
	if err != nil {
		log.Printf("[SYNC_WORKER] Failed to list settings: %v", err)
		return
	}
	now := time.Now()
	for _, s := range allSettings {
		cfg := s.GitSync()
		if cfg == nil || !cfg.Enabled {
			continue
		}
		name := s.Name()

		w.mu.Lock()
		next, known := w.nextSyncAt[name]
		if !known {
			// First encounter: stagger based on deterministic hash of settings name.
			next = now.Add(tenantOffset(name, w.interval))
			w.nextSyncAt[name] = next
		}
		w.mu.Unlock()

		if now.Before(next) {
			continue
		}

		if err := w.syncOne(ctx, name); err != nil {
			log.Printf("[SYNC_WORKER] Sync failed for %s: %v", name, err)
		}

		w.mu.Lock()
		w.nextSyncAt[name] = time.Now().Add(w.interval)
		w.mu.Unlock()
	}
}

// tenantOffset returns a deterministic time offset in [0, interval) for a settings name.
func tenantOffset(name string, interval time.Duration) time.Duration {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	ms := uint64(interval.Milliseconds())
	if ms == 0 {
		return 0
	}
	return time.Duration(uint64(h.Sum32())%ms) * time.Millisecond
}

func (w *Worker) syncOne(ctx context.Context, settingsName string) error {
	settings, err := w.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return err
	}
	cfg := settings.GitSync()
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	token, err := w.syncer.resolveToken(cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return err
	}

	// Fetch per-settings .sync-meta.yaml to determine direction.
	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"
	metaPath := rootPath + settingsName + "/.sync-meta.yaml"

	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return err
	}

	remoteSyncedAt := time.Time{}
	if metaBytes, metaErr := ghClient.GetFile(ctx, cfg.Branch, metaPath); metaErr == nil {
		var meta SyncMeta
		if parseErr := yaml.Unmarshal(metaBytes, &meta); parseErr == nil {
			remoteSyncedAt = meta.SyncedAt
		}
	}

	// Determine sync direction.
	if !remoteSyncedAt.IsZero() && remoteSyncedAt.After(cfg.LastPushedAt) {
		log.Printf("[SYNC_WORKER] %s: remote is newer (%s > %s) — pulling", settingsName,
			remoteSyncedAt.Format(time.RFC3339), cfg.LastPushedAt.Format(time.RFC3339))
		_, err = w.syncer.Pull(ctx, settingsName, settingsName, false)
	} else {
		log.Printf("[SYNC_WORKER] %s: local is newer or equal — pushing", settingsName)
		_, err = w.syncer.Push(ctx, settingsName, settingsName, "")
	}
	return err
}

// LeaderWorker wraps Worker with Kubernetes leader election so only one
// replica in the cluster runs the periodic sync at a time.
type LeaderWorker struct {
	worker  *Worker
	elector *schedule.LeaderElector
}

// NewLeaderWorker creates a LeaderWorker.
func NewLeaderWorker(
	syncer *Syncer,
	settingsRepo portrepos.SettingsRepository,
	interval time.Duration,
	client kubernetes.Interface,
	electionConfig schedule.LeaderElectionConfig,
) *LeaderWorker {
	electionConfig.LeaseName = "agentapi-github-sync-worker"
	return &LeaderWorker{
		worker:  NewWorker(syncer, settingsRepo, interval),
		elector: schedule.NewLeaderElector(client, electionConfig),
	}
}

// Run starts the leader election loop. Only the elected leader runs the sync worker.
func (lw *LeaderWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx,
		func(leaderCtx context.Context) {
			log.Printf("[SYNC_WORKER] Became leader, starting periodic sync worker")
			if err := lw.worker.Start(leaderCtx); err != nil {
				log.Printf("[SYNC_WORKER] Failed to start worker: %v", err)
			}
		},
		func() {
			log.Printf("[SYNC_WORKER] Lost leadership, stopping periodic sync worker")
			lw.worker.Stop()
		},
	)
}
