package githubsyncmodule

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/modulehost"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	githubsync "github.com/takutakahashi/agentapi-proxy/pkg/github_sync"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterHandlers registers GitHub bidirectional sync REST API handlers.
func RegisterHandlers(configData *config.Config, proxyServer modulehost.GitHubSyncHost) {
	log.Printf("[GITHUB_SYNC] Registering GitHub sync handlers...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[GITHUB_SYNC] Kubernetes config not available, skipping GitHub sync handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[GITHUB_SYNC] Failed to create Kubernetes client, skipping GitHub sync handlers: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)

	scheduleManager := schedule.NewKubernetesManager(client, namespace)
	webhookRepo := repositories.NewKubernetesWebhookRepository(client, namespace)
	settingsRepo := proxyServer.GetSettingsRepository()
	memoryRepo := proxyServer.GetMemoryRepository()
	taskRepo := proxyServer.GetTaskRepository()
	taskGroupRepo := proxyServer.GetTaskGroupRepository()

	userFileRepo := portrepos.UserFileRepository(repositories.NewKubernetesUserFileRepository(client, namespace))
	slackbotRepo := portrepos.SlackBotRepository(repositories.NewKubernetesSlackBotRepository(client, namespace))

	syncHandlers := githubsync.NewHandlers(
		settingsRepo,
		scheduleManager,
		webhookRepo,
		memoryRepo,
		taskRepo,
		taskGroupRepo,
		userFileRepo,
		slackbotRepo,
		configData.GitSync.Encryption.KMSKeyARN,
		configData.GitSync.Encryption.AWSRegion,
		configData.GitSync.GitHubApp.InstallationID,
	)
	if sessionProfileRepo := proxyServer.GetSessionProfileRepository(); sessionProfileRepo != nil {
		syncHandlers.Syncer().SetSessionProfileRepository(sessionProfileRepo)
	}
	proxyServer.AddCustomHandler(syncHandlers)

	startPeriodicWorker(configData, syncHandlers, settingsRepo, client, namespace)

	log.Printf("[GITHUB_SYNC] GitHub sync handlers registered successfully")
}

func startPeriodicWorker(
	configData *config.Config,
	syncHandlers *githubsync.Handlers,
	settingsRepo portrepos.SettingsRepository,
	client kubernetes.Interface,
	namespace string,
) {
	interval := configData.GitSync.SyncInterval
	if interval == "" || interval == "0" {
		return
	}

	d, err := time.ParseDuration(interval)
	if err != nil {
		log.Printf("[GITHUB_SYNC] Invalid sync_interval %q: %v — periodic sync disabled", interval, err)
		return
	}

	syncNamespace := configData.GitSync.Namespace
	if syncNamespace == "" {
		syncNamespace = namespace
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: parseDurationOrDefault(configData.GitSync.LeaseDuration, 15*time.Second),
		RenewDeadline: parseDurationOrDefault(configData.GitSync.RenewDeadline, 10*time.Second),
		RetryPeriod:   parseDurationOrDefault(configData.GitSync.RetryPeriod, 2*time.Second),
		Namespace:     syncNamespace,
	}
	leaderWorker := githubsync.NewLeaderWorker(syncHandlers.Syncer(), settingsRepo, d, client, electionConfig)
	go leaderWorker.Run(context.Background())
	log.Printf("[GITHUB_SYNC] Periodic sync worker started with leader election (interval=%s)", interval)
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
