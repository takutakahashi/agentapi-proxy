package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	proxyconfig "github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	adminSettingsHeadKey       = "agentapi-admin-system-settings-head"
	adminSettingsVersionPrefix = "agentapi-admin-system-settings-v"
	adminSettingsDataKey       = "settings.json"
	adminSettingsHeadDataKey   = "head.json"
	maxAdminSettingsSize       = 1024 * 1024
)

// AdminSettingsController stores one complete settings.json snapshot per
// revision. Snapshots are immutable; a small CAS-updated head record points at
// the current revision.
type AdminSettingsController struct {
	store     kvstore.Store
	namespace string
	defaults  map[string]interface{}
	provider  *runtimeconfig.Provider
}

func (c *AdminSettingsController) WithRuntimeConfigProvider(provider *runtimeconfig.Provider) *AdminSettingsController {
	c.provider = provider
	return c
}

func (c *AdminSettingsController) runtimeDefaults() map[string]interface{} {
	if c.provider != nil {
		return adminSettingsDefaults(c.provider.Current())
	}
	return c.defaults
}

func NewAdminSettingsController(store kvstore.Store, namespace string, cfg ...*proxyconfig.Config) *AdminSettingsController {
	controller := &AdminSettingsController{store: store, namespace: namespace, defaults: map[string]interface{}{}}
	if len(cfg) > 0 && cfg[0] != nil {
		controller.defaults = adminSettingsDefaults(cfg[0])
	}
	return controller
}

type adminSettingsDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	Version       int64                  `json:"version"`
	Sections      map[string]interface{} `json:"sections"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type adminSettingsUpdate struct {
	BaseVersion int64                  `json:"base_version"`
	Sections    map[string]interface{} `json:"sections"`
}

type adminSettingsHead struct {
	CurrentVersion int64     `json:"current_version"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type adminSettingsResponse struct {
	SchemaVersion    int                    `json:"schema_version"`
	Version          int64                  `json:"version"`
	Sections         map[string]interface{} `json:"sections"`
	SecretConfigured map[string]bool        `json:"secret_configured"`
	UpdatedAt        time.Time              `json:"updated_at,omitempty"`
}

type adminSettingsVersion struct {
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

var adminSecretPaths = []string{
	"github.oauth.client_secret", "github.app.private_key",
	"slack.bot_token", "slack.app_token", "slack.signing_secret",
	"notifications.vapid_private_key", "security.encryption_key",
	"storage.database_url", "storage.database_auth_token", "storage.redis_password",
	"integrations.google_client_secret", "integrations.todoist_client_secret",
}

func (c *AdminSettingsController) Get(ctx echo.Context) error {
	var (
		doc adminSettingsDocument
		err error
	)
	if requested := ctx.QueryParam("version"); requested != "" {
		version, parseErr := strconv.ParseInt(requested, 10, 64)
		if parseErr != nil || version < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "version must be a positive integer")
		}
		doc, _, err = c.loadVersion(ctx.Request().Context(), version)
	} else {
		doc, _, err = c.loadCurrent(ctx.Request().Context())
	}
	if errors.Is(err, kvstore.ErrNotFound) {
		doc = adminSettingsDocument{SchemaVersion: 1, Sections: cloneSections(c.runtimeDefaults())}
		return ctx.JSON(http.StatusOK, sanitizeAdminSettings(doc))
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin settings").SetInternal(err)
	}
	if ctx.QueryParam("version") == "" {
		mergeMissing(doc.Sections, c.runtimeDefaults())
	}
	return ctx.JSON(http.StatusOK, sanitizeAdminSettings(doc))
}

func (c *AdminSettingsController) ListVersions(ctx echo.Context) error {
	records, err := c.store.List(ctx.Request().Context(), kvstore.Query{Kind: kvstore.KindSecret, Namespace: c.namespace})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list admin settings versions").SetInternal(err)
	}
	versions := make([]adminSettingsVersion, 0)
	for _, record := range records {
		if !strings.HasPrefix(record.Key, adminSettingsVersionPrefix) {
			continue
		}
		doc, err := decodeDocument(record)
		if err == nil {
			versions = append(versions, adminSettingsVersion{Version: doc.Version, UpdatedAt: doc.UpdatedAt})
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	return ctx.JSON(http.StatusOK, map[string]interface{}{"versions": versions})
}

func (c *AdminSettingsController) Put(ctx echo.Context) error {
	ctx.Request().Body = http.MaxBytesReader(ctx.Response(), ctx.Request().Body, maxAdminSettingsSize)
	var requested adminSettingsUpdate
	decoder := json.NewDecoder(ctx.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requested); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid admin settings document").SetInternal(err)
	}
	if requested.Sections == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "sections is required")
	}

	current, headRecord, err := c.loadCurrent(ctx.Request().Context())
	if errors.Is(err, kvstore.ErrNotFound) {
		if requested.BaseVersion != 0 {
			return echo.NewHTTPError(http.StatusConflict, "admin settings version is stale")
		}
		current = adminSettingsDocument{Sections: cloneSections(c.runtimeDefaults())}
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load admin settings").SetInternal(err)
	} else if requested.BaseVersion != current.Version {
		return echo.NewHTTPError(http.StatusConflict, "admin settings version is stale")
	}
	mergeMissing(current.Sections, c.runtimeDefaults())

	preserveOmittedSecrets(current.Sections, requested.Sections)
	now := time.Now().UTC()
	doc := adminSettingsDocument{SchemaVersion: 1, Version: current.Version + 1, Sections: requested.Sections, UpdatedAt: now}
	versionValue, err := marshalSecret(versionKey(doc.Version), "admin-system-settings-version", adminSettingsDataKey, doc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode admin settings").SetInternal(err)
	}
	if _, err = c.store.Create(ctx.Request().Context(), kvstore.Record{Kind: kvstore.KindSecret, Namespace: c.namespace, Key: versionKey(doc.Version), Value: versionValue}); err != nil {
		if errors.Is(err, kvstore.ErrConflict) {
			return echo.NewHTTPError(http.StatusConflict, "admin settings version is stale")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save admin settings version").SetInternal(err)
	}

	head := adminSettingsHead{CurrentVersion: doc.Version, UpdatedAt: now}
	headValue, err := marshalSecret(adminSettingsHeadKey, "admin-system-settings-head", adminSettingsHeadDataKey, head)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode admin settings head").SetInternal(err)
	}
	if headRecord.Key == "" {
		_, err = c.store.Create(ctx.Request().Context(), kvstore.Record{Kind: kvstore.KindSecret, Namespace: c.namespace, Key: adminSettingsHeadKey, Value: headValue})
	} else {
		headRecord.Value = headValue
		_, err = c.store.Update(ctx.Request().Context(), headRecord)
	}
	if errors.Is(err, kvstore.ErrConflict) {
		return echo.NewHTTPError(http.StatusConflict, "admin settings were updated by another request")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update admin settings head").SetInternal(err)
	}
	if c.provider != nil {
		if err := c.provider.Apply(doc.Version, doc.Sections); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "settings were saved but could not be applied").SetInternal(err)
		}
	}
	return ctx.JSON(http.StatusOK, sanitizeAdminSettings(doc))
}

func adminSettingsDefaults(cfg *proxyconfig.Config) map[string]interface{} {
	sections := map[string]interface{}{}
	put := func(path string, value interface{}) { setNestedValue(sections, path, value) }

	if cfg.Auth.GitHub != nil {
		put("github.oauth.enabled", cfg.Auth.GitHub.Enabled)
		put("github.enterprise.base_url", cfg.Auth.GitHub.BaseURL)
		put("authentication.allow_users_without_team", cfg.Auth.GitHub.UserMapping.AllowUsersWithoutTeam)
		put("authentication.default_role", cfg.Auth.GitHub.UserMapping.DefaultRole)
		put("authentication.default_permissions", strings.Join(cfg.Auth.GitHub.UserMapping.DefaultPermissions, "\n"))
		put("authentication.team_role_mapping", cfg.Auth.GitHub.UserMapping.TeamRoleMapping)
		if cfg.Auth.GitHub.OAuth != nil {
			put("github.oauth.client_id", cfg.Auth.GitHub.OAuth.ClientID)
			put("github.oauth.client_secret", cfg.Auth.GitHub.OAuth.ClientSecret)
			put("github.oauth.scope", cfg.Auth.GitHub.OAuth.Scope)
		}
	}
	if cfg.Auth.Static != nil {
		put("authentication.static.enabled", cfg.Auth.Static.Enabled)
		put("authentication.static.header_name", cfg.Auth.Static.HeaderName)
	}
	put("slack.cleanup_enabled", cfg.SlackbotCleanupWorker.Enabled)
	put("slack.session_ttl", cfg.SlackbotCleanupWorker.SessionTTL)
	put("slack.cleanup_check_interval", cfg.SlackbotCleanupWorker.CheckInterval)
	put("slack.cleanup_dry_run", cfg.SlackbotCleanupWorker.DryRun)
	put("notifications.webhook_base_url", cfg.Webhook.BaseURL)
	put("notifications.github_enterprise_host", cfg.Webhook.GitHubEnterpriseHost)
	put("workers.schedule.enabled", cfg.ScheduleWorker.Enabled)
	put("workers.schedule.check_interval", cfg.ScheduleWorker.CheckInterval)
	put("workers.stock.enabled", cfg.StockInventoryWorker.Enabled)
	put("workers.stock.check_interval", cfg.StockInventoryWorker.CheckInterval)
	put("workers.stock.target_count", cfg.StockInventoryWorker.TargetCount)
	put("workers.stock.docker_enabled", cfg.StockInventoryWorker.DockerEnabled)
	put("sessions.image", cfg.KubernetesSession.Image)
	put("sessions.cpu_request", cfg.KubernetesSession.CPURequest)
	put("sessions.cpu_limit", cfg.KubernetesSession.CPULimit)
	put("sessions.memory_request", cfg.KubernetesSession.MemoryRequest)
	put("sessions.memory_limit", cfg.KubernetesSession.MemoryLimit)
	if cfg.KubernetesSession.PVCEnabled != nil {
		put("sessions.pvc_enabled", *cfg.KubernetesSession.PVCEnabled)
	}
	put("sessions.pvc_storage_class", cfg.KubernetesSession.PVCStorageClass)
	put("sessions.pvc_size", cfg.KubernetesSession.PVCStorageSize)
	put("sessions.pod_start_timeout", cfg.KubernetesSession.PodStartTimeout)
	put("sessions.pod_stop_timeout", cfg.KubernetesSession.PodStopTimeout)
	put("sessions.otel_enabled", cfg.KubernetesSession.OtelCollectorEnabled)
	put("security.network_filter_image", cfg.KubernetesSession.NetworkFilterImage)
	put("storage.backend", cfg.KVStore.Backend)
	put("storage.database_url", cfg.KVStore.DatabaseURL)
	put("storage.database_auth_token", cfg.KVStore.AuthToken)
	put("storage.usage_enabled", cfg.Usage.Enabled)
	put("storage.redis_enabled", cfg.Redis.Addr != "")
	put("storage.redis_address", cfg.Redis.Addr)
	put("storage.redis_password", cfg.Redis.Password)
	put("storage.redis_tls_enabled", cfg.Redis.TLSEnabled)
	put("storage.session_persistence_backend", cfg.SessionPersistence.Backend)
	if cfg.SessionPersistence.S3 != nil {
		put("storage.session_persistence_bucket", cfg.SessionPersistence.S3.Bucket)
	}
	put("integrations.scia_enabled", cfg.Scia.Enabled)
	put("integrations.todoist_enabled", cfg.Scia.TodoistCredential != "")

	for path, envName := range map[string]string{
		"notifications.base_url": "NOTIFICATION_BASE_URL", "notifications.vapid_public_key": "VAPID_PUBLIC_KEY",
		"notifications.vapid_private_key": "VAPID_PRIVATE_KEY", "notifications.vapid_contact_email": "VAPID_CONTACT_EMAIL",
		"sessions.claude_args": "CLAUDE_ARGS", "github.oauth.allowed_redirect_uris": "OAUTH_ALLOWED_REDIRECT_URIS",
		"github.app.id": "GITHUB_APP_ID", "github.app.installation_id": "GITHUB_INSTALLATION_ID",
		"github.app.private_key": "GITHUB_APP_PEM", "slack.bot_token": "SLACK_BOT_TOKEN",
		"slack.app_token": "SLACK_APP_TOKEN", "slack.signing_secret": "SLACK_SIGNING_SECRET",
	} {
		if value, ok := os.LookupEnv(envName); ok {
			put(path, value)
		}
	}
	if value, ok := os.LookupEnv("SESSION_CONTROL_LONG_POLL_ENABLED"); ok {
		if enabled, err := strconv.ParseBool(value); err == nil {
			put("security.session_control_enabled", enabled)
		}
	}
	return sections
}

func cloneSections(source map[string]interface{}) map[string]interface{} {
	data, _ := json.Marshal(source)
	cloned := map[string]interface{}{}
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func mergeMissing(target, defaults map[string]interface{}) {
	for key, value := range defaults {
		defaultMap, nested := value.(map[string]interface{})
		if nested {
			targetMap, ok := target[key].(map[string]interface{})
			if !ok {
				targetMap = map[string]interface{}{}
				target[key] = targetMap
			}
			mergeMissing(targetMap, defaultMap)
		} else if _, exists := target[key]; !exists {
			target[key] = value
		}
	}
}

func setNestedValue(root map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func (c *AdminSettingsController) loadCurrent(ctx context.Context) (adminSettingsDocument, kvstore.Record, error) {
	headRecord, err := c.store.Get(ctx, kvstore.KindSecret, c.namespace, adminSettingsHeadKey)
	if err != nil {
		return adminSettingsDocument{}, kvstore.Record{}, err
	}
	var head adminSettingsHead
	if err := decodeSecretData(headRecord, adminSettingsHeadDataKey, &head); err != nil {
		return adminSettingsDocument{}, headRecord, err
	}
	doc, _, err := c.loadVersion(ctx, head.CurrentVersion)
	return doc, headRecord, err
}

func (c *AdminSettingsController) loadVersion(ctx context.Context, version int64) (adminSettingsDocument, kvstore.Record, error) {
	record, err := c.store.Get(ctx, kvstore.KindSecret, c.namespace, versionKey(version))
	if err != nil {
		return adminSettingsDocument{}, kvstore.Record{}, err
	}
	doc, err := decodeDocument(record)
	return doc, record, err
}

func decodeDocument(record kvstore.Record) (adminSettingsDocument, error) {
	var doc adminSettingsDocument
	if err := decodeSecretData(record, adminSettingsDataKey, &doc); err != nil {
		return doc, err
	}
	if doc.Sections == nil {
		doc.Sections = map[string]interface{}{}
	}
	return doc, nil
}

func decodeSecretData(record kvstore.Record, key string, target interface{}) error {
	var secret corev1.Secret
	if err := json.Unmarshal(record.Value, &secret); err != nil {
		return fmt.Errorf("decode KV secret: %w", err)
	}
	if err := json.Unmarshal(secret.Data[key], target); err != nil {
		return fmt.Errorf("decode KV document: %w", err)
	}
	return nil
}

func marshalSecret(name, recordType, dataKey string, value interface{}) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"agentapi.proxy/type": recordType}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{dataKey: data}}
	return json.Marshal(secret)
}

func versionKey(version int64) string {
	return fmt.Sprintf("%s%010d", adminSettingsVersionPrefix, version)
}

func sanitizeAdminSettings(doc adminSettingsDocument) adminSettingsResponse {
	copyBytes, _ := json.Marshal(doc.Sections)
	sections := map[string]interface{}{}
	_ = json.Unmarshal(copyBytes, &sections)
	configured := map[string]bool{}
	for _, path := range adminSecretPaths {
		if value, ok := getNestedString(sections, path); ok && value != "" {
			configured[path] = true
			deleteNested(sections, path)
		}
	}
	return adminSettingsResponse{SchemaVersion: doc.SchemaVersion, Version: doc.Version, Sections: sections, SecretConfigured: configured, UpdatedAt: doc.UpdatedAt}
}

func preserveOmittedSecrets(existing, requested map[string]interface{}) {
	for _, path := range adminSecretPaths {
		if _, present := getNestedString(requested, path); present {
			continue
		}
		if value, ok := getNestedString(existing, path); ok {
			setNested(requested, path, value)
		}
	}
}

func getNestedString(root map[string]interface{}, path string) (string, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = root
	for _, part := range parts {
		object, ok := current.(map[string]interface{})
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	value, ok := current.(string)
	return value, ok
}

func setNested(root map[string]interface{}, path, value string) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func deleteNested(root map[string]interface{}, path string) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return
		}
		current = next
	}
	delete(current, parts[len(parts)-1])
}
