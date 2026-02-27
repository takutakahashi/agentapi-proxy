package slackbot_cleanup

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// slackbotIDLabelKey is the Kubernetes label key used to identify Slackbot sessions.
	// Sessions created by the Slackbot have this label set to the bot ID.
	slackbotIDLabelKey = "agentapi.proxy/tag-slackbot_id"

	// slackLastMessageAtAnnotation is the internal annotation key that stores the
	// RFC3339 timestamp of the last message received for a Slackbot session.
	// Set at session creation and updated on each follow-up Slack message.
	slackLastMessageAtAnnotation = "agentapi.proxy/slack-last-message-at"

	// sessionIDLabel is the label key holding the session ID on Kubernetes Services.
	sessionIDLabel = "agentapi.proxy/session-id"
)

// CleanupWorkerConfig holds configuration for the Slackbot session cleanup worker.
type CleanupWorkerConfig struct {
	// CheckInterval is how often the worker scans for stale sessions.
	// Default: 1h
	CheckInterval time.Duration
	// SessionTTL is the duration after the last message before a session is deleted.
	// Default: 72h (3 days)
	SessionTTL time.Duration
	// Enabled controls whether the worker actually runs.
	Enabled bool
}

// DefaultCleanupWorkerConfig returns the default configuration.
func DefaultCleanupWorkerConfig() CleanupWorkerConfig {
	return CleanupWorkerConfig{
		CheckInterval: 1 * time.Hour,
		SessionTTL:    72 * time.Hour,
		Enabled:       true,
	}
}

// CleanupWorker periodically deletes Slackbot sessions whose last message is older
// than SessionTTL. It uses the agentapi.proxy/slack-last-message-at annotation
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

	log.Printf("[SLACKBOT_CLEANUP] Started with check interval %v, session TTL %v",
		w.config.CheckInterval, w.config.SessionTTL)
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

	ticker := time.NewTicker(w.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start
	w.pruneStaleSlackbotSessions(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[SLACKBOT_CLEANUP] Context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Printf("[SLACKBOT_CLEANUP] Stop signal received")
			return
		case <-ticker.C:
			w.pruneStaleSlackbotSessions(ctx)
		}
	}
}

// pruneStaleSlackbotSessions lists all Slackbot sessions and deletes those whose
// last message time (or creation time as fallback) is older than SessionTTL.
func (w *CleanupWorker) pruneStaleSlackbotSessions(ctx context.Context) {
	now := time.Now()
	threshold := now.Add(-w.config.SessionTTL)

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
		log.Printf("[SLACKBOT_CLEANUP] Failed to list Slackbot sessions: %v", err)
		return
	}

	if len(svcList.Items) == 0 {
		return
	}

	log.Printf("[SLACKBOT_CLEANUP] Scanning %d Slackbot session(s) for TTL expiry (threshold: %s)",
		len(svcList.Items), threshold.Format(time.RFC3339))

	deleted := 0
	for _, svc := range svcList.Items {
		// Defensive check: verify the service actually carries the slackbot label.
		// The label selector already filters by this key, but we re-check here to
		// guard against any unexpected label selector behaviour.
		if svc.Labels[slackbotIDLabelKey] == "" {
			log.Printf("[SLACKBOT_CLEANUP] Service %s does not have slackbot label, skipping", svc.Name)
			continue
		}

		sessionID := svc.Labels[sessionIDLabel]
		if sessionID == "" {
			log.Printf("[SLACKBOT_CLEANUP] Service %s missing session-id label, skipping", svc.Name)
			continue
		}

		// Determine the reference time for TTL calculation from slack-last-message-at.
		refTime, err := w.resolveReferenceTime(svc.Annotations)
		if err != nil {
			log.Printf("[SLACKBOT_CLEANUP] Session %s: cannot determine reference time (%v), skipping", sessionID, err)
			continue
		}

		if refTime.After(threshold) {
			// Session is still within TTL, skip
			continue
		}

		log.Printf("[SLACKBOT_CLEANUP] Deleting session %s (last message at %s, threshold %s)",
			sessionID, refTime.Format(time.RFC3339), threshold.Format(time.RFC3339))

		if err := w.sessionManager.DeleteSession(sessionID); err != nil {
			log.Printf("[SLACKBOT_CLEANUP] Failed to delete session %s: %v", sessionID, err)
		} else {
			log.Printf("[SLACKBOT_CLEANUP] Deleted session %s", sessionID)
			deleted++
		}
	}

	if deleted > 0 {
		log.Printf("[SLACKBOT_CLEANUP] Deleted %d stale Slackbot session(s)", deleted)
	}
}

// resolveReferenceTime returns the reference time for TTL calculation.
// It uses slack-last-message-at exclusively so that only Slackbot-specific
// timing data drives deletion decisions.  Sessions without this annotation
// (e.g. created before the feature was introduced) are skipped rather than
// deleted based on the generic created-at value, which could inadvertently
// affect non-Slackbot sessions.
func (w *CleanupWorker) resolveReferenceTime(annotations map[string]string) (time.Time, error) {
	if v, ok := annotations[slackLastMessageAtAnnotation]; ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no %s annotation found", slackLastMessageAtAnnotation)
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
