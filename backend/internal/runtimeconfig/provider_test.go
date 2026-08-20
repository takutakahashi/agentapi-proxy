package runtimeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// testStore is a minimal in-memory kvstore.Store used only by these tests.
type testStore struct {
	records map[string]kvstore.Record
}

func newTestStore() *testStore { return &testStore{records: map[string]kvstore.Record{}} }

func keyOf(kind kvstore.Kind, namespace, key string) string {
	return string(kind) + "/" + namespace + "/" + key
}

func (s *testStore) Create(_ context.Context, r kvstore.Record) (kvstore.Record, error) {
	k := keyOf(r.Kind, r.Namespace, r.Key)
	if _, exists := s.records[k]; exists {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	r.Version = 1
	s.records[k] = r
	return r, nil
}
func (s *testStore) Update(_ context.Context, r kvstore.Record) (kvstore.Record, error) {
	k := keyOf(r.Kind, r.Namespace, r.Key)
	existing, exists := s.records[k]
	if !exists {
		return kvstore.Record{}, kvstore.ErrNotFound
	}
	if r.Version != existing.Version {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	r.Version = existing.Version + 1
	s.records[k] = r
	return r, nil
}
func (s *testStore) Get(_ context.Context, kind kvstore.Kind, namespace, key string) (kvstore.Record, error) {
	r, exists := s.records[keyOf(kind, namespace, key)]
	if !exists {
		return kvstore.Record{}, kvstore.ErrNotFound
	}
	return r, nil
}
func (s *testStore) Delete(_ context.Context, kind kvstore.Kind, namespace, key string, _ int64) error {
	delete(s.records, keyOf(kind, namespace, key))
	return nil
}
func (s *testStore) List(_ context.Context, q kvstore.Query) ([]kvstore.Record, error) {
	var out []kvstore.Record
	for _, r := range s.records {
		if r.Kind == q.Kind && r.Namespace == q.Namespace {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *testStore) Close() error { return nil }

func mustSecretValue(t *testing.T, dataKey string, value interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	secret := &corev1.Secret{Data: map[string][]byte{dataKey: raw}}
	out, err := json.Marshal(secret)
	require.NoError(t, err)
	return out
}

func versionKeyFor(v int64) string { return fmt.Sprintf(versionKey, v) }

// TestProviderDoesNotApplyAdminSettingsAtRuntime asserts that admin-managed
// system settings are never overlaid onto the base config at runtime. This is
// the safety guard introduced to prevent the secret-wiping defect where saving
// from the admin "System Settings" screen overwrites OAuth client secrets / bot
// tokens with empty strings and breaks authentication (see session 73a1e4).
func TestProviderDoesNotApplyAdminSettingsAtRuntime(t *testing.T) {
	pvc := false
	base := &config.Config{
		Auth:              config.AuthConfig{GitHub: &config.GitHubAuthConfig{Enabled: true, UserMapping: config.GitHubUserMapping{DefaultRole: "user"}}},
		KubernetesSession: config.KubernetesSessionConfig{Image: "base:image", CPURequest: "100m", PVCEnabled: &pvc},
	}
	provider := New(base, nil, "default")
	sections := map[string]interface{}{
		"authentication": map[string]interface{}{
			"default_role":      "admin",
			"team_role_mapping": map[string]interface{}{"org/team": map[string]interface{}{"role": "admin", "permissions": []interface{}{"session:access"}}},
		},
		"sessions": map[string]interface{}{"image": "runtime:image", "pvc_enabled": true},
		"agents":   map[string]interface{}{"auth_mode": "bedrock", "env_vars": map[string]interface{}{"SYSTEM_DEFAULT": "enabled"}},
		"storage":  map[string]interface{}{"redis_enabled": true, "redis_address": "runtime-redis:6379", "session_persistence_backend": "s3", "session_persistence_bucket": "runtime-bucket"},
	}

	var notified *config.Config
	provider.Subscribe(func(cfg *config.Config) { notified = cfg })
	require.NoError(t, provider.Apply(7, sections))

	current := provider.Current()
	// Version and current config must reflect the base config, not the overlay.
	require.Equal(t, int64(0), provider.Version())
	require.Equal(t, "base:image", current.KubernetesSession.Image)
	require.Equal(t, "100m", current.KubernetesSession.CPURequest)
	require.False(t, *current.KubernetesSession.PVCEnabled)
	require.Equal(t, "user", current.Auth.GitHub.UserMapping.DefaultRole)
	require.Empty(t, current.Auth.GitHub.UserMapping.TeamRoleMapping)
	require.Empty(t, current.Redis.Addr)
	require.Empty(t, current.SessionPersistence.Backend)
	// Subscribers are not notified when runtime application is disabled.
	require.Nil(t, notified)

	// Admin-set agent defaults are not surfaced either.
	defaults := provider.AgentDefaults()
	require.Empty(t, defaults.AuthMode)
	require.Empty(t, defaults.EnvVars)

	// Current returns a defensive copy.
	current.KubernetesSession.Image = "mutated"
	require.Equal(t, "base:image", provider.Current().KubernetesSession.Image)
}

// TestProviderReloadDoesNotApplyStoredDocument asserts that Reload does not
// overlay a versioned admin settings document even when one exists in the KV
// store (e.g. a document that would wipe the GitHub OAuth client secret).
func TestProviderReloadDoesNotApplyStoredDocument(t *testing.T) {
	pvc := false
	base := &config.Config{
		Auth:              config.AuthConfig{GitHub: &config.GitHubAuthConfig{Enabled: true, OAuth: &config.GitHubOAuthConfig{ClientID: "base-id", ClientSecret: "base-secret"}}},
		KubernetesSession: config.KubernetesSessionConfig{Image: "base:image", PVCEnabled: &pvc},
	}
	store := newTestStore()
	sections := map[string]interface{}{
		"github": map[string]interface{}{"oauth": map[string]interface{}{"client_secret": ""}},
	}
	docValue := mustSecretValue(t, dataKey, storedDocument{Version: 3, Sections: sections})
	headValue := mustSecretValue(t, headDataKey, storedHead{CurrentVersion: 3})
	_, err := store.Create(context.Background(), kvstore.Record{Kind: kvstore.KindSecret, Namespace: "default", Key: headKey, Value: headValue})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), kvstore.Record{Kind: kvstore.KindSecret, Namespace: "default", Key: versionKeyFor(3), Value: docValue})
	require.NoError(t, err)

	provider := New(base, store, "default")
	require.NoError(t, provider.Reload(context.Background()))

	current := provider.Current()
	// The base secret is preserved; the stored empty-string override is not applied.
	require.Equal(t, "base-secret", current.Auth.GitHub.OAuth.ClientSecret)
	require.Equal(t, "base:image", current.KubernetesSession.Image)
	require.Equal(t, int64(0), provider.Version())
}

// keep imports used by the test store wiring (runtime is referenced indirectly)
var _ runtime.Object = (*corev1.Secret)(nil)
