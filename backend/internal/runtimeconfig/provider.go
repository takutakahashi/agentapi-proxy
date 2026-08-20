package runtimeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
	corev1 "k8s.io/api/core/v1"
)

const (
	headKey     = "agentapi-admin-system-settings-head"
	versionKey  = "agentapi-admin-system-settings-v%010d"
	headDataKey = "head.json"
	dataKey     = "settings.json"
)

// Provider is the single runtime source of truth for system configuration.
// The immutable Helm/environment configuration is the base layer and the latest
// versioned KV document is overlaid on top.
type Provider struct {
	mu        sync.RWMutex
	base      *config.Config
	current   *config.Config
	sections  map[string]interface{}
	version   int64
	store     kvstore.Store
	ns        string
	listeners []func(*config.Config)
}

type storedHead struct {
	CurrentVersion int64 `json:"current_version"`
}

type storedDocument struct {
	Version  int64                  `json:"version"`
	Sections map[string]interface{} `json:"sections"`
}

func New(base *config.Config, store kvstore.Store, namespace string) *Provider {
	baseCopy := cloneConfig(base)
	return &Provider{base: baseCopy, current: cloneConfig(baseCopy), sections: map[string]interface{}{}, store: store, ns: namespace}
}

func (p *Provider) Current() *config.Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneConfig(p.current)
}

func (p *Provider) Version() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

func (p *Provider) Sections() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneMap(p.sections)
}

func (p *Provider) String(path string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var current interface{} = p.sections
	for _, part := range splitPath(path) {
		object, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current, ok = object[part]
		if !ok {
			return ""
		}
	}
	value, _ := current.(string)
	return value
}

func (p *Provider) AgentDefaults() settingspatch.SettingsPatch {
	p.mu.RLock()
	agents := p.sections["agents"]
	p.mu.RUnlock()
	var patch settingspatch.SettingsPatch
	data, _ := json.Marshal(agents)
	_ = json.Unmarshal(data, &patch)
	return patch
}

func (p *Provider) Reload(ctx context.Context) error {
	// Runtime application of admin-managed system settings is intentionally
	// disabled. The admin "System Settings" screen can still persist versioned
	// documents to the KV store, but those settings are no longer overlaid onto
	// the base (Helm/environment) configuration at runtime. This prevents the
	// secret-wiping defect where saving from the admin screen overwrites OAuth
	// client secrets / bot tokens with empty strings and breaks authentication
	// (see investigation session 73a1e4). The runtime config therefore always
	// reflects the immutable base config. Re-enable this overlay only after
	// preserveOmittedSecrets treats empty strings as "unset".
	return nil
}

func (p *Provider) Start(ctx context.Context, interval time.Duration, onError func(error)) {
	// Runtime application of admin settings is disabled (see Reload), so there
	// is nothing to periodically refresh.
	_ = ctx
	_ = interval
	_ = onError
}

var _ = []any{applySections, stringList, splitLines, decodeSecret}

func (p *Provider) Apply(version int64, sections map[string]interface{}) error {
	// Runtime application of admin-managed system settings is disabled (see
	// Reload for the rationale). Accept the call so the admin controller can still
	// persist settings to the KV store without mutating the running config.
	return nil
}

func (p *Provider) Subscribe(listener func(*config.Config)) {
	if listener == nil {
		return
	}
	p.mu.Lock()
	p.listeners = append(p.listeners, listener)
	p.mu.Unlock()
}

func applySections(cfg *config.Config, sections map[string]interface{}) error {
	data, err := json.Marshal(sections)
	if err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	decode := func(section string, target interface{}) {
		if raw := root[section]; len(raw) > 0 {
			_ = json.Unmarshal(raw, target)
		}
	}

	var authentication struct {
		Static                *config.StaticAuthConfig       `json:"static"`
		AllowUsersWithoutTeam *bool                          `json:"allow_users_without_team"`
		DefaultRole           *string                        `json:"default_role"`
		DefaultPermissions    interface{}                    `json:"default_permissions"`
		TeamRoleMapping       map[string]config.TeamRoleRule `json:"team_role_mapping"`
	}
	decode("authentication", &authentication)
	if authentication.Static != nil {
		cfg.Auth.Static = authentication.Static
	}
	if cfg.Auth.GitHub != nil {
		if authentication.AllowUsersWithoutTeam != nil {
			cfg.Auth.GitHub.UserMapping.AllowUsersWithoutTeam = *authentication.AllowUsersWithoutTeam
		}
		if authentication.DefaultRole != nil {
			cfg.Auth.GitHub.UserMapping.DefaultRole = *authentication.DefaultRole
		}
		if authentication.TeamRoleMapping != nil {
			cfg.Auth.GitHub.UserMapping.TeamRoleMapping = authentication.TeamRoleMapping
		}
		if authentication.DefaultPermissions != nil {
			cfg.Auth.GitHub.UserMapping.DefaultPermissions = stringList(authentication.DefaultPermissions)
		}
	}

	var github struct {
		OAuth *struct {
			Enabled      *bool   `json:"enabled"`
			ClientID     *string `json:"client_id"`
			ClientSecret *string `json:"client_secret"`
			Scope        *string `json:"scope"`
			BaseURL      *string `json:"base_url"`
		} `json:"oauth"`
		Enterprise *struct {
			BaseURL *string `json:"base_url"`
		} `json:"enterprise"`
	}
	decode("github", &github)
	if cfg.Auth.GitHub != nil {
		if github.OAuth != nil {
			if github.OAuth.Enabled != nil {
				cfg.Auth.GitHub.Enabled = *github.OAuth.Enabled
			}
			if cfg.Auth.GitHub.OAuth == nil {
				cfg.Auth.GitHub.OAuth = &config.GitHubOAuthConfig{}
			}
			if github.OAuth.ClientID != nil {
				cfg.Auth.GitHub.OAuth.ClientID = *github.OAuth.ClientID
			}
			if github.OAuth.ClientSecret != nil {
				cfg.Auth.GitHub.OAuth.ClientSecret = *github.OAuth.ClientSecret
			}
			if github.OAuth.Scope != nil {
				cfg.Auth.GitHub.OAuth.Scope = *github.OAuth.Scope
			}
			if github.OAuth.BaseURL != nil {
				cfg.Auth.GitHub.OAuth.BaseURL = *github.OAuth.BaseURL
			}
		}
		if github.Enterprise != nil && github.Enterprise.BaseURL != nil {
			cfg.Auth.GitHub.BaseURL = *github.Enterprise.BaseURL
		}
	}

	var slack struct {
		CleanupEnabled       *bool   `json:"cleanup_enabled"`
		SessionTTL           *string `json:"session_ttl"`
		CleanupCheckInterval *string `json:"cleanup_check_interval"`
		CleanupDryRun        *bool   `json:"cleanup_dry_run"`
	}
	decode("slack", &slack)
	if slack.CleanupEnabled != nil {
		cfg.SlackbotCleanupWorker.Enabled = *slack.CleanupEnabled
	}
	if slack.SessionTTL != nil {
		cfg.SlackbotCleanupWorker.SessionTTL = *slack.SessionTTL
	}
	if slack.CleanupCheckInterval != nil {
		cfg.SlackbotCleanupWorker.CheckInterval = *slack.CleanupCheckInterval
	}
	if slack.CleanupDryRun != nil {
		cfg.SlackbotCleanupWorker.DryRun = *slack.CleanupDryRun
	}

	var notifications struct {
		WebhookBaseURL       *string `json:"webhook_base_url"`
		GitHubEnterpriseHost *string `json:"github_enterprise_host"`
	}
	decode("notifications", &notifications)
	if notifications.WebhookBaseURL != nil {
		cfg.Webhook.BaseURL = *notifications.WebhookBaseURL
	}
	if notifications.GitHubEnterpriseHost != nil {
		cfg.Webhook.GitHubEnterpriseHost = *notifications.GitHubEnterpriseHost
	}

	var workers struct {
		Schedule struct {
			Enabled       *bool   `json:"enabled"`
			CheckInterval *string `json:"check_interval"`
		} `json:"schedule"`
		Stock struct {
			Enabled       *bool   `json:"enabled"`
			CheckInterval *string `json:"check_interval"`
			TargetCount   *int    `json:"target_count"`
			DockerEnabled *bool   `json:"docker_enabled"`
		} `json:"stock"`
	}
	decode("workers", &workers)
	if workers.Schedule.Enabled != nil {
		cfg.ScheduleWorker.Enabled = *workers.Schedule.Enabled
	}
	if workers.Schedule.CheckInterval != nil {
		cfg.ScheduleWorker.CheckInterval = *workers.Schedule.CheckInterval
	}
	if workers.Stock.Enabled != nil {
		cfg.StockInventoryWorker.Enabled = *workers.Stock.Enabled
	}
	if workers.Stock.CheckInterval != nil {
		cfg.StockInventoryWorker.CheckInterval = *workers.Stock.CheckInterval
	}
	if workers.Stock.TargetCount != nil {
		cfg.StockInventoryWorker.TargetCount = *workers.Stock.TargetCount
	}
	if workers.Stock.DockerEnabled != nil {
		cfg.StockInventoryWorker.DockerEnabled = *workers.Stock.DockerEnabled
	}

	var sessions struct {
		Image           *string `json:"image"`
		CPURequest      *string `json:"cpu_request"`
		CPULimit        *string `json:"cpu_limit"`
		MemoryRequest   *string `json:"memory_request"`
		MemoryLimit     *string `json:"memory_limit"`
		PVCEnabled      *bool   `json:"pvc_enabled"`
		PVCStorageClass *string `json:"pvc_storage_class"`
		PVCSize         *string `json:"pvc_size"`
		PodStartTimeout *int    `json:"pod_start_timeout"`
		PodStopTimeout  *int    `json:"pod_stop_timeout"`
		OtelEnabled     *bool   `json:"otel_enabled"`
	}
	decode("sessions", &sessions)
	if sessions.Image != nil {
		cfg.KubernetesSession.Image = *sessions.Image
	}
	if sessions.CPURequest != nil {
		cfg.KubernetesSession.CPURequest = *sessions.CPURequest
	}
	if sessions.CPULimit != nil {
		cfg.KubernetesSession.CPULimit = *sessions.CPULimit
	}
	if sessions.MemoryRequest != nil {
		cfg.KubernetesSession.MemoryRequest = *sessions.MemoryRequest
	}
	if sessions.MemoryLimit != nil {
		cfg.KubernetesSession.MemoryLimit = *sessions.MemoryLimit
	}
	if sessions.PVCEnabled != nil {
		cfg.KubernetesSession.PVCEnabled = sessions.PVCEnabled
	}
	if sessions.PVCStorageClass != nil {
		cfg.KubernetesSession.PVCStorageClass = *sessions.PVCStorageClass
	}
	if sessions.PVCSize != nil {
		cfg.KubernetesSession.PVCStorageSize = *sessions.PVCSize
	}
	if sessions.PodStartTimeout != nil {
		cfg.KubernetesSession.PodStartTimeout = *sessions.PodStartTimeout
	}
	if sessions.PodStopTimeout != nil {
		cfg.KubernetesSession.PodStopTimeout = *sessions.PodStopTimeout
	}
	if sessions.OtelEnabled != nil {
		cfg.KubernetesSession.OtelCollectorEnabled = *sessions.OtelEnabled
	}

	var storage struct {
		UsageEnabled              *bool   `json:"usage_enabled"`
		RedisEnabled              *bool   `json:"redis_enabled"`
		RedisAddress              *string `json:"redis_address"`
		RedisPassword             *string `json:"redis_password"`
		RedisTLSEnabled           *bool   `json:"redis_tls_enabled"`
		SessionPersistenceBackend *string `json:"session_persistence_backend"`
		SessionPersistenceBucket  *string `json:"session_persistence_bucket"`
	}
	decode("storage", &storage)
	if storage.UsageEnabled != nil {
		cfg.Usage.Enabled = *storage.UsageEnabled
	}
	if storage.RedisEnabled != nil && !*storage.RedisEnabled {
		cfg.Redis.Addr = ""
	}
	if storage.RedisAddress != nil {
		cfg.Redis.Addr = *storage.RedisAddress
	}
	if storage.RedisPassword != nil {
		cfg.Redis.Password = *storage.RedisPassword
	}
	if storage.RedisTLSEnabled != nil {
		cfg.Redis.TLSEnabled = *storage.RedisTLSEnabled
	}
	if storage.SessionPersistenceBackend != nil {
		cfg.SessionPersistence.Backend = *storage.SessionPersistenceBackend
	}
	if storage.SessionPersistenceBucket != nil {
		if cfg.SessionPersistence.S3 == nil {
			cfg.SessionPersistence.S3 = &config.MemoryS3Config{}
		}
		cfg.SessionPersistence.S3.Bucket = *storage.SessionPersistenceBucket
	}

	var integrations struct {
		SciaEnabled    *bool `json:"scia_enabled"`
		TodoistEnabled *bool `json:"todoist_enabled"`
	}
	decode("integrations", &integrations)
	if integrations.SciaEnabled != nil {
		cfg.Scia.Enabled = *integrations.SciaEnabled
	}

	var security struct {
		NetworkFilterImage *string `json:"network_filter_image"`
	}
	decode("security", &security)
	if security.NetworkFilterImage != nil {
		cfg.KubernetesSession.NetworkFilterImage = *security.NetworkFilterImage
	}
	return nil
}

func stringList(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		var result []string
		for _, item := range splitLines(typed) {
			if item != "" {
				result = append(result, item)
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func splitLines(value string) []string {
	var result []string
	start := 0
	for i, r := range value {
		if r == '\n' || r == ',' {
			result = append(result, value[start:i])
			start = i + 1
		}
	}
	return append(result, value[start:])
}

func splitPath(value string) []string {
	var result []string
	start := 0
	for i, r := range value {
		if r == '.' {
			result = append(result, value[start:i])
			start = i + 1
		}
	}
	return append(result, value[start:])
}

func decodeSecret(value []byte, key string, target interface{}) error {
	var secret corev1.Secret
	if err := json.Unmarshal(value, &secret); err != nil {
		return err
	}
	if len(secret.Data[key]) == 0 {
		return fmt.Errorf("runtime config data %q is missing", key)
	}
	return json.Unmarshal(secret.Data[key], target)
}

func cloneConfig(source *config.Config) *config.Config {
	if source == nil {
		return &config.Config{}
	}
	data, _ := json.Marshal(source)
	var cloned config.Config
	_ = json.Unmarshal(data, &cloned)
	return &cloned
}

func cloneMap(source map[string]interface{}) map[string]interface{} {
	data, _ := json.Marshal(source)
	cloned := map[string]interface{}{}
	_ = json.Unmarshal(data, &cloned)
	return cloned
}
