package githubsync

import (
	"context"
	"log"
	"strings"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"gopkg.in/yaml.v3"
)

// Worker runs periodic bidirectional sync for all enabled git_sync configs.
// Direction is determined by comparing the remote .sync-meta.yaml syncedAt
// timestamp against the locally stored LastPushedAt:
//
//	remoteSyncedAt > LastPushedAt  →  Pull  (GitHub is newer — someone pushed externally)
//	otherwise                      →  Push  (local is newer or equal)
type Worker struct {
	syncer       *Syncer
	settingsRepo portrepos.SettingsRepository
	interval     time.Duration
}

// NewWorker creates a Worker. interval must be > 0.
func NewWorker(syncer *Syncer, settingsRepo portrepos.SettingsRepository, interval time.Duration) *Worker {
	return &Worker{
		syncer:       syncer,
		settingsRepo: settingsRepo,
		interval:     interval,
	}
}

// Start runs the periodic sync loop until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	log.Printf("[SYNC_WORKER] Starting periodic sync worker (interval=%s)", w.interval)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[SYNC_WORKER] Stopping periodic sync worker")
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
	for _, s := range allSettings {
		cfg := s.GitSync()
		if cfg == nil || !cfg.Enabled {
			continue
		}
		name := s.Name()
		if err := w.syncOne(ctx, name); err != nil {
			log.Printf("[SYNC_WORKER] Sync failed for %s: %v", name, err)
		}
	}
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

	token, err := w.syncer.resolveToken(cfg.GitHubToken)
	if err != nil {
		return err
	}

	// Fetch remote sync-meta to determine direction.
	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"
	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return err
	}

	remoteSyncedAt := time.Time{}
	if metaBytes, metaErr := ghClient.GetFile(ctx, cfg.Branch, rootPath+".sync-meta.yaml"); metaErr == nil {
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
