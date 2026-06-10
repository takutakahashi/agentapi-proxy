package sessionmodule

import (
	"context"
	"crypto/tls"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionmanager"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartAllocator starts the leader-elected SessionAllocator.
func StartAllocator(configData *config.Config, proxyServer *app.Server) *services.SessionAllocator {
	log.Printf("[SESSION_ALLOCATOR] Initializing session allocator...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SESSION_ALLOCATOR] Kubernetes config not available, session allocator disabled: %v", err)
		return nil
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to create Kubernetes client, session allocator disabled: %v", err)
		return nil
	}

	manager, ok := proxyServer.GetSessionManager().(*services.KubernetesSessionManager)
	if !ok {
		log.Printf("[SESSION_ALLOCATOR] Session manager is not KubernetesSessionManager, session allocator disabled")
		return nil
	}
	manager.SetSessionAllocationNotifier(buildAllocationNotifier(configData))

	namespace := k8sutil.ResolveNamespace(configData.StockInventoryWorker.Namespace, configData.KubernetesSession.Namespace)

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

	manager.SetSessionAllocatorEnabled(true)
	allocator := services.NewSessionAllocator(manager)
	electorConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     namespace,
		LeaseName:     "agentapi-session-allocator",
	}
	elector := schedule.NewLeaderElector(client, electorConfig)
	go elector.Run(context.Background(),
		func(leaderCtx context.Context) {
			log.Printf("[SESSION_ALLOCATOR] Became leader")
			if err := allocator.Start(leaderCtx); err != nil {
				log.Printf("[SESSION_ALLOCATOR] Failed to start: %v", err)
			}
		},
		func() {
			log.Printf("[SESSION_ALLOCATOR] Lost leadership")
			allocator.Stop()
		},
	)
	log.Printf("[SESSION_ALLOCATOR] Session allocator started in namespace: %s", namespace)
	return allocator
}

func buildAllocationNotifier(configData *config.Config) services.SessionAllocationNotifier {
	if configData.Redis.Addr == "" {
		log.Printf("[SESSION_ALLOCATOR] Redis not configured; using local allocation notifier")
		return services.NewLocalSessionAllocationNotifier()
	}
	opts := &redis.Options{
		Addr:     configData.Redis.Addr,
		Password: configData.Redis.Password,
		DB:       configData.Redis.DB,
	}
	if d, err := time.ParseDuration(configData.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(configData.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(configData.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if configData.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Warning: Redis ping failed (%s); using local allocation notifier: %v", configData.Redis.Addr, err)
		_ = client.Close()
		return services.NewLocalSessionAllocationNotifier()
	}
	log.Printf("[SESSION_ALLOCATOR] Redis allocation notifier connected: addr=%s", configData.Redis.Addr)
	return services.NewRedisSessionAllocationNotifier(client)
}

// RegisterManagerHandlers registers the session manager forwarding endpoint.
func RegisterManagerHandlers(configData *config.Config, proxyServer *app.Server) {
	if !configData.SessionManager.Enabled {
		log.Printf("[SESSION_MANAGER] Session manager endpoint is disabled")
		return
	}

	sessionManager := proxyServer.GetSessionManager()
	if sessionManager == nil {
		log.Printf("[SESSION_MANAGER] Warning: session manager is not available, skipping handler registration")
		return
	}

	handlers := sessionmanager.NewHandlers(sessionManager, configData.SessionManager.HMACSecret)
	proxyServer.AddCustomHandler(handlers)
	log.Printf("[SESSION_MANAGER] Session manager handler registered")
}

// StartManagerAllocator starts outbound allocator polling against an upstream proxy.
func StartManagerAllocator(ctx context.Context, configData *config.Config, proxyServer *app.Server) {
	upstreamURL := configData.SessionManager.UpstreamURL
	token := configData.SessionManager.ConnectionToken
	if upstreamURL == "" || token == "" {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Upstream URL or connection token is empty; allocator disabled")
		return
	}

	sessionManager := proxyServer.GetSessionManager()
	if sessionManager == nil {
		log.Printf("[SESSION_MANAGER_ALLOCATOR] Warning: session manager is not available, allocator disabled")
		return
	}

	worker := sessionmanager.NewAllocatorWorker(sessionManager, upstreamURL, token, configData.SessionManager.PublicURL)
	go worker.Start(ctx)
	log.Printf("[SESSION_MANAGER_ALLOCATOR] Started outbound allocator polling upstream: %s", upstreamURL)
}
