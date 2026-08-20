package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	infraallocation "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionmanagerapi"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	allocationworker "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionallocation"
	externalmanager "github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// SessionManagerRuntime is the execution-plane composition root. It does not
// construct the public API router, OAuth/auth services, webhooks, schedules or
// Slack workers. Kubernetes workload credentials terminate here.
type SessionManagerRuntime struct {
	config        *config.Config
	echo          *echo.Echo
	manager       *services.KubernetesSessionManager
	kvStore       kvstore.Store
	redis         *redis.Client
	allocator     *allocationworker.Worker
	runtimeCancel context.CancelFunc
}

func NewSessionManagerRuntime(parent context.Context, cfg *config.Config, verbose bool) (*SessionManagerRuntime, error) {
	if cfg == nil {
		return nil, errors.New("session-manager config is required")
	}
	if cfg.SessionManager.InternalAPIToken == "" {
		return nil, errors.New("session-manager internal API token is required")
	}
	if cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != "" && (cfg.SessionManager.ID == "" || cfg.SessionManager.RunnerPool == "") {
		return nil, errors.New("session-manager ID and runner pool are required")
	}
	manager, err := services.NewKubernetesSessionManager(cfg, verbose, logger.NewLogger())
	if err != nil {
		return nil, fmt.Errorf("initialize Kubernetes session manager: %w", err)
	}
	if cfg.SessionManager.RunnerPool != "" {
		manager.ConfigureSessionRunnerPool(cfg.SessionManager.UpstreamURL, cfg.SessionManager.ID, cfg.SessionManager.ConnectionToken, cfg.SessionManager.RunnerPool)
	}

	persistence := manager.GetClient()
	applicationStore, wrapped, err := buildApplicationKVStore(cfg.KVStore, persistence)
	if err != nil {
		_ = manager.Shutdown(5 * time.Second)
		return nil, fmt.Errorf("initialize session-manager KV store: %w", err)
	}
	if wrapped {
		persistence = kvstore.NewKubernetesAdapter(persistence, applicationStore)
	}

	applicationNamespace := cfg.KVStore.Namespace
	if applicationNamespace == "" {
		applicationNamespace = "default"
	}
	runtimeProvider := runtimeconfig.New(cfg, applicationStore, applicationNamespace)
	if err := runtimeProvider.Reload(parent); err != nil {
		log.Printf("[SESSION_MANAGER] Runtime settings reload failed; using startup configuration: %v", err)
	} else if runtimeProvider.Version() > 0 {
		cfg = runtimeProvider.Current()
	}
	manager.SetConfigProvider(runtimeProvider)
	runtimeCtx, runtimeCancel := context.WithCancel(parent)
	runtimeProvider.Start(runtimeCtx, 30*time.Second, func(err error) {
		log.Printf("[SESSION_MANAGER] Runtime settings reload failed: %v", err)
	})

	registry, err := newSessionManagerEncryptionRegistry()
	if err != nil {
		runtimeCancel()
		if applicationStore != nil {
			_ = applicationStore.Close()
		}
		_ = manager.Shutdown(5 * time.Second)
		return nil, err
	}
	settingsRepo := repositories.NewKubernetesSettingsRepository(persistence, applicationNamespace, registry)
	credentialsRepo := repositories.NewKubernetesCredentialsRepository(persistence, applicationNamespace)
	teamConfigRepo := repositories.NewKubernetesTeamConfigRepository(persistence, applicationNamespace)
	personalKeyRepo := repositories.NewKubernetesPersonalAPIKeyRepository(persistence, applicationNamespace)
	sandboxPolicyRepo := repositories.NewKubernetesSandboxPolicyRepository(persistence, applicationNamespace)
	manager.SetSettingsRepository(settingsRepo)
	manager.SetCredentialsRepository(credentialsRepo)
	manager.SetTeamConfigRepository(teamConfigRepo)
	manager.SetPersonalAPIKeyRepository(personalKeyRepo)
	manager.SetSandboxPolicyRepository(sandboxPolicyRepo)
	sessionRouteRepo := repositories.NewKubernetesSessionRouteRepository(persistence, applicationNamespace)
	manager.AddSessionDeletedHandler(func(ctx context.Context, session entities.Session) {
		cleanupLocalSessionRoutes(ctx, sessionRouteRepo, session.ID())
	})
	statusRepo := buildStatusEventRepository(cfg)
	manager.SetStatusEventRepository(statusRepo)
	if cache, ok := statusRepo.(portrepos.SessionListCacheRepository); ok {
		manager.SetSessionListCacheRepository(cache)
	}

	stateStore, err := services.NewSessionStateStore(parent, cfg.SessionPersistence)
	if err != nil {
		runtimeCancel()
		if applicationStore != nil {
			_ = applicationStore.Close()
		}
		_ = manager.Shutdown(5 * time.Second)
		return nil, fmt.Errorf("initialize session state store: %w", err)
	}
	controlStore := buildSessionControlStore(cfg)
	if controlStore != nil {
		manager.SetSessionControlStore(controlStore)
	}

	// A manager registered with a parent is an execution plane only: the parent
	// owns allocations and their claim leases.  Redis is needed only by the
	// standalone/local allocator.  Remote replicas coordinate their single
	// upstream poller with a Kubernetes Lease below.
	remoteMode := cfg.SessionManager.UpstreamURL != "" && cfg.SessionManager.ConnectionToken != ""
	var redisClient *redis.Client
	if !remoteMode {
		redisClient, err = newSessionManagerRedis(cfg)
		if err != nil {
			runtimeCancel()
			if applicationStore != nil {
				_ = applicationStore.Close()
			}
			_ = manager.Shutdown(5 * time.Second)
			return nil, err
		}
		manager.SetSessionAllocationNotifier(infraallocation.NewRedisNotifier(redisClient))
		manager.SetSessionAllocatorEnabled(true)
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.GET("/livez", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	e.GET("/healthz", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	e.GET("/readyz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
		defer cancel()
		if redisClient != nil {
			if err := redisClient.Ping(ctx).Err(); err != nil {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "redis unavailable"})
			}
		}
		if _, err := manager.GetClient().Discovery().ServerVersion(); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "kubernetes unavailable"})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})
	privateHandler, err := sessionmanagerapi.NewHandler(manager, cfg.SessionManager.InternalAPIToken)
	if err != nil {
		runtimeCancel()
		if redisClient != nil {
			_ = redisClient.Close()
		}
		if applicationStore != nil {
			_ = applicationStore.Close()
		}
		_ = manager.Shutdown(5 * time.Second)
		return nil, err
	}
	privateHandler.RegisterRoutes(e)

	// Session Pods authenticate with the narrower provisioner/session token.
	provisioner := controllers.NewProvisionerController(manager, manager, settingsRepo, nil, stateStore)
	e.POST("/internal/session-provisioners/connect", provisioner.Connect)
	e.GET("/internal/session-provisioners/:sessionId/provision-requests", provisioner.GetProvisionRequest)
	e.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", provisioner.UpdateProvisionRequestStatus)
	if stateStore != nil {
		e.PUT("/internal/session-state/:sessionId", provisioner.SaveSessionState)
		e.POST("/internal/session-state/:sessionId/suspend", provisioner.ScheduleSessionSuspend)
		e.GET("/internal/session-state/:sessionId", provisioner.LoadSessionState)
		e.POST("/internal/session-state/:sessionId/uploads", provisioner.BeginSessionStateUpload)
		e.GET("/internal/session-state/:sessionId/uploads/:uploadId/parts/:partNumber", provisioner.PresignSessionStatePart)
		e.POST("/internal/session-state/:sessionId/uploads/:uploadId/complete", provisioner.CompleteSessionStateUpload)
		e.DELETE("/internal/session-state/:sessionId/uploads/:uploadId", provisioner.AbortSessionStateUpload)
		e.GET("/internal/session-state/:sessionId/download-url", provisioner.PresignSessionStateDownload)
	}
	if controlStore != nil {
		control := controllers.NewSessionControlController(controlStore, manager)
		e.GET("/internal/session-control/:sessionId/commands", control.WaitCommands)
		e.POST("/internal/session-control/:sessionId/events", control.AppendEvents)
	}
	managedFiles := controllers.NewManagedFilesController(manager, credentialsRepo)
	e.PUT("/internal/session-control/:sessionId/managed-files", managedFiles.Save)
	// Register the top-level runtime forwarding routes after private and
	// provisioner routes. The forwarding pattern is intentionally limited to
	// one endpoint segment so it cannot overlap /internal/* callbacks.
	if remoteMode {
		if err := externalmanager.NewHandlers(manager, cfg.SessionManager.HMACSecret).RegisterRoutes(e); err != nil {
			runtimeCancel()
			if redisClient != nil {
				_ = redisClient.Close()
			}
			if applicationStore != nil {
				_ = applicationStore.Close()
			}
			_ = manager.Shutdown(5 * time.Second)
			return nil, fmt.Errorf("register remote session-manager routes: %w", err)
		}
	}
	var allocator *allocationworker.Worker
	lease := durationOr(cfg.SessionManager.Allocation.LeaseDuration, 15*time.Second)
	renew := durationOr(cfg.SessionManager.Allocation.RenewDeadline, 10*time.Second)
	retry := durationOr(cfg.SessionManager.Allocation.RetryPeriod, 2*time.Second)
	if !remoteMode {
		localClient := newLocalAllocationClient(manager, sessionRouteRepo)
		allocator = allocationworker.NewWorker(manager, localClient)
		elector := schedule.NewLeaderElector(redisClient, schedule.LeaderElectionConfig{
			LeaseDuration: lease, RenewDeadline: renew, RetryPeriod: retry,
			LeaseName: schedule.SessionAllocatorLeaseName, Namespace: manager.GetNamespace(),
		})
		go elector.Run(runtimeCtx, func(leaderCtx context.Context) {
			log.Printf("[SESSION_MANAGER] Became local allocation leader")
			if err := allocator.Start(leaderCtx); err != nil {
				log.Printf("[SESSION_MANAGER] Allocation loop failed: %v", err)
			}
		}, allocator.Stop)
	} else {
		instanceID, _ := os.Hostname()
		if instanceID == "" {
			instanceID = uuid.NewString()
		}
		lock, lockErr := resourcelock.New(
			resourcelock.LeasesResourceLock,
			manager.GetNamespace(),
			schedule.SessionAllocatorLeaseName,
			manager.GetClient().CoreV1(),
			manager.GetClient().CoordinationV1(),
			resourcelock.ResourceLockConfig{Identity: instanceID},
		)
		if lockErr != nil {
			runtimeCancel()
			if applicationStore != nil {
				_ = applicationStore.Close()
			}
			_ = manager.Shutdown(5 * time.Second)
			return nil, fmt.Errorf("create Kubernetes session-manager lease: %w", lockErr)
		}
		go leaderelection.RunOrDie(runtimeCtx, leaderelection.LeaderElectionConfig{
			Lock: lock, LeaseDuration: lease, RenewDeadline: renew, RetryPeriod: retry, ReleaseOnCancel: true,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(leaderCtx context.Context) {
					log.Printf("[SESSION_MANAGER] Became remote execution leader")
					control := externalmanager.NewControlWorker(cfg.SessionManager.UpstreamURL, cfg.SessionManager.ConnectionToken, "", cfg.SessionManager.APIURL, instanceID, cfg.SessionManager.HMACSecret)
					go control.Start(leaderCtx)
					runSessionRunnerManagerHeartbeat(leaderCtx, cfg.SessionManager.UpstreamURL, cfg.SessionManager.ID, cfg.SessionManager.ConnectionToken, manager)
				},
				OnStoppedLeading: func() { log.Printf("[SESSION_MANAGER] Lost remote execution leadership") },
				OnNewLeader:      func(identity string) { log.Printf("[SESSION_MANAGER] Remote execution leader is %s", identity) },
			},
		})
	}

	return &SessionManagerRuntime{config: cfg, echo: e, manager: manager, kvStore: applicationStore, redis: redisClient, allocator: allocator, runtimeCancel: runtimeCancel}, nil
}

func runSessionRunnerManagerHeartbeat(ctx context.Context, upstream, managerID, token string, manager *services.KubernetesSessionManager) {
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	appliedRevision := ""
	for {
		if revision, err := syncSessionRunnerRuntimeProfile(ctx, client, upstream, managerID, token, appliedRevision, manager); err != nil {
			log.Printf("[SESSION_MANAGER] Runner runtime profile sync failed: %v", err)
		} else if revision != "" {
			appliedRevision = revision
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(upstream, "/")+"/internal/session-managers/"+url.PathEscape(managerID)+"/heartbeat", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			if resp, doErr := client.Do(req); doErr != nil {
				log.Printf("[SESSION_MANAGER] Runner pool heartbeat failed: %v", doErr)
			} else {
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					log.Printf("[SESSION_MANAGER] Runner pool heartbeat returned HTTP %d", resp.StatusCode)
					_ = resp.Body.Close()
				} else {
					var result struct {
						Pools []*sessionrunnercore.PoolSupplier `json:"pools"`
					}
					if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
						log.Printf("[SESSION_MANAGER] Decode runner pool heartbeat: %v", decodeErr)
					}
					_ = resp.Body.Close()
					reconcileSessionRunnerPools(ctx, manager, result.Pools)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func syncSessionRunnerRuntimeProfile(ctx context.Context, client *http.Client, upstream, managerID, token, currentRevision string, manager *services.KubernetesSessionManager) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(upstream, "/")+"/internal/session-managers/"+url.PathEscape(managerID)+"/runtime-profile", nil)
	if err != nil {
		return currentRevision, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return currentRevision, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return currentRevision, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var snapshot coreallocation.RuntimeProfileSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return currentRevision, err
	}
	if snapshot.Revision == "" || snapshot.Profile == nil || snapshot.Revision == currentRevision {
		return currentRevision, nil
	}
	if err := manager.ApplyRuntimeProfile(ctx, snapshot.Profile); err != nil {
		return currentRevision, err
	}
	log.Printf("[SESSION_MANAGER] Applied runner runtime profile revision %s", snapshot.Revision)
	return snapshot.Revision, nil
}

func reconcileSessionRunnerPools(ctx context.Context, manager *services.KubernetesSessionManager, pools []*sessionrunnercore.PoolSupplier) {
	for _, pool := range pools {
		if pool == nil || !pool.Enabled || pool.Draining || pool.MinIdle <= 0 {
			continue
		}
		idle := pool.IdleRunners
		total, err := manager.CountRunnerSessionsForPool(ctx, pool.Pool)
		if err != nil {
			log.Printf("[SESSION_MANAGER] Count pool %s runners: %v", pool.Pool, err)
			continue
		}
		for idle < pool.MinIdle && (pool.MaxRunners <= 0 || total < pool.MaxRunners) {
			if err := manager.CreateStockSessionForPool(ctx, pool.Pool, false); err != nil {
				log.Printf("[SESSION_MANAGER] Create pool %s runner: %v", pool.Pool, err)
				break
			}
			idle++
			total++
		}
	}
}

func newSessionManagerEncryptionRegistry() (*services.EncryptionServiceRegistry, error) {
	factory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	primary, err := factory.Create()
	if err != nil {
		return nil, fmt.Errorf("create session-manager encryption service: %w", err)
	}
	registry := services.NewEncryptionServiceRegistry(primary)
	registry.Register(services.NewNoopEncryptionService())
	decryptFactory := services.NewEncryptionServiceFactory("AGENTAPI_DECRYPTION")
	if decrypt, err := decryptFactory.Create(); err == nil && (decrypt.Algorithm() != primary.Algorithm() || decrypt.KeyID() != primary.KeyID()) {
		registry.Register(decrypt)
	}
	return registry, nil
}

func newSessionManagerRedis(cfg *config.Config) (*redis.Client, error) {
	if cfg.Redis.Addr == "" {
		return nil, errors.New("session-manager Redis is required for allocation leader election")
	}
	opts := &redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB}
	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if cfg.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect session-manager Redis: %w", err)
	}
	return client, nil
}

func durationOr(raw string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

type localAllocationClient struct {
	queue  coreallocation.Queue
	routes portrepos.SessionRouteRepository
}

func newLocalAllocationClient(queue coreallocation.Queue, routes portrepos.SessionRouteRepository) *localAllocationClient {
	return &localAllocationClient{queue: queue, routes: routes}
}

func (c *localAllocationClient) Next(ctx context.Context, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.queue.NextSessionAllocation(ctx, wait)
}

func (c *localAllocationClient) Complete(ctx context.Context, sessionID string, result coreallocation.AllocationResult) error {
	// Persist the public-to-runtime stock alias before acknowledging the queue.
	// Queue completion deletes the allocation, so doing this in the opposite
	// order can permanently lose the only mapping after a crash or KV failure.
	if result.Status == coreallocation.StatusAssigned && result.AllocatedSessionID != "" && result.AllocatedSessionID != sessionID {
		if c.routes == nil {
			return fmt.Errorf("save session route for allocation %s: route repository is not configured", sessionID)
		}
		reader, ok := c.queue.(coreallocation.AllocationReader)
		if !ok {
			return fmt.Errorf("load allocation %s before saving session route: queue does not support durable allocation reads", sessionID)
		}
		allocation, err := reader.GetSessionAllocation(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("load allocation %s before saving session route: %w", sessionID, err)
		}
		if allocation == nil {
			return fmt.Errorf("load allocation %s before saving session route: allocation not found", sessionID)
		}
		if allocation.SessionID != sessionID {
			return fmt.Errorf("load allocation %s before saving session route: durable allocation has session ID %s", sessionID, allocation.SessionID)
		}
		route := &portrepos.SessionRoute{SessionID: allocation.SessionID, RemoteSessionID: result.AllocatedSessionID, StartedAt: time.Now()}
		if allocation.Request != nil {
			route.UserID = allocation.Request.UserID
			route.Scope = string(allocation.Request.Scope)
			route.TeamID = allocation.Request.TeamID
			route.Tags = allocation.Request.Tags
			route.InitialMessage = allocation.Request.InitialMessage
		}
		if err := c.routes.Save(ctx, route); err != nil {
			return err
		}
	}
	if _, err := c.queue.CompleteSessionAllocation(ctx, sessionID, result); err != nil {
		return err
	}
	return nil
}

func (r *SessionManagerRuntime) Echo() *echo.Echo { return r.echo }

func (r *SessionManagerRuntime) Manager() *services.KubernetesSessionManager { return r.manager }

func (r *SessionManagerRuntime) Config() *config.Config { return r.config }

func (r *SessionManagerRuntime) Shutdown(timeout time.Duration) error {
	if r.runtimeCancel != nil {
		r.runtimeCancel()
	}
	if r.allocator != nil {
		r.allocator.Stop()
	}
	var managerErr, kvErr, redisErr error
	if r.manager != nil {
		managerErr = r.manager.Shutdown(timeout)
	}
	if r.kvStore != nil {
		kvErr = r.kvStore.Close()
	}
	if r.redis != nil {
		redisErr = r.redis.Close()
	}
	return errors.Join(managerErr, kvErr, redisErr)
}
