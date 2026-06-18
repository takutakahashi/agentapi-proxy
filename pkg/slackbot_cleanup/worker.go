package slackbot_cleanup

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// slackbotIDLabelKey is the Kubernetes label key used to identify Slackbot sessions.
	// Sessions created by the Slackbot have this label set to the bot ID.
	slackbotIDLabelKey = "agentapi.proxy/tag-slackbot_id"

	// lastMessageAtAnnotation is the annotation key that stores the RFC3339 timestamp
	// of the last message received for a session.
	lastMessageAtAnnotation = "agentapi.proxy/last-message-at"

	// slackLastMessageAtAnnotation is the legacy annotation key retained for backward
	// compatibility with sessions created before the unified last-message-at was introduced.
	slackLastMessageAtAnnotation = "agentapi.proxy/slack-last-message-at"

	// sessionIDLabel is the label key holding the session ID on Kubernetes Services.
	sessionIDLabel = "agentapi.proxy/session-id"

	// sessionTTLAnnotation stores the per-session TTL as a Go duration string (e.g. "48h").
	// When present, this overrides the global CleanupWorkerConfig.SessionTTL for that session.
	sessionTTLAnnotation = "agentapi.proxy/session-ttl"
)

// CleanupWorkerConfig holds configuration for the Slackbot session cleanup worker.
type CleanupWorkerConfig struct {
	// CheckInterval is how often the worker scans for stale Slackbot sessions.
	// Default: 1h
	CheckInterval time.Duration
	// SessionTTLCheckInterval is how often the worker scans for sessions with an explicit
	// agentapi.proxy/session-ttl annotation. This can be much shorter than CheckInterval
	// to support short-lived sessions (e.g. 1m TTL).
	// Default: 1m
	SessionTTLCheckInterval time.Duration
	// SessionTTL is the duration after the last message before a session is deleted.
	// Default: 72h (3 days)
	SessionTTL time.Duration
	// Enabled controls whether the worker actually runs.
	Enabled bool
	// DryRun disables actual session deletion; stale sessions are only logged.
	// Useful for verifying TTL settings before enabling real cleanup.
	// Default: false
	DryRun bool
}

// DefaultCleanupWorkerConfig returns the default configuration.
func DefaultCleanupWorkerConfig() CleanupWorkerConfig {
	return CleanupWorkerConfig{
		CheckInterval:           1 * time.Hour,
		SessionTTLCheckInterval: 1 * time.Minute,
		SessionTTL:              72 * time.Hour,
		Enabled:                 true,
		DryRun:                  false,
	}
}

// CleanupWorker periodically deletes Slackbot sessions whose last message is older
// than SessionTTL. It uses the agentapi.proxy/last-message-at annotation
// (with fallback to the legacy agentapi.proxy/slack-last-message-at annotation)
// to determine when the last message occurred.
type CleanupWorker struct {
	sessionManager portrepos.SessionManager
	k8sClient      kubernetes.Interface
	namespace      string
	config         CleanupWorkerConfig

	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
	wg      sync.WaitGroup
}

// NewCleanupWorker creates a new CleanupWorker.
func NewCleanupWorker(
	sessionManager portrepos.SessionManager,
	k8sClient kubernetes.Interface,
	namespace string,
	config CleanupWorkerConfig,
) *CleanupWorker {
	return &CleanupWorker{
		sessionManager: sessionManager,
		k8sClient:      k8sClient,
		namespace:      namespace,
		config:         config,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the cleanup worker loop. It is safe to call from multiple goroutines.
func (w *CleanupWorker) Start(ctx context.Context) error {
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

	dryRunNote := ""
	if w.config.DryRun {
		dryRunNote = " (dry-run mode: no sessions will be deleted)"
	}
	log.Printf("[SLACKBOT_CLEANUP] Started with check interval %v, session TTL %v, TTL annotation check interval %v%s",
		w.config.CheckInterval, w.config.SessionTTL, w.config.SessionTTLCheckInterval, dryRunNote)
	return nil
}

// Stop gracefully stops the worker.
func (w *CleanupWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.wg.Wait()
	log.Printf("[SLACKBOT_CLEANUP] Stopped")
}

// run is the main worker loop.
func (w *CleanupWorker) run(ctx context.Context) {
	defer w.wg.Done()

	slackbotTicker := time.NewTicker(w.config.CheckInterval)
	defer slackbotTicker.Stop()

	ttlInterval := w.config.SessionTTLCheckInterval
	if ttlInterval <= 0 {
		ttlInterval = 1 * time.Minute
	}
	ttlTicker := time.NewTicker(ttlInterval)
	defer ttlTicker.Stop()

	// Run immediately on start
	w.pruneStaleSlackbotSessions(ctx)
	w.pruneSessionsWithTTL(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[SLACKBOT_CLEANUP] Context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Printf("[SLACKBOT_CLEANUP] Stop signal received")
			return
		case <-slackbotTicker.C:
			w.pruneStaleSlackbotSessions(ctx)
		case <-ttlTicker.C:
			w.pruneSessionsWithTTL(ctx)
		}
	}
}

// pruneStaleSlackbotSessions lists all Slackbot sessions and deletes those whose
// last message time is older than SessionTTL.  When DryRun is enabled the
// worker only logs which sessions would be deleted without touching them.
// If a session has the agentapi.proxy/session-ttl annotation, that value overrides
// the global SessionTTL for that individual session.
func (w *CleanupWorker) pruneStaleSlackbotSessions(ctx context.Context) {
	now := time.Now()

	dryRunPrefix := ""
	if w.config.DryRun {
		dryRunPrefix = "[DRY-RUN] "
	}

	// List Services that have the slackbot_id label (value-independent existence check).
	// The label selector "agentapi.proxy/tag-slackbot_id" matches any Service that has
	// this label set, regardless of its value.
	labelSelector := "app.kubernetes.io/managed-by=agentapi-proxy," +
		"app.kubernetes.io/name=agentapi-session," +
		slackbotIDLabelKey

	svcList, err := w.k8sClient.CoreV1().Services(w.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("[SLACKBOT_CLEANUP] %sFailed to list Slackbot sessions: %v", dryRunPrefix, err)
		return
	}

	if len(svcList.Items) == 0 {
		return
	}

	log.Printf("[SLACKBOT_CLEANUP] %sScanning %d Slackbot session(s) for TTL expiry",
		dryRunPrefix, len(svcList.Items))

	deleted := 0
	for _, svc := range svcList.Items {
		// Defensive check: verify the service actually carries the slackbot label.
		if svc.Labels[slackbotIDLabelKey] == "" {
			log.Printf("[SLACKBOT_CLEANUP] %sService %s does not have slackbot label, skipping", dryRunPrefix, svc.Name)
			continue
		}

		sessionID := svc.Labels[sessionIDLabel]
		if sessionID == "" {
			log.Printf("[SLACKBOT_CLEANUP] %sService %s missing session-id label, skipping", dryRunPrefix, svc.Name)
			continue
		}

		// Resolve effective TTL: per-session annotation takes priority over global config.
		effectiveTTL := w.config.SessionTTL
		if ttlStr, ok := svc.Annotations[sessionTTLAnnotation]; ok && ttlStr != "" {
			if parsed, err := time.ParseDuration(ttlStr); err == nil {
				effectiveTTL = parsed
			} else {
				log.Printf("[SLACKBOT_CLEANUP] %sSession %s: invalid session-ttl annotation %q, using global TTL", dryRunPrefix, sessionID, ttlStr)
			}
		}
		threshold := now.Add(-effectiveTTL)

		// Determine the reference time for TTL calculation from last-message-at.
		refTime, err := w.resolveReferenceTime(svc.Annotations)
		if err != nil {
			log.Printf("[SLACKBOT_CLEANUP] %sSession %s: cannot determine reference time (%v), skipping", dryRunPrefix, sessionID, err)
			continue
		}

		if refTime.After(threshold) {
			// Session is still within TTL, skip
			continue
		}

		if w.config.DryRun {
			log.Printf("[SLACKBOT_CLEANUP] [DRY-RUN] Would delete session %s (last message at %s, threshold %s, ttl %s)",
				sessionID, refTime.Format(time.RFC3339), threshold.Format(time.RFC3339), effectiveTTL)
			deleted++
			continue
		}

		log.Printf("[SLACKBOT_CLEANUP] Deleting session %s (last message at %s, threshold %s, ttl %s)",
			sessionID, refTime.Format(time.RFC3339), threshold.Format(time.RFC3339), effectiveTTL)

		if err := w.sessionManager.DeleteSession(sessionID); err != nil {
			log.Printf("[SLACKBOT_CLEANUP] Failed to delete session %s: %v", sessionID, err)
		} else {
			log.Printf("[SLACKBOT_CLEANUP] Deleted session %s", sessionID)
			deleted++
		}
	}

	if deleted > 0 {
		if w.config.DryRun {
			log.Printf("[SLACKBOT_CLEANUP] [DRY-RUN] Would delete %d stale Slackbot session(s)", deleted)
		} else {
			log.Printf("[SLACKBOT_CLEANUP] Deleted %d stale Slackbot session(s)", deleted)
		}
	}
}

// pruneSessionsWithTTL scans all agentapi-proxy sessions (regardless of Slackbot label)
// that have the agentapi.proxy/session-ttl annotation set, and deletes those whose
// last-message-at is older than the annotation value.  Slackbot sessions are skipped
// here because they are already handled by pruneStaleSlackbotSessions.
func (w *CleanupWorker) pruneSessionsWithTTL(ctx context.Context) {
	now := time.Now()

	dryRunPrefix := ""
	if w.config.DryRun {
		dryRunPrefix = "[DRY-RUN] "
	}

	labelSelector := "app.kubernetes.io/managed-by=agentapi-proxy," +
		"app.kubernetes.io/name=agentapi-session"

	svcList, err := w.k8sClient.CoreV1().Services(w.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("[SESSION_TTL_CLEANUP] %sFailed to list sessions: %v", dryRunPrefix, err)
		return
	}

	deleted := 0
	for _, svc := range svcList.Items {
		// Skip Slackbot sessions — they are managed by pruneStaleSlackbotSessions.
		if svc.Labels[slackbotIDLabelKey] != "" {
			continue
		}

		ttlStr, ok := svc.Annotations[sessionTTLAnnotation]
		if !ok || ttlStr == "" {
			continue
		}

		ttl, err := time.ParseDuration(ttlStr)
		if err != nil {
			log.Printf("[SESSION_TTL_CLEANUP] %sSession %s: invalid session-ttl %q: %v", dryRunPrefix, svc.Labels[sessionIDLabel], ttlStr, err)
			continue
		}

		sessionID := svc.Labels[sessionIDLabel]
		if sessionID == "" {
			continue
		}

		threshold := now.Add(-ttl)

		refTime, err := w.resolveReferenceTime(svc.Annotations)
		if err != nil {
			log.Printf("[SESSION_TTL_CLEANUP] %sSession %s: cannot determine reference time (%v), skipping", dryRunPrefix, sessionID, err)
			continue
		}

		if refTime.After(threshold) {
			continue
		}

		if w.config.DryRun {
			log.Printf("[SESSION_TTL_CLEANUP] [DRY-RUN] Would delete session %s (last message at %s, threshold %s, ttl %s)",
				sessionID, refTime.Format(time.RFC3339), threshold.Format(time.RFC3339), ttl)
			deleted++
			continue
		}

		log.Printf("[SESSION_TTL_CLEANUP] Deleting session %s (last message at %s, threshold %s, ttl %s)",
			sessionID, refTime.Format(time.RFC3339), threshold.Format(time.RFC3339), ttl)

		if err := w.sessionManager.DeleteSession(sessionID); err != nil {
			log.Printf("[SESSION_TTL_CLEANUP] Failed to delete session %s: %v", sessionID, err)
		} else {
			log.Printf("[SESSION_TTL_CLEANUP] Deleted session %s", sessionID)
			deleted++
		}
	}

	if deleted > 0 {
		if w.config.DryRun {
			log.Printf("[SESSION_TTL_CLEANUP] [DRY-RUN] Would delete %d session(s) with TTL annotation", deleted)
		} else {
			log.Printf("[SESSION_TTL_CLEANUP] Deleted %d session(s) with TTL annotation", deleted)
		}
	}
}

// resolveReferenceTime returns the reference time for TTL calculation.
// It prefers the unified last-message-at annotation and falls back to the
// legacy slack-last-message-at annotation for sessions created before the
// unified annotation was introduced.  Sessions without either annotation
// are skipped to avoid inadvertently deleting unrelated sessions.
func (w *CleanupWorker) resolveReferenceTime(annotations map[string]string) (time.Time, error) {
	for _, key := range []string{lastMessageAtAnnotation, slackLastMessageAtAnnotation} {
		if v, ok := annotations[key]; ok && v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no %s annotation found", lastMessageAtAnnotation)
}

// LeaderCleanupWorker combines leader election with the Slackbot cleanup worker.
// Only the elected leader runs the cleanup loop, preventing duplicate deletions
// in multi-replica deployments.
type LeaderCleanupWorker struct {
	worker  *CleanupWorker
	elector *schedule.LeaderElector
}

// NewLeaderCleanupWorker creates a new LeaderCleanupWorker.
func NewLeaderCleanupWorker(
	sessionManager portrepos.SessionManager,
	k8sClient kubernetes.Interface,
	namespace string,
	workerConfig CleanupWorkerConfig,
	electionConfig schedule.LeaderElectionConfig,
) *LeaderCleanupWorker {
	// Use a distinct lease name so this worker does not compete with the schedule worker.
	electionConfig.LeaseName = "agentapi-slackbot-cleanup-worker"

	worker := NewCleanupWorker(sessionManager, k8sClient, namespace, workerConfig)
	elector := schedule.NewLeaderElector(k8sClient, electionConfig)

	return &LeaderCleanupWorker{
		worker:  worker,
		elector: elector,
	}
}

// Run starts the leader election loop. Only the leader runs the cleanup worker.
func (lw *LeaderCleanupWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx,
		func(leaderCtx context.Context) {
			if err := lw.worker.Start(leaderCtx); err != nil {
				log.Printf("[SLACKBOT_CLEANUP] Failed to start worker: %v", err)
			}
		},
		func() {
			lw.worker.Stop()
		},
	)
}

// Stop gracefully stops the leader cleanup worker.
func (lw *LeaderCleanupWorker) Stop() {
	lw.worker.Stop()
}
