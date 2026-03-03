package memory_summarizer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// summarizeSourceSessionIDTag is the session tag key used to identify which source session
// a summarize-drafts session is processing. Used for deduplication checks.
const summarizeSourceSessionIDTag = "summarize-source-session-id"

// defaultInitialMessageWaitSecond is the wait time (in seconds) before the summarize-drafts
// session sends its initial message. This gives the memory-sync sidecar enough time to
// complete its final upsert after the source session's pod is terminated.
const defaultInitialMessageWaitSecond = 30

// WorkerConfig holds configuration for the memory draft summarizer worker.
type WorkerConfig struct {
	// CheckInterval is how often the worker scans for orphaned draft memories.
	// Default: 5m
	CheckInterval time.Duration
	// Enabled controls whether the worker actually runs.
	Enabled bool
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		CheckInterval: 5 * time.Minute,
		Enabled:       true,
	}
}

// Worker periodically lists all draft memories and starts summarize-drafts sessions
// for those whose source session no longer exists.
type Worker struct {
	memoryRepo     portrepos.MemoryRepository
	sessionManager portrepos.SessionManager
	config         WorkerConfig

	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
	wg      sync.WaitGroup
}

// NewWorker creates a new memory draft summarizer Worker.
func NewWorker(
	memoryRepo portrepos.MemoryRepository,
	sessionManager portrepos.SessionManager,
	config WorkerConfig,
) *Worker {
	return &Worker{
		memoryRepo:     memoryRepo,
		sessionManager: sessionManager,
		config:         config,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the worker loop. It is safe to call from multiple goroutines.
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

	log.Printf("[MEMORY_SUMMARIZER] Started with check interval %v", w.config.CheckInterval)
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
	close(w.stopCh)
	w.mu.Unlock()

	w.wg.Wait()
	log.Printf("[MEMORY_SUMMARIZER] Stopped")
}

// run is the main worker loop.
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start
	w.processDraftMemories(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[MEMORY_SUMMARIZER] Context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Printf("[MEMORY_SUMMARIZER] Stop signal received")
			return
		case <-ticker.C:
			w.processDraftMemories(ctx)
		}
	}
}

// processDraftMemories lists all draft memories, groups them by source session ID,
// and starts summarize-drafts sessions for those whose source session no longer exists.
func (w *Worker) processDraftMemories(ctx context.Context) {
	if w.memoryRepo == nil {
		return
	}

	// List all draft memories across all users/teams
	memories, err := w.memoryRepo.List(ctx, portrepos.MemoryFilter{
		Tags: map[string]string{"draft": "true"},
	})
	if err != nil {
		log.Printf("[MEMORY_SUMMARIZER] Failed to list draft memories: %v", err)
		return
	}

	if len(memories) == 0 {
		return
	}

	log.Printf("[MEMORY_SUMMARIZER] Found %d draft memory entry(ies), checking source sessions...", len(memories))

	// Group draft memories by session-id tag; use the first entry per session to
	// derive scope/ownerID/teamID (all entries for a session share the same values).
	type sessionInfo struct {
		scope   entities.ResourceScope
		ownerID string
		teamID  string
	}
	sessionMap := make(map[string]sessionInfo)
	for _, m := range memories {
		sourceSessionID := m.Tags()["session-id"]
		if sourceSessionID == "" {
			continue
		}
		if _, seen := sessionMap[sourceSessionID]; !seen {
			sessionMap[sourceSessionID] = sessionInfo{
				scope:   m.Scope(),
				ownerID: m.OwnerID(),
				teamID:  m.TeamID(),
			}
		}
	}

	for sourceSessionID, info := range sessionMap {
		// Skip if the source session is still running
		if w.sessionManager.GetSession(sourceSessionID) != nil {
			continue
		}

		log.Printf("[MEMORY_SUMMARIZER] Source session %s is gone, starting summarize-drafts session", sourceSessionID)
		if err := StartSummarizeDraftsSession(ctx, w.sessionManager, sourceSessionID, info.scope, info.teamID, info.ownerID); err != nil {
			log.Printf("[MEMORY_SUMMARIZER] Failed to start summarize-drafts for session %s: %v", sourceSessionID, err)
		}
	}
}

// StartSummarizeDraftsSession starts a oneshot summarize-drafts session for the given
// source session. It performs a deduplication check before creating the session to avoid
// running multiple summarize sessions for the same source session concurrently.
func StartSummarizeDraftsSession(
	ctx context.Context,
	sessionManager portrepos.SessionManager,
	sourceSessionID string,
	scope entities.ResourceScope,
	teamID string,
	ownerID string,
) error {
	// Deduplication: skip if a summarize session is already running for this source session.
	existing := sessionManager.ListSessions(entities.SessionFilter{
		Tags: map[string]string{summarizeSourceSessionIDTag: sourceSessionID},
	})
	if len(existing) > 0 {
		log.Printf("[MEMORY_SUMMARIZER] Summarize session already running for source session %s, skipping", sourceSessionID)
		return nil
	}

	sessionID := uuid.New().String()
	today := time.Now().Format("2006-01-02")
	waitSec := defaultInitialMessageWaitSecond

	req := &entities.RunServerRequest{
		UserID: ownerID,
		Scope:  scope,
		TeamID: teamID,
		Tags:   map[string]string{summarizeSourceSessionIDTag: sourceSessionID},
		// InitialMessage is embedded directly to avoid an external dependency on the CLI package.
		InitialMessage:           buildSummarizationMessage(sourceSessionID, today),
		Oneshot:                  true,
		InitialMessageWaitSecond: &waitSec,
		// MemoryKey is intentionally not set: summarize sessions must not accumulate
		// their own draft memories, which would trigger recursive summarization.
	}

	if _, err := sessionManager.CreateSession(ctx, sessionID, req, nil); err != nil {
		return fmt.Errorf("create summarize-drafts session: %w", err)
	}

	log.Printf("[MEMORY_SUMMARIZER] Summarize-drafts session %s started for source session %s", sessionID, sourceSessionID)
	return nil
}

// buildSummarizationMessage returns the initial message for a draft summarization session.
func buildSummarizationMessage(sourceSessionID, today string) string {
	return fmt.Sprintf(
		"前のセッション（セッション ID: %s）のドラフトメモリをサマライズし、メモリを更新してください。\n\n"+
			"## 作業手順\n\n"+
			"1. `list_memories` ツールで以下の条件のメモリを取得してください\n"+
			"   - タグ: `session-id=%s` かつ `draft=true`\n"+
			"2. ドラフトの会話ログから重要な情報・決定事項・知見を抽出してください\n"+
			"3. 対応するメインメモリを探してください（同じ memory_key タグを持ち `draft` タグのないもの）\n"+
			"   - 存在しない場合は新規作成してください\n"+
			"4. メインメモリを次の方針で更新してください\n"+
			"   - 本日（%s）の日付スナップショットセクションを追加し、抽出した重要情報を記録\n"+
			"   - 重複・陳腐化した内容は削除\n"+
			"   - 将来的に参照価値の高い決定事項・知見を優先して残す\n"+
			"5. ドラフトメモリを `delete_memory` ツールで削除してください\n\n"+
			"ドラフトメモリが見つからない場合はその旨を確認して終了してください。",
		sourceSessionID, sourceSessionID, today,
	)
}

