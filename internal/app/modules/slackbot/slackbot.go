package slackbotmodule

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/slackbot"
	slackbotcleanup "github.com/takutakahashi/agentapi-proxy/pkg/slackbot_cleanup"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterHandlers registers SlackBot management REST API handlers.
func RegisterHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SLACKBOT_HANDLERS] Registering slackbot handlers...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SLACKBOT_HANDLERS] Kubernetes config not available, skipping slackbot handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SLACKBOT_HANDLERS] Failed to create Kubernetes client, skipping slackbot handlers: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)
	slackbotRepo := repositories.NewKubernetesSlackBotRepository(client, namespace)

	slackbotHandlers := slackbot.NewHandlers(slackbotRepo)
	proxyServer.AddCustomHandler(slackbotHandlers)

	log.Printf("[SLACKBOT_HANDLERS] SlackBot management handlers registered successfully")
}

// StartCleanupWorker starts the Slackbot session cleanup worker with leader election.
func StartCleanupWorker(configData *config.Config, proxyServer *app.Server) *slackbotcleanup.LeaderCleanupWorker {
	log.Printf("[SLACKBOT_CLEANUP] Initializing Slackbot cleanup worker...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SLACKBOT_CLEANUP] Kubernetes config not available, cleanup worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SLACKBOT_CLEANUP] Failed to create Kubernetes client, cleanup worker disabled: %v", err)
		return nil
	}

	namespace := k8sutil.ResolveNamespace(configData.KubernetesSession.Namespace)

	checkInterval, err := time.ParseDuration(configData.SlackbotCleanupWorker.CheckInterval)
	if err != nil || checkInterval <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid check_interval, using default 1h: %v", err)
		checkInterval = 1 * time.Hour
	}

	sessionTTL, err := time.ParseDuration(configData.SlackbotCleanupWorker.SessionTTL)
	if err != nil || sessionTTL <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid session_ttl, using default 72h: %v", err)
		sessionTTL = 72 * time.Hour
	}

	sessionTTLCheckInterval := 1 * time.Minute
	if configData.SlackbotCleanupWorker.SessionTTLCheckInterval != "" {
		if d, err := time.ParseDuration(configData.SlackbotCleanupWorker.SessionTTLCheckInterval); err == nil && d > 0 {
			sessionTTLCheckInterval = d
		} else {
			log.Printf("[SLACKBOT_CLEANUP] Invalid session_ttl_check_interval, using default 1m: %v", err)
		}
	}

	workerConfig := slackbotcleanup.CleanupWorkerConfig{
		CheckInterval:           checkInterval,
		SessionTTLCheckInterval: sessionTTLCheckInterval,
		SessionTTL:              sessionTTL,
		Enabled:                 true,
		DryRun:                  configData.SlackbotCleanupWorker.DryRun,
	}

	leaseDuration, err := time.ParseDuration(configData.SlackbotCleanupWorker.LeaseDuration)
	if err != nil || leaseDuration <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid lease_duration, using default 15s: %v", err)
		leaseDuration = 15 * time.Second
	}

	renewDeadline, err := time.ParseDuration(configData.SlackbotCleanupWorker.RenewDeadline)
	if err != nil || renewDeadline <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid renew_deadline, using default 10s: %v", err)
		renewDeadline = 10 * time.Second
	}

	retryPeriod, err := time.ParseDuration(configData.SlackbotCleanupWorker.RetryPeriod)
	if err != nil || retryPeriod <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid retry_period, using default 2s: %v", err)
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     namespace,
	}

	leaderCleanupWorker := slackbotcleanup.NewLeaderCleanupWorker(
		proxyServer.GetSessionManager(),
		client,
		namespace,
		workerConfig,
		electionConfig,
	)

	go leaderCleanupWorker.Run(context.Background())

	dryRunNote := ""
	if configData.SlackbotCleanupWorker.DryRun {
		dryRunNote = " [DRY-RUN]"
	}
	log.Printf("[SLACKBOT_CLEANUP] Slackbot cleanup worker started in namespace: %s (TTL: %v)%s", namespace, sessionTTL, dryRunNote)
	return leaderCleanupWorker
}

// StartSocketManager starts the Slack Socket Mode manager with per-bot leader election.
func StartSocketManager(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SOCKET_MANAGER] Initializing Slack Socket Mode manager...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Kubernetes config not available, skipping Socket Mode: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Failed to create Kubernetes client, skipping Socket Mode: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)

	slackbotRepo := repositories.NewKubernetesSlackBotRepository(client, namespace)
	channelResolver := services.NewSlackChannelResolver(client, namespace)

	eventHandler := controllers.NewSlackBotEventHandler(
		slackbotRepo,
		proxyServer.GetSessionManager(),
		configData.KubernetesSession.SlackBotTokenSecretName,
		configData.KubernetesSession.SlackBotTokenSecretKey,
		channelResolver,
		configData.Webhook.BaseURL,
		configData.Slack.DryRun,
		proxyServer.GetMemoryRepository(),
		proxyServer.GetSessionProfileRepository(),
	)
	if configData.Slack.DryRun {
		log.Printf("[SOCKET_MANAGER] Slack dry-run mode enabled: session creation and Slack posts will be logged only")
	}

	appTokenSecretName := configData.Slack.AppTokenSecretName
	if appTokenSecretName == "" {
		appTokenSecretName = configData.KubernetesSession.SlackBotTokenSecretName
	}
	appTokenSecretKey := configData.Slack.AppTokenSecretKey
	if appTokenSecretKey == "" {
		appTokenSecretKey = "app-token"
	}

	leaseDuration, err := time.ParseDuration(configData.ScheduleWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 15 * time.Second
	}
	renewDeadline, err := time.ParseDuration(configData.ScheduleWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 10 * time.Second
	}
	retryPeriod, err := time.ParseDuration(configData.ScheduleWorker.RetryPeriod)
	if err != nil {
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     namespace,
	}

	managerConfig := slackbot.SlackSocketManagerConfig{
		DefaultAppTokenSecretName: appTokenSecretName,
		DefaultAppTokenSecretKey:  appTokenSecretKey,
		DefaultBotTokenSecretName: configData.KubernetesSession.SlackBotTokenSecretName,
		DefaultBotTokenSecretKey:  configData.KubernetesSession.SlackBotTokenSecretKey,
		LeaderElectionConfig:      electionConfig,
	}

	manager := slackbot.NewSlackSocketManager(
		client,
		namespace,
		slackbotRepo,
		eventHandler,
		channelResolver,
		managerConfig,
	)

	go manager.Run(context.Background())

	log.Printf("[SOCKET_MANAGER] Slack Socket Mode manager started in namespace: %s", namespace)
}
