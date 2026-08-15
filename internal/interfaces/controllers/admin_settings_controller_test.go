package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	proxyconfig "github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes/fake"
)

func newAdminSettingsTestController() (*AdminSettingsController, kvstore.Store) {
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	return NewAdminSettingsController(store, "default"), store
}

func TestAdminSettingsControllerAutofillsRuntimeConfig(t *testing.T) {
	t.Setenv("NOTIFICATION_BASE_URL", "https://helm.example")
	t.Setenv("SESSION_CONTROL_LONG_POLL_ENABLED", "false")
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	pvcEnabled := true
	cfg := &proxyconfig.Config{
		Auth: proxyconfig.AuthConfig{
			Static: &proxyconfig.StaticAuthConfig{Enabled: true, HeaderName: "X-Admin-Key"},
			GitHub: &proxyconfig.GitHubAuthConfig{Enabled: true, OAuth: &proxyconfig.GitHubOAuthConfig{ClientID: "helm-client", ClientSecret: "helm-secret", Scope: "read:user"}, UserMapping: proxyconfig.GitHubUserMapping{TeamRoleMapping: map[string]proxyconfig.TeamRoleRule{"example/admins": {Role: "admin", Permissions: []string{"session:access"}}}}},
		},
		KubernetesSession: proxyconfig.KubernetesSessionConfig{Image: "session:helm", CPURequest: "500m", PVCEnabled: &pvcEnabled},
		KVStore:           proxyconfig.KVStoreConfig{Backend: "libsql", DatabaseURL: "libsql://helm", AuthToken: "db-secret"},
		Redis:             proxyconfig.RedisConfig{Addr: "redis:6379"},
	}
	controller := NewAdminSettingsController(store, "default", cfg)
	e := echo.New()
	ctx, recorder := adminSettingsRequest(t, e, http.MethodGet, "/admin/system-settings", "")
	require.NoError(t, controller.Get(ctx))

	var response adminSettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, int64(0), response.Version)
	clientID, ok := getNestedString(response.Sections, "github.oauth.client_id")
	require.True(t, ok)
	require.Equal(t, "helm-client", clientID)
	require.Equal(t, "session:helm", response.Sections["sessions"].(map[string]interface{})["image"])
	require.Equal(t, "https://helm.example", response.Sections["notifications"].(map[string]interface{})["base_url"])
	require.Equal(t, false, response.Sections["security"].(map[string]interface{})["session_control_enabled"])
	mapping := response.Sections["authentication"].(map[string]interface{})["team_role_mapping"].(map[string]interface{})
	rule := mapping["example/admins"].(map[string]interface{})
	require.Equal(t, "admin", rule["role"])
	require.Equal(t, []interface{}{"session:access"}, rule["permissions"])
	require.True(t, response.SecretConfigured["github.oauth.client_secret"])
	require.True(t, response.SecretConfigured["storage.database_auth_token"])
	_, exposed := getNestedString(response.Sections, "github.oauth.client_secret")
	require.False(t, exposed)
}

func TestAdminSettingsControllerKVValuesOverrideRuntimeDefaults(t *testing.T) {
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	cfg := &proxyconfig.Config{Auth: proxyconfig.AuthConfig{GitHub: &proxyconfig.GitHubAuthConfig{OAuth: &proxyconfig.GitHubOAuthConfig{ClientID: "helm-client", ClientSecret: "helm-secret"}}}}
	controller := NewAdminSettingsController(store, "default", cfg)
	e := echo.New()

	ctx, recorder := adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":0,"sections":{"github":{"oauth":{"client_id":"kv-client"}}}}`)
	require.NoError(t, controller.Put(ctx))
	var saved adminSettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &saved))
	clientID, ok := getNestedString(saved.Sections, "github.oauth.client_id")
	require.True(t, ok)
	require.Equal(t, "kv-client", clientID)
	require.True(t, saved.SecretConfigured["github.oauth.client_secret"])
}

func TestAdminSettingsControllerAppliesSavedVersionToRuntimeProvider(t *testing.T) {
	store := kvstore.NewKubernetesStore(fake.NewSimpleClientset())
	base := &proxyconfig.Config{KubernetesSession: proxyconfig.KubernetesSessionConfig{Image: "helm:image"}}
	provider := runtimeconfig.New(base, store, "default")
	controller := NewAdminSettingsController(store, "default", base).WithRuntimeConfigProvider(provider)
	e := echo.New()

	ctx, recorder := adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":0,"sections":{"sessions":{"image":"kv:image"},"agents":{"auth_mode":"bedrock"}}}`)
	require.NoError(t, controller.Put(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	// The document is persisted to the KV store...
	saved, _, err := controller.loadVersion(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	// ...but runtime application of admin settings is disabled, so the provider
	// keeps reflecting the immutable base config and does not expose admin-set
	// agent defaults. See session 73a1e4 for the rationale.
	require.Equal(t, int64(0), provider.Version())
	require.Equal(t, "helm:image", provider.Current().KubernetesSession.Image)
	require.Empty(t, provider.AgentDefaults().AuthMode)
}

func adminSettingsRequest(t *testing.T, e *echo.Echo, method, target, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	return e.NewContext(req, recorder), recorder
}

func TestAdminSettingsControllerVersionsAndMasksSecrets(t *testing.T) {
	controller, store := newAdminSettingsTestController()
	e := echo.New()

	ctx, recorder := adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":0,"sections":{"github":{"oauth":{"client_id":"client","client_secret":"secret"}},"slack":{"session_ttl":"72h"}}}`)
	require.NoError(t, controller.Put(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	var first adminSettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &first))
	require.Equal(t, int64(1), first.Version)
	require.True(t, first.SecretConfigured["github.oauth.client_secret"])
	_, exposed := getNestedString(first.Sections, "github.oauth.client_secret")
	require.False(t, exposed)

	ctx, recorder = adminSettingsRequest(t, e, http.MethodPut, "/admin/system-settings", `{"base_version":1,"sections":{"github":{"oauth":{"client_id":"changed"}},"slack":{"session_ttl":"48h"}}}`)
	require.NoError(t, controller.Put(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)

	oldDoc, _, err := controller.loadVersion(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "client", oldDoc.Sections["github"].(map[string]interface{})["oauth"].(map[string]interface{})["client_id"])
	newDoc, _, err := controller.loadVersion(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, "changed", newDoc.Sections["github"].(map[string]interface{})["oauth"].(map[string]interface{})["client_id"])
	secret, ok := getNestedString(newDoc.Sections, "github.oauth.client_secret")
	require.True(t, ok)
	require.Equal(t, "secret", secret)

	records, err := store.List(context.Background(), kvstore.Query{Kind: kvstore.KindSecret, Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, records, 3) // head plus two immutable settings.json snapshots
}

func TestAdminSettingsControllerRejectsStaleVersion(t *testing.T) {
	controller, _ := newAdminSettingsTestController()
	e := echo.New()
	ctx, _ := adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":0,"sections":{"github":{}}}`)
	require.NoError(t, controller.Put(ctx))

	ctx, _ = adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":0,"sections":{"github":{}}}`)
	err := controller.Put(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestAdminSettingsControllerListsVersionsNewestFirst(t *testing.T) {
	controller, _ := newAdminSettingsTestController()
	e := echo.New()
	for version := 0; version < 2; version++ {
		ctx, _ := adminSettingsRequest(t, e, http.MethodPut, "/", `{"base_version":`+strconv.Itoa(version)+`,"sections":{"workers":{}}}`)
		require.NoError(t, controller.Put(ctx))
	}
	ctx, recorder := adminSettingsRequest(t, e, http.MethodGet, "/admin/system-settings/versions", "")
	require.NoError(t, controller.ListVersions(ctx))
	var response struct {
		Versions []adminSettingsVersion `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Versions, 2)
	require.Equal(t, int64(2), response.Versions[0].Version)
}
