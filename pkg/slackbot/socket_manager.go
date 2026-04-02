package slackbot

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	repoports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"k8s.io/client-go/kubernetes"
)

const (
	// slackSocketLeasePrefix is the prefix for Socket Mode leader election Lease names
	slackSocketLeasePrefix = "agentapi-slackbot-socket-"
	// defaultBotKey is the key used for the default (no custom token) bot group
	defaultBotKey = "default"
	// defaultReconcileInterval is how often to reconcile the bot list
	defaultReconcileInterval = 30 * time.Second
)

// SlackSocketManager discovers registered SlackBots and manages one leader-elected
// socket worker per bot group:
//   - "default": all bots with no custom bot token secret
//   - <slackbot-id>: each bot with a custom BotTokenSecretName
type SlackSocketManager struct {
	kubeClient      kubernetes.Interface
	namespace       string
	repo            repoports.SlackBotRepository
	eventHandler    *controllers.SlackBotEventHandler
	channelResolver *services.SlackChannelResolver

	// Default token configuration (from server config)
	defaultAppTokenSecretName string
	defaultAppTokenSecretKey  string
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string

	reconcileInterval    time.Duration
	leaderElectionConfig schedule.LeaderElectionConfig

	mu      sync.Mutex
	running map[string]runningEntry // botKey → entry
}

// runningEntry holds the cancel function and updatedAt snapshot for a running worker
type runningEntry struct {
	cancel    context.CancelFunc
	updatedAt time.Time // snapshot of bot.UpdatedAt() when worker started
	id        string    // unique ID to detect stale entries after natural goroutine exit
}

// SlackSocketManagerConfig holds configuration for SlackSocketManager
type SlackSocketManagerConfig struct {
	DefaultAppTokenSecretName string
	DefaultAppTokenSecretKey  string
	DefaultBotTokenSecretName string
	DefaultBotTokenSecretKey  string
	ReconcileInterval         time.Duration
	LeaderElectionConfig      schedule.LeaderElectionConfig
}

// NewSlackSocketManager creates a new SlackSocketManager
func NewSlackSocketManager(
	kubeClient kubernetes.Interface,
	namespace string,
	repo repoports.SlackBotRepository,
	eventHandler *controllers.SlackBotEventHandler,
	channelResolver *services.SlackChannelResolver,
	cfg SlackSocketManagerConfig,
) *SlackSocketManager {
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = defaultReconcileInterval
	}
	if cfg.DefaultAppTokenSecretKey == "" {
		cfg.DefaultAppTokenSecretKey = "app-token"
	}
	if cfg.DefaultBotTokenSecretKey == "" {
		cfg.DefaultBotTokenSecretKey = "bot-token"
	}

	return &SlackSocketManager{
		kubeClient:                kubeClient,
		namespace:                 namespace,
		repo:                      repo,
		eventHandler:              eventHandler,
		channelResolver:           channelResolver,
		defaultAppTokenSecretName: cfg.DefaultAppTokenSecretName,
		defaultAppTokenSecretKey:  cfg.DefaultAppTokenSecretKey,
		defaultBotTokenSecretName: cfg.DefaultBotTokenSecretName,
		defaultBotTokenSecretKey:  cfg.DefaultBotTokenSecretKey,
		reconcileInterval:         cfg.ReconcileInterval,
		leaderElectionConfig:      cfg.LeaderElectionConfig,
		running:                   make(map[string]runningEntry),
	}
}

// Run starts the reconciliation loop.
// On each tick, it discovers SlackBots and starts/stops leader-elected socket workers.
func (m *SlackSocketManager) Run(ctx context.Context) {
	log.Printf("[SOCKET_MANAGER] Starting")
	m.reconcile(ctx)

	ticker := time.NewTicker(m.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			log.Printf("[SOCKET_MANAGER] Stopped")
			return
		case <-ticker.C:
			m.reconcile(ctx)
		}
	}
}

// Stop cancels all running workers
func (m *SlackSocketManager) Stop() {
	m.stopAll()
}

// reconcile lists all SlackBots and starts/stops leader elections as needed
func (m *SlackSocketManager) reconcile(ctx context.Context) {
	bots, err := m.repo.List(ctx, repoports.SlackBotFilter{})
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Failed to list SlackBots: %v", err)
		return
	}

	// Build required bot key set
	required := make(map[string]struct{})

	// "default" group: enabled if default App token secret is configured
	if m.defaultAppTokenSecretName != "" {
		required[defaultBotKey] = struct{}{}
	}

	// Custom bots: each SlackBot with a custom BotTokenSecretName gets its own worker
	for _, bot := range bots {
		if bot.BotTokenSecretName() != "" {
			required[bot.ID()] = struct{}{}
		}
	}

	// Restart workers whose updatedAt has changed (token updated)
	for _, bot := range bots {
		if bot.BotTokenSecretName() == "" {
			continue
		}
		key := bot.ID()
		m.mu.Lock()
		entry, running := m.running[key]
		m.mu.Unlock()
		if running && !entry.updatedAt.Equal(bot.UpdatedAt()) {
			log.Printf("[SOCKET_MANAGER] Bot %s updated (updatedAt changed), restarting worker", key)
			m.stopWorker(key)
			// required に入っているので次のループで startLeaderElection される
		}
	}

	// Start new workers for new keys
	for key := range required {
		m.mu.Lock()
		_, alreadyRunning := m.running[key]
		m.mu.Unlock()
		if !alreadyRunning {
			// Find updatedAt for this key (zero value for default bot)
			var updatedAt time.Time
			for _, bot := range bots {
				if bot.ID() == key {
					updatedAt = bot.UpdatedAt()
					break
				}
			}
			m.startLeaderElection(ctx, key, updatedAt)
		}
	}

	// Stop workers for removed keys
	m.mu.Lock()
	var toStop []string
	for key := range m.running {
		if _, needed := required[key]; !needed {
			toStop = append(toStop, key)
		}
	}
	m.mu.Unlock()

	for _, key := range toStop {
		m.stopWorker(key)
	}
}

// startLeaderElection starts a leader election for the given bot key.
// The leader runs a SlackSocketWorker goroutine.
// updatedAt should be bot.UpdatedAt() for custom bots, or zero value for the default bot.
func (m *SlackSocketManager) startLeaderElection(ctx context.Context, botKey string, updatedAt time.Time) {
	leaseName := slackSocketLeasePrefix + sanitizeLeaseName(botKey)
	electionConfig := m.leaderElectionConfig
	electionConfig.LeaseName = leaseName
	elector := schedule.NewLeaderElector(m.kubeClient, electionConfig)

	childCtx, cancel := context.WithCancel(ctx)
	entryID := uuid.New().String()
	m.mu.Lock()
	m.running[botKey] = runningEntry{cancel: cancel, updatedAt: updatedAt, id: entryID}
	m.mu.Unlock()

	log.Printf("[SOCKET_MANAGER] Starting leader election for botKey=%s (lease=%s)", botKey, leaseName)

	go func() {
		elector.Run(childCtx,
			func(leaderCtx context.Context) {
				log.Printf("[SOCKET_MANAGER] Became leader for botKey=%s", botKey)
				worker := m.newWorker(botKey)
				if worker == nil {
					log.Printf("[SOCKET_MANAGER] Cannot create worker for botKey=%s (missing configuration)", botKey)
					return
				}
				worker.Run(leaderCtx)
			},
			func() {
				log.Printf("[SOCKET_MANAGER] Lost leadership for botKey=%s", botKey)
			},
		)
		// When the leader election goroutine exits naturally (e.g. lease renewal failure,
		// network partition), clean up the running entry so the next reconcile cycle can
		// restart the election.  We only delete if the entry still belongs to this
		// goroutine (matched by entryID); stopWorker() may have already replaced it with
		// a newer entry.
		m.mu.Lock()
		if entry, ok := m.running[botKey]; ok && entry.id == entryID {
			delete(m.running, botKey)
			log.Printf("[SOCKET_MANAGER] Leader election goroutine exited for botKey=%s, entry removed for restart", botKey)
		}
		m.mu.Unlock()
	}()
}

// newWorker creates a SlackSocketWorker for the given bot key
func (m *SlackSocketManager) newWorker(botKey string) *SlackSocketWorker {
	if botKey == defaultBotKey {
		// Default worker: uses server-configured App token and bot token
		if m.defaultAppTokenSecretName == "" {
			log.Printf("[SOCKET_MANAGER] Default App token secret not configured; cannot start default worker")
			return nil
		}
		botTokenSecretName := m.defaultBotTokenSecretName
		if botTokenSecretName == "" {
			// Fall back to App token secret (same Secret holds both tokens)
			botTokenSecretName = m.defaultAppTokenSecretName
		}
		return NewSlackSocketWorker(
			defaultBotKey,
			m.defaultAppTokenSecretName,
			m.defaultAppTokenSecretKey,
			botTokenSecretName,
			m.defaultBotTokenSecretKey,
			m.channelResolver,
			m.eventHandler,
		)
	}

	// Custom bot: load the SlackBot entity to get its token configuration
	bot, err := m.repo.Get(context.Background(), botKey)
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Failed to get SlackBot %s: %v", botKey, err)
		return nil
	}

	// App token and bot token are stored in the same Secret (different keys)
	secretName := bot.BotTokenSecretName()
	if secretName == "" {
		log.Printf("[SOCKET_MANAGER] SlackBot %s has no custom BotTokenSecretName; skipping", botKey)
		return nil
	}

	return NewSlackSocketWorker(
		botKey,
		secretName,              // App token secret (same Secret as bot token)
		bot.AppTokenSecretKey(), // App token key within Secret
		secretName,              // Bot token secret
		bot.BotTokenSecretKey(), // Bot token key within Secret
		m.channelResolver,
		m.eventHandler,
	)
}

// stopWorker cancels a running leader election / worker for the given bot key
func (m *SlackSocketManager) stopWorker(key string) {
	m.mu.Lock()
	entry, ok := m.running[key]
	if ok {
		delete(m.running, key)
	}
	m.mu.Unlock()

	if ok {
		log.Printf("[SOCKET_MANAGER] Stopping worker for botKey=%s", key)
		entry.cancel()
	}
}

// stopAll cancels all running workers
func (m *SlackSocketManager) stopAll() {
	m.mu.Lock()
	keys := make([]string, 0, len(m.running))
	for k := range m.running {
		keys = append(keys, k)
	}
	m.mu.Unlock()

	for _, key := range keys {
		m.stopWorker(key)
	}
}

// sanitizeLeaseName converts a bot key to a valid Kubernetes Lease name component.
// Kubernetes Lease names must be lowercase alphanumeric or '-'.
func sanitizeLeaseName(key string) string {
	// Replace any non-alphanumeric characters with '-'
	var result strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}
	}
	// Trim leading/trailing dashes
	s := strings.Trim(result.String(), "-")
	if s == "" {
		s = "unknown"
	}
	return s
}
