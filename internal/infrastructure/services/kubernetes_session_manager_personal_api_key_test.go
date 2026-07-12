package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// fakePersonalAPIKeyRepo is an in-memory PersonalAPIKeyRepository for tests.
type fakePersonalAPIKeyRepo struct {
	keys map[entities.UserID]*entities.PersonalAPIKey
}

func newFakePersonalAPIKeyRepo() *fakePersonalAPIKeyRepo {
	return &fakePersonalAPIKeyRepo{keys: make(map[entities.UserID]*entities.PersonalAPIKey)}
}

func (r *fakePersonalAPIKeyRepo) FindByUserID(_ context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error) {
	if k, ok := r.keys[userID]; ok {
		return k, nil
	}
	return nil, &notFoundError{}
}

func (r *fakePersonalAPIKeyRepo) Save(_ context.Context, k *entities.PersonalAPIKey) error {
	r.keys[k.UserID()] = k
	return nil
}

func (r *fakePersonalAPIKeyRepo) Delete(_ context.Context, userID entities.UserID) error {
	delete(r.keys, userID)
	return nil
}

func (r *fakePersonalAPIKeyRepo) List(_ context.Context) ([]*entities.PersonalAPIKey, error) {
	out := make([]*entities.PersonalAPIKey, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, k)
	}
	return out, nil
}

// notFoundError satisfies the error interface and is treated as "not found".
type notFoundError struct{}

func (e *notFoundError) Error() string { return "not found" }

// fakePersonalAPIKeyLoader records calls to LoadPersonalAPIKey.
type fakePersonalAPIKeyLoader struct {
	loaded []*entities.PersonalAPIKey
}

func (l *fakePersonalAPIKeyLoader) LoadPersonalAPIKey(_ context.Context, k *entities.PersonalAPIKey) error {
	l.loaded = append(l.loaded, k)
	return nil
}

func newTestManagerForPersonalAPIKey(t *testing.T) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(false),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	manager.namespace = "test-ns"
	return manager
}

// TestBuildSessionSettings_NewPersonalAPIKeyLoaded verifies that when a new
// personal API key is created for a user (first session), it is immediately
// registered via PersonalAPIKeyLoader so that oneshot Stop hooks can
// authenticate their delete-session calls without a proxy restart.
func TestBuildSessionSettings_NewPersonalAPIKeyLoaded(t *testing.T) {
	manager := newTestManagerForPersonalAPIKey(t)

	repo := newFakePersonalAPIKeyRepo()
	loader := &fakePersonalAPIKeyLoader{}

	manager.SetPersonalAPIKeyRepository(repo)
	manager.SetPersonalAPIKeyLoader(loader)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "new-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	req := &entities.RunServerRequest{
		UserID: "new-user",
		Scope:  entities.ScopeUser,
	}

	if _, buildErr := manager.buildSessionSettings(context.Background(), session, req, nil); buildErr != nil {
		t.Fatalf("buildSessionSettings() error = %v", buildErr)
	}

	if len(loader.loaded) == 0 {
		t.Fatal("Expected PersonalAPIKeyLoader.LoadPersonalAPIKey to be called for new user, but it was not")
	}
	if loader.loaded[0].UserID() != "new-user" {
		t.Errorf("Expected loaded key user ID %q, got %q", "new-user", loader.loaded[0].UserID())
	}
	// Key must also be persisted to the repository
	if _, err := repo.FindByUserID(context.Background(), "new-user"); err != nil {
		t.Errorf("Expected personal API key to be saved to repo: %v", err)
	}
}

// TestBuildSessionSettings_ExistingPersonalAPIKeyNotReloaded verifies that when a
// personal API key already exists for a user, the loader is NOT called again
// (no duplicate registration on every session start).
func TestBuildSessionSettings_ExistingPersonalAPIKeyNotReloaded(t *testing.T) {
	manager := newTestManagerForPersonalAPIKey(t)

	repo := newFakePersonalAPIKeyRepo()
	existing := entities.NewPersonalAPIKey("existing-user", "existing-key-abc")
	_ = repo.Save(context.Background(), existing)

	loader := &fakePersonalAPIKeyLoader{}

	manager.SetPersonalAPIKeyRepository(repo)
	manager.SetPersonalAPIKeyLoader(loader)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "existing-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	req := &entities.RunServerRequest{
		UserID: "existing-user",
		Scope:  entities.ScopeUser,
	}

	if _, buildErr := manager.buildSessionSettings(context.Background(), session, req, nil); buildErr != nil {
		t.Fatalf("buildSessionSettings() error = %v", buildErr)
	}

	if len(loader.loaded) != 0 {
		t.Errorf("Expected LoadPersonalAPIKey NOT to be called for existing user, but it was called %d time(s)", len(loader.loaded))
	}
}
