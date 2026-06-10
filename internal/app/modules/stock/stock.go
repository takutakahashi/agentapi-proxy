package stockmodule

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/modulehost"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	stockinventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartWorker starts the stock session inventory worker with leader election.
func StartWorker(configData *config.Config, proxyServer modulehost.SessionManagerProvider) *stockinventory.LeaderWorker {
	log.Printf("[STOCK_INVENTORY] Initializing stock inventory worker...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Kubernetes config not available, stock inventory worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Failed to create Kubernetes client, stock inventory worker disabled: %v", err)
		return nil
	}

	namespace := k8sutil.ResolveNamespace(configData.StockInventoryWorker.Namespace, configData.KubernetesSession.Namespace)

	stockRepo, ok := proxyServer.GetSessionManager().(stockinventory.StockRepository)
	if !ok {
		log.Printf("[STOCK_INVENTORY] Session manager does not implement StockRepository, stock inventory worker disabled")
		return nil
	}

	checkInterval, err := time.ParseDuration(configData.StockInventoryWorker.CheckInterval)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Invalid check_interval, using default 30s: %v", err)
		checkInterval = 30 * time.Second
	}

	targetCount := configData.StockInventoryWorker.TargetCount
	if targetCount <= 0 {
		targetCount = 2
	}
	pools := BuildPools(configData.StockInventoryWorker, targetCount)

	workerConfig := stockinventory.WorkerConfig{
		CheckInterval: checkInterval,
		TargetCount:   targetCount,
		Requirements: stockinventory.StockRequirements{
			Sandbox: configData.StockInventoryWorker.SandboxEnabled,
			DinD:    configData.StockInventoryWorker.DockerEnabled,
		},
		Pools:   pools,
		Enabled: true,
	}

	leaseDuration, err := time.ParseDuration(configData.StockInventoryWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 15 * time.Second
	}
	renewDeadline, err := time.ParseDuration(configData.StockInventoryWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 10 * time.Second
	}
	retryPeriod, err := time.ParseDuration(configData.StockInventoryWorker.RetryPeriod)
	if err != nil {
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		LeaseName:     "agentapi-stock-inventory-worker",
		Namespace:     namespace,
	}

	leaderWorker := stockinventory.NewLeaderWorker(stockRepo, client, workerConfig, electionConfig)

	go leaderWorker.Run(context.Background())

	poolCount := len(pools)
	if poolCount == 0 {
		poolCount = 1
	}
	log.Printf("[STOCK_INVENTORY] Stock inventory worker started in namespace: %s (pools: %d)",
		namespace, poolCount)
	return leaderWorker
}

func BuildPools(workerConfig config.StockInventoryWorkerConfig, defaultTargetCount int) []stockinventory.StockPool {
	if len(workerConfig.Pools) == 0 {
		return nil
	}

	pools := make([]stockinventory.StockPool, 0, len(workerConfig.Pools))
	for _, poolConfig := range workerConfig.Pools {
		targetCount := poolConfig.TargetCount
		if targetCount < 0 {
			targetCount = defaultTargetCount
		}
		pools = append(pools, stockinventory.StockPool{
			TargetCount: targetCount,
			Requirements: stockinventory.StockRequirements{
				Sandbox: poolConfig.SandboxEnabled,
				DinD:    poolConfig.DockerEnabled,
			},
		})
	}
	return pools
}
