package slackbot_cleanup

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// mockSessionManager is a minimal SessionManager for testing.
type mockSessionManager struct {
	deletedIDs []string
	deleteErr  error
}

func (m *mockSessionManager) CreateSession(_ context.Context, id string, _ *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	return nil, nil
}
func (m *mockSessionManager) GetSession(_ string) entities.Session                     { return nil }
func (m *mockSessionManager) ListSessions(_ entities.SessionFilter) []entities.Session { return nil }
func (m *mockSessionManager) SendMessage(_ context.Context, _ string, _ string) error  { return nil }
func (m *mockSessionManager) StopAgent(_ context.Context, _ string) error              { return nil }
func (m *mockSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *mockSessionManager) UpdateSlackLastMessageAt(_ string, _ time.Time) error { return nil }
func (m *mockSessionManager) Shutdown(_ time.Duration) error                       { return nil }
func (m *mockSessionManager) DeleteSession(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedIDs = append(m.deletedIDs, id)
	return nil
}

// helpers

func slackbotService(name, sessionID, botID, lastMessageAt string) corev1.Service {
	annotations := map[string]string{}
	if lastMessageAt != "" {
		annotations[slackLastMessageAtAnnotation] = lastMessageAt
	}
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/name":       "agentapi-session",
				slackbotIDLabelKey:             botID,
				sessionIDLabel:                 sessionID,
			},
			Annotations: annotations,
		},
	}
}

func nonSlackbotService(name, sessionID string) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/name":       "agentapi-session",
				sessionIDLabel:                 sessionID,
				// NOTE: no slackbotIDLabelKey
			},
			Annotations: map[string]string{
				"agentapi.proxy/created-at": time.Now().Add(-100 * time.Hour).Format(time.RFC3339),
			},
		},
	}
}

func newTestWorker(mgr portrepos.SessionManager, svcs ...corev1.Service) *CleanupWorker {
	runtimeObjects := make([]k8sruntime.Object, len(svcs))
	for i := range svcs {
		svc := svcs[i]
		runtimeObjects[i] = &svc
	}
	k8s := fake.NewSimpleClientset(runtimeObjects...)
	return NewCleanupWorker(mgr, k8s, "test", CleanupWorkerConfig{
		CheckInterval: time.Hour,
		SessionTTL:    72 * time.Hour,
	})
}

// TestPruneStaleSlackbotSessions_OnlySlackbotSessionsDeleted verifies that
// the cleanup worker only deletes slackbot sessions (those with the
// agentapi.proxy/tag-slackbot_id label).  Non-slackbot sessions in the same
// namespace must never be touched, even when they are older than the TTL.
func TestPruneStaleSlackbotSessions_OnlySlackbotSessionsDeleted(t *testing.T) {
	staleAt := time.Now().Add(-100 * time.Hour).Format(time.RFC3339)

	slackSvc := slackbotService("slack-svc", "slack-session-1", "bot-123", staleAt)
	nonSlackSvc := nonSlackbotService("non-slack-svc", "non-slack-session-1")

	mgr := &mockSessionManager{}
	w := newTestWorker(mgr, slackSvc, nonSlackSvc)

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 1 {
		t.Fatalf("expected 1 deletion, got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	if mgr.deletedIDs[0] != "slack-session-1" {
		t.Errorf("expected slack-session-1 to be deleted, got %s", mgr.deletedIDs[0])
	}
}

// TestPruneStaleSlackbotSessions_WithinTTL verifies that slackbot sessions
// whose last message is within the TTL window are NOT deleted.
func TestPruneStaleSlackbotSessions_WithinTTL(t *testing.T) {
	recentAt := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	svc := slackbotService("slack-svc", "slack-session-fresh", "bot-123", recentAt)
	mgr := &mockSessionManager{}
	w := newTestWorker(mgr, svc)

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("expected no deletions for fresh session, got %v", mgr.deletedIDs)
	}
}

// TestPruneStaleSlackbotSessions_StaleSession verifies that a slackbot session
// whose last message is older than the TTL IS deleted.
func TestPruneStaleSlackbotSessions_StaleSession(t *testing.T) {
	staleAt := time.Now().Add(-100 * time.Hour).Format(time.RFC3339)

	svc := slackbotService("slack-svc", "slack-session-stale", "bot-123", staleAt)
	mgr := &mockSessionManager{}
	w := newTestWorker(mgr, svc)

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 1 || mgr.deletedIDs[0] != "slack-session-stale" {
		t.Errorf("expected slack-session-stale to be deleted, got %v", mgr.deletedIDs)
	}
}

// TestPruneStaleSlackbotSessions_MissingLastMessageAt verifies that a slackbot
// session without the slack-last-message-at annotation is SKIPPED (not deleted),
// even though it has the slackbot label.
func TestPruneStaleSlackbotSessions_MissingLastMessageAt(t *testing.T) {
	// lastMessageAt = "" means the annotation is not set
	svc := slackbotService("slack-svc", "slack-session-no-ts", "bot-123", "")
	mgr := &mockSessionManager{}
	w := newTestWorker(mgr, svc)

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("expected session without slack-last-message-at to be skipped, got deletions: %v", mgr.deletedIDs)
	}
}

// TestPruneStaleSlackbotSessions_DefensiveCheck verifies that a service
// without the slackbot label is skipped even if it is returned by the
// Kubernetes client (defensive in-loop guard).
func TestPruneStaleSlackbotSessions_DefensiveCheck(t *testing.T) {
	staleAt := time.Now().Add(-100 * time.Hour).Format(time.RFC3339)

	// Manually create a service that looks like a slackbot session but without
	// the slackbot label – simulates a label-selector failure.
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sneaky-svc",
			Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/name":       "agentapi-session",
				sessionIDLabel:                 "sneaky-session",
				// slackbotIDLabelKey deliberately absent
			},
			Annotations: map[string]string{
				slackLastMessageAtAnnotation: staleAt,
			},
		},
	}

	mgr := &mockSessionManager{}
	// Feed the service directly to the fake client so it bypasses the label selector.
	k8s := fake.NewSimpleClientset(&svc)
	w := NewCleanupWorker(mgr, k8s, "test", CleanupWorkerConfig{
		CheckInterval: time.Hour,
		SessionTTL:    72 * time.Hour,
	})

	// Call pruneStaleSlackbotSessions directly; the label selector won't filter
	// the service because we use the fake client, so the defensive check must.
	// (The fake client ignores label selectors and returns all objects.)
	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("expected sneaky (non-slackbot) session to be skipped by defensive check, got deletions: %v", mgr.deletedIDs)
	}
}

// TestPruneStaleSlackbotSessions_DryRun verifies that dry-run mode logs stale
// sessions but does NOT call DeleteSession.
func TestPruneStaleSlackbotSessions_DryRun(t *testing.T) {
	staleAt := time.Now().Add(-100 * time.Hour).Format(time.RFC3339)

	svc := slackbotService("slack-svc", "slack-session-dry", "bot-123", staleAt)
	mgr := &mockSessionManager{}

	runtimeObjects := []k8sruntime.Object{&svc}
	k8s := fake.NewSimpleClientset(runtimeObjects...)
	w := NewCleanupWorker(mgr, k8s, "test", CleanupWorkerConfig{
		CheckInterval: time.Hour,
		SessionTTL:    72 * time.Hour,
		DryRun:        true,
	})

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("dry-run must not delete sessions, got deletions: %v", mgr.deletedIDs)
	}
}

// TestPruneStaleSlackbotSessions_DryRun_FreshSession verifies that dry-run mode
// does NOT count sessions that are within TTL.
func TestPruneStaleSlackbotSessions_DryRun_FreshSession(t *testing.T) {
	recentAt := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	svc := slackbotService("slack-svc", "slack-session-fresh", "bot-123", recentAt)
	mgr := &mockSessionManager{}

	runtimeObjects := []k8sruntime.Object{&svc}
	k8s := fake.NewSimpleClientset(runtimeObjects...)
	w := NewCleanupWorker(mgr, k8s, "test", CleanupWorkerConfig{
		CheckInterval: time.Hour,
		SessionTTL:    72 * time.Hour,
		DryRun:        true,
	})

	w.pruneStaleSlackbotSessions(context.Background())

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("dry-run must not delete sessions, got deletions: %v", mgr.deletedIDs)
	}
}

// TestResolveReferenceTime verifies the annotation-based time resolution.
func TestResolveReferenceTime(t *testing.T) {
	w := &CleanupWorker{}
	now := time.Now().Truncate(time.Second).UTC()

	t.Run("valid annotation", func(t *testing.T) {
		ann := map[string]string{
			slackLastMessageAtAnnotation: now.Format(time.RFC3339),
		}
		got, err := w.resolveReferenceTime(ann)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Equal(now) {
			t.Errorf("expected %v, got %v", now, got)
		}
	})

	t.Run("missing annotation returns error", func(t *testing.T) {
		_, err := w.resolveReferenceTime(map[string]string{})
		if err == nil {
			t.Error("expected error for missing annotation, got nil")
		}
	})

	t.Run("created-at annotation alone does NOT satisfy requirement", func(t *testing.T) {
		ann := map[string]string{
			"agentapi.proxy/created-at": now.Format(time.RFC3339),
		}
		_, err := w.resolveReferenceTime(ann)
		if err == nil {
			t.Error("expected error when only created-at is present, got nil")
		}
	})
}
