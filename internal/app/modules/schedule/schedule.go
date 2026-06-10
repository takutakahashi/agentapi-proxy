package schedulemodule

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterHandlers registers schedule REST API handlers.
func RegisterHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SCHEDULE_HANDLERS] Registering schedule handlers...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Kubernetes config not available, skipping schedule handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Failed to create Kubernetes client, skipping schedule handlers: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)
	scheduleManager := schedule.NewKubernetesManager(client, namespace)

	if err := scheduleManager.MigrateFromLegacy(context.Background()); err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Migration from legacy format failed: %v", err)
	}

	scheduleHandlers := schedule.NewHandlers(scheduleManager, proxyServer.GetSessionManager(), proxyServer.GetMemoryRepository(), proxyServer.GetSessionProfileRepository())
	proxyServer.AddCustomHandler(scheduleHandlers)

	log.Printf("[SCHEDULE_HANDLERS] Schedule handlers registered successfully")
}

// StartWorker starts the schedule worker with leader election.
func StartWorker(configData *config.Config, proxyServer *app.Server) *schedule.LeaderWorker {
	log.Printf("[SCHEDULE_WORKER] Initializing schedule worker...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Kubernetes config not available, schedule worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to create Kubernetes client, schedule worker disabled: %v", err)
		return nil
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)
	scheduleManager := schedule.NewKubernetesManager(client, namespace)

	checkInterval, err := time.ParseDuration(configData.ScheduleWorker.CheckInterval)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid check_interval, using default 30s: %v", err)
		checkInterval = 30 * time.Second
	}

	workerConfig := schedule.WorkerConfig{
		CheckInterval: checkInterval,
		Enabled:       true,
	}

	leaseDuration, err := time.ParseDuration(configData.ScheduleWorker.LeaseDuration)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid lease_duration, using default 15s: %v", err)
		leaseDuration = 15 * time.Second
	}

	renewDeadline, err := time.ParseDuration(configData.ScheduleWorker.RenewDeadline)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid renew_deadline, using default 10s: %v", err)
		renewDeadline = 10 * time.Second
	}

	retryPeriod, err := time.ParseDuration(configData.ScheduleWorker.RetryPeriod)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid retry_period, using default 2s: %v", err)
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		LeaseName:     "agentapi-schedule-worker",
		Namespace:     namespace,
	}

	leaderWorker := schedule.NewLeaderWorker(
		scheduleManager,
		proxyServer.GetSessionManager(),
		client,
		workerConfig,
		electionConfig,
		proxyServer.GetMemoryRepository(),
		proxyServer.GetSessionProfileRepository(),
	)

	go leaderWorker.Run(context.Background())

	log.Printf("[SCHEDULE_WORKER] Schedule worker started in namespace: %s", namespace)
	return leaderWorker
}
