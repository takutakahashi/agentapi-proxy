package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
)

// provisionerPort is the TCP port on which agent-provisioner listens inside session Pods.
// The proxy server calls POST http://<sessionDNS>:provisionerPort/provision to trigger
// the session startup sequence after the Pod becomes ready.
const provisionerPort = 9001

// ProvisionerPort is the exported version of provisionerPort for use by other packages
// (e.g. the session controller error handler that checks provisioner /status).
const ProvisionerPort = provisionerPort

// KubernetesSessionManager manages sessions using Kubernetes Deployments
// ServiceAccountEnsurer ensures a service account exists for a team.
// Implementations must be safe to call concurrently.
type ServiceAccountEnsurer interface {
	EnsureServiceAccount(ctx context.Context, teamID string) error
}

// SessionDeletedHandler is a callback invoked just before a session's Kubernetes resources
// are removed. At this point the session's Service endpoint is still reachable, so handlers
// can safely call GetMessages or other in-session APIs.
// Handlers are called synchronously and their errors are intentionally ignored so that
// session deletion always proceeds regardless of handler failures.
type SessionDeletedHandler func(ctx context.Context, session entities.Session)

type KubernetesSessionManager struct {
	config                *config.Config
	k8sConfig             *config.KubernetesSessionConfig
	client                kubernetes.Interface
	verbose               bool
	logger                *logger.Logger
	sessions              map[string]*KubernetesSession
	mutex                 sync.RWMutex
	namespace             string
	settingsRepo          portrepos.SettingsRepository
	teamConfigRepo        portrepos.TeamConfigRepository
	personalAPIKeyRepo    portrepos.PersonalAPIKeyRepository
	serviceAccountEnsurer ServiceAccountEnsurer
	// onSessionDeletedHandlers holds callbacks registered via AddSessionDeletedHandler.
	// Protected by handlersMutex.
	onSessionDeletedHandlers []SessionDeletedHandler
	handlersMutex            sync.RWMutex
}

// NewKubernetesSessionManager creates a new KubernetesSessionManager
func NewKubernetesSessionManager(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
) (*KubernetesSessionManager, error) {
	// Get config using controller-runtime (supports in-cluster and kubeconfig)
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return NewKubernetesSessionManagerWithClient(cfg, verbose, lgr, client)
}

// NewKubernetesSessionManagerWithClient creates a new KubernetesSessionManager with a custom client
// This is useful for testing with a fake client
func NewKubernetesSessionManagerWithClient(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
	client kubernetes.Interface,
) (*KubernetesSessionManager, error) {
	k8sConfig := &cfg.KubernetesSession

	// Determine namespace
	namespace := k8sConfig.Namespace
	if namespace == "" {
		// Use namespace from controller-runtime config or default
		namespace = "default"
	}

	log.Printf("[K8S_SESSION] Initialized KubernetesSessionManager in namespace: %s", namespace)

	manager := &KubernetesSessionManager{
		config:    cfg,
		k8sConfig: k8sConfig,
		client:    client,
		verbose:   verbose,
		logger:    lgr,
		sessions:  make(map[string]*KubernetesSession),
		namespace: namespace,
	}

	// Ensure OpenTelemetry Collector ConfigMap exists
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.ensureOtelcolConfigMap(ctx); err != nil {
		log.Printf("[K8S_SESSION] Warning: Failed to ensure otelcol ConfigMap: %v", err)
		// Don't fail initialization if ConfigMap creation fails
	}

	return manager, nil
}

// CreateSession creates a new session with a Kubernetes Deployment.
// It first attempts to use a pre-warmed stock session (labeled agentapi.proxy/stock=true).
// If no stock is available, a new session is created from scratch.
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	// Attempt to adopt a stock session before creating a new one.
	if stockSvc, err := m.findStockSession(ctx); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to search for stock sessions: %v", err)
	} else if stockSvc != nil {
		claimedSvc, claimErr := m.claimStockService(ctx, stockSvc)
		if claimErr != nil {
			log.Printf("[K8S_SESSION] Stock session claim failed (concurrent claim?), falling back to new session creation: %v", claimErr)
		} else {
			log.Printf("[K8S_SESSION] Found stock session %s, adopting for new request", claimedSvc.Labels["agentapi.proxy/session-id"])
			return m.adoptStockSession(ctx, req, webhookPayload, claimedSvc)
		}
	}

	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Generate resource names
	deploymentName := fmt.Sprintf("agentapi-session-%s", id)
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)

	// Create KubernetesSession using constructor
	session := NewKubernetesSession(
		id,
		req,
		deploymentName,
		serviceName,
		pvcName,
		m.namespace,
		m.k8sConfig.BasePort,
		cancel,
		webhookPayload,
	)

	// Store session
	m.mutex.Lock()
	m.sessions[id] = session
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Creating session %s in namespace %s", id, m.namespace)

	// Create PVC if enabled
	if m.isPVCEnabled() {
		if err := m.createPVC(ctx, session); err != nil {
			m.cleanupSession(id)
			return nil, fmt.Errorf("failed to create PVC: %w", err)
		}
		log.Printf("[K8S_SESSION] Created PVC %s for session %s", pvcName, id)
	} else {
		log.Printf("[K8S_SESSION] PVC disabled, using EmptyDir for session %s", id)
	}

	// Cache initial message as description
	if req.InitialMessage != "" {
		session.SetDescription(req.InitialMessage)
	}

	// Create webhook payload Secret if webhook payload is provided
	if len(webhookPayload) > 0 {
		if err := m.createWebhookPayloadSecret(ctx, session, webhookPayload); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create webhook payload secret: %v", err)
			// Continue anyway - session will work without payload file
		}
	}

	// Ensure service account exists for team-scoped sessions (best-effort)
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.serviceAccountEnsurer != nil {
		if err := m.serviceAccountEnsurer.EnsureServiceAccount(ctx, req.TeamID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to ensure service account for team %s: %v", req.TeamID, err)
			// Continue anyway - session will work without service account
		}
	}

	// Create oneshot settings Secret if oneshot is enabled
	if req.Oneshot {
		if err := m.createOneshotSettingsSecret(ctx, session); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create oneshot settings secret: %v", err)
			// Continue anyway - session will work without oneshot hook
		}
	}

	// Build session settings once (used both for the Secret and the /provision payload).
	// When req.ProvisionSettings is provided (small-cluster / forwarding mode), use it
	// directly instead of resolving secrets from this cluster.
	var sessionSettings *sessionsettings.SessionSettings
	if req.ProvisionSettings != nil {
		sessionSettings = req.ProvisionSettings
	} else {
		sessionSettings = m.buildSessionSettings(ctx, session, req, webhookPayload)
	}

	// Serialise to JSON and cache in session for the watchSession /provision call.
	if provisionJSON, err := json.Marshal(sessionSettings); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to marshal session settings to JSON: %v", err)
	} else {
		session.SetProvisionPayload(provisionJSON)
	}
	session.SetProvisionSettings(sessionSettings)

	// Create Deployment
	if err := m.createDeployment(ctx, session, req); err != nil {
		if m.isPVCEnabled() {
			if delErr := m.deletePVC(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup PVC after deployment creation failure: %v", delErr)
			}
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create Deployment: %w", err)
	}
	log.Printf("[K8S_SESSION] Created Deployment %s for session %s", deploymentName, id)

	// Create Service
	if err := m.createService(ctx, session); err != nil {
		if delErr := m.deleteDeployment(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup Deployment after service creation failure: %v", delErr)
		}
		if m.isPVCEnabled() {
			if delErr := m.deletePVC(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup PVC after service creation failure: %v", delErr)
			}
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create Service: %w", err)
	}
	log.Printf("[K8S_SESSION] Created Service %s for session %s", serviceName, id)

	// Start watching session in background
	go m.watchSession(sessionCtx, session)

	// Log session start
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
	}
	if err := m.logger.LogSessionStart(id, repository); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session start: %v", err)
	}

	log.Printf("[K8S_SESSION] Session %s created successfully", id)
	return session, nil
}

// CreateStockSession creates a pre-warmed stock session (Deployment + Service)
// without calling /provision. The pod starts the agent-provisioner and waits
// for adoption via adoptStockSession, which sends the actual /provision call.
func (m *KubernetesSessionManager) CreateStockSession(ctx context.Context) error {
	id := uuid.New().String()
	deploymentName := fmt.Sprintf("agentapi-session-%s", id)
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)

	_, cancel := context.WithCancel(context.Background())

	// Stock sessions have no owner; the minimal request holds defaults only.
	minimalReq := &entities.RunServerRequest{}

	session := NewKubernetesSession(id, minimalReq, deploymentName, serviceName, pvcName,
		m.namespace, m.k8sConfig.BasePort, cancel, nil)
	session.SetIsStock(true)

	// Create PVC if enabled (required for Deployment volume mounts).
	if m.isPVCEnabled() {
		if err := m.createPVC(ctx, session); err != nil {
			cancel()
			return fmt.Errorf("failed to create stock PVC: %w", err)
		}
	}

	if err := m.createDeployment(ctx, session, minimalReq); err != nil {
		if m.isPVCEnabled() {
			if delErr := m.deletePVC(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup PVC after stock deployment creation failure: %v", delErr)
			}
		}
		cancel()
		return fmt.Errorf("failed to create stock deployment: %w", err)
	}
	if err := m.createService(ctx, session); err != nil {
		if delErr := m.deleteDeployment(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup deployment after stock service creation failure: %v", delErr)
		}
		if m.isPVCEnabled() {
			if delErr := m.deletePVC(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup PVC after stock service creation failure: %v", delErr)
			}
		}
		cancel()
		return fmt.Errorf("failed to create stock service: %w", err)
	}
	log.Printf("[K8S_SESSION] Stock session %s created successfully", id)
	return nil
}

// CountStockSessions returns the number of available (not being deleted) stock sessions.
func (m *KubernetesSessionManager) CountStockSessions(ctx context.Context) (int, error) {
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/stock=true,app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list stock services: %w", err)
	}
	count := 0
	for i := range svcs.Items {
		if svcs.Items[i].DeletionTimestamp == nil {
			count++
		}
	}
	return count, nil
}

// PurgeStockSessions deletes all existing pre-warmed stock sessions (Service,
// Deployment, PVC). Called by the stock inventory worker on startup to ensure
// that stale pods built from an old image are replaced with fresh ones.
// This also purges sessions stuck in the "claiming" state (stock=claiming) that
// were abandoned mid-adoption due to a crash or restart.
func (m *KubernetesSessionManager) PurgeStockSessions(ctx context.Context) error {
	// Use a set-based selector to match both stock=true (unclaimed) and
	// stock=claiming (abandoned mid-adoption) services.
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/stock in (true, claiming),app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return fmt.Errorf("failed to list stock services for purge: %w", err)
	}

	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	var purgeErrs []string
	for i := range svcs.Items {
		svc := &svcs.Items[i]
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		log.Printf("[STOCK_INVENTORY] Purging stock session %s", sessionID)

		// Delete Service
		if err := m.client.CoreV1().Services(m.namespace).Delete(ctx, svc.Name, deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("service %s: %v", svc.Name, err))
		}

		if sessionID == "" {
			continue
		}

		// Delete Deployment
		depName := fmt.Sprintf("agentapi-session-%s", sessionID)
		if err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, depName, deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("deployment %s: %v", depName, err))
		}

		// Delete PVC if enabled
		if m.isPVCEnabled() {
			pvcName := fmt.Sprintf("agentapi-session-%s-pvc", sessionID)
			if err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, pvcName, deleteOptions); err != nil && !errors.IsNotFound(err) {
				purgeErrs = append(purgeErrs, fmt.Sprintf("pvc %s: %v", pvcName, err))
			}
		}
	}

	if len(purgeErrs) > 0 {
		return fmt.Errorf("purge errors: %s", strings.Join(purgeErrs, "; "))
	}
	log.Printf("[STOCK_INVENTORY] Purged %d stock session(s)", len(svcs.Items))
	return nil
}

// findStockSession lists Services labeled agentapi.proxy/stock=true and returns the
// oldest available one (by CreationTimestamp, ascending). Oldest sessions have been
// warmed up the longest and are the most ready to serve.
// Returns (nil, nil) when no stock is available.
func (m *KubernetesSessionManager) findStockSession(ctx context.Context) (*corev1.Service, error) {
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/stock=true,app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list stock services: %w", err)
	}

	// Sort by creation time ascending so the oldest (longest-warmed) session is preferred.
	sort.Slice(svcs.Items, func(i, j int) bool {
		return svcs.Items[i].CreationTimestamp.Before(&svcs.Items[j].CreationTimestamp)
	})

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		if svc.DeletionTimestamp != nil {
			continue
		}
		if svc.Labels["agentapi.proxy/session-id"] == "" {
			continue
		}
		return svc, nil
	}
	return nil, nil
}

// claimStockService atomically claims a stock Service by transitioning the
// agentapi.proxy/stock label from "true" → "claiming" via an Update (which uses
// Kubernetes' resourceVersion-based optimistic locking).
// Using "claiming" instead of deleting the label ensures that ListSessions
// (which excludes any Service with the agentapi.proxy/stock key) never restores
// this Service with an empty user-id during the adoption window, preventing 403
// errors and ghost-session appearances on other proxy replicas.
// If another replica claims the same stock concurrently, the Update returns a
// Conflict error and the caller should fall back to creating a new session.
func (m *KubernetesSessionManager) claimStockService(ctx context.Context, svc *corev1.Service) (*corev1.Service, error) {
	if svc.Labels["agentapi.proxy/stock"] != "true" {
		return nil, fmt.Errorf("service %s is not a stock service", svc.Name)
	}
	// Transition to "claiming" so findStockSession (which filters stock=true) no
	// longer picks it up, while ListSessions (which requires !agentapi.proxy/stock)
	// also skips it until adoptStockSession removes the label entirely.
	svc.Labels["agentapi.proxy/stock"] = "claiming"
	// Update uses the ResourceVersion already set on svc for optimistic locking.
	updated, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to claim stock service %s: %w", svc.Name, err)
	}
	return updated, nil
}

// adoptStockSession takes a claimed stock Service and turns it into a regular session.
// It updates Service/Deployment labels and annotations, builds the provision payload,
// and starts watchStockSession in the background.
func (m *KubernetesSessionManager) adoptStockSession(
	ctx context.Context,
	req *entities.RunServerRequest,
	webhookPayload []byte,
	stockSvc *corev1.Service,
) (entities.Session, error) {
	stockID := stockSvc.Labels["agentapi.proxy/session-id"]
	deploymentName := fmt.Sprintf("agentapi-session-%s", stockID)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", stockID)

	sessionCtx, cancel := context.WithCancel(context.Background())

	// Build KubernetesSession reusing the stock's resource names.
	session := NewKubernetesSession(
		stockID,
		req,
		deploymentName,
		stockSvc.Name, // service name stays the same
		pvcName,
		m.namespace,
		m.k8sConfig.BasePort,
		cancel,
		webhookPayload,
	)

	// Cache initial message as description.
	if req.InitialMessage != "" {
		session.SetDescription(req.InitialMessage)
	}

	// Register session in memory.
	m.mutex.Lock()
	m.sessions[stockID] = session
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Adopting stock session %s in namespace %s", stockID, m.namespace)

	// Check whether the stock already has a PVC; if not, we leave it as EmptyDir.
	_, pvcErr := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Get(ctx, pvcName, metav1.GetOptions{})
	switch {
	case pvcErr == nil:
		log.Printf("[K8S_SESSION] Stock session %s has existing PVC %s, reusing it", stockID, pvcName)
	case errors.IsNotFound(pvcErr):
		log.Printf("[K8S_SESSION] Stock session %s has no PVC, using EmptyDir", stockID)
	default:
		log.Printf("[K8S_SESSION] Warning: failed to check PVC for stock session %s: %v", stockID, pvcErr)
	}

	// Create webhook payload Secret if provided.
	if len(webhookPayload) > 0 {
		if err := m.createWebhookPayloadSecret(ctx, session, webhookPayload); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create webhook payload secret for stock session %s: %v", stockID, err)
		}
	}

	// Ensure service account for team-scoped sessions (best-effort).
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.serviceAccountEnsurer != nil {
		if err := m.serviceAccountEnsurer.EnsureServiceAccount(ctx, req.TeamID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to ensure service account for team %s: %v", req.TeamID, err)
		}
	}

	// Create oneshot settings Secret if needed.
	if req.Oneshot {
		if err := m.createOneshotSettingsSecret(ctx, session); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create oneshot settings secret for stock session %s: %v", stockID, err)
		}
	}

	// Build session settings and cache provision payload.
	sessionSettings := m.buildSessionSettings(ctx, session, req, webhookPayload)
	if provisionJSON, err := json.Marshal(sessionSettings); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to marshal session settings to JSON: %v", err)
	} else {
		session.SetProvisionPayload(provisionJSON)
	}
	session.SetProvisionSettings(sessionSettings)

	// Update Service labels and annotations to reflect the new owner.
	now := time.Now()
	newLabels := m.buildLabels(session)
	annotations := map[string]string{
		"agentapi.proxy/created-at":      now.Format(time.RFC3339),
		"agentapi.proxy/updated-at":      now.Format(time.RFC3339),
		"agentapi.proxy/team-id":         req.TeamID,
		"agentapi.proxy/last-message-at": now.UTC().Format(time.RFC3339),
	}
	if req.AgentType != "" {
		annotations["agentapi.proxy/agent-type"] = req.AgentType
	}
	// Store initial message in annotation so all proxy replicas can read it immediately.
	if req.InitialMessage != "" {
		annotations["agentapi.proxy/initial-message"] = req.InitialMessage
	}

	currentSvc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, stockSvc.Name, metav1.GetOptions{})
	if err != nil {
		m.cleanupSession(stockID)
		cancel()
		return nil, fmt.Errorf("failed to get stock service for label update: %w", err)
	}
	currentSvc.Labels = newLabels
	if currentSvc.Annotations == nil {
		currentSvc.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		currentSvc.Annotations[k] = v
	}
	if _, err := m.client.CoreV1().Services(m.namespace).Update(ctx, currentSvc, metav1.UpdateOptions{}); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to update stock service labels for session %s: %v", stockID, err)
	}

	// Update Deployment metadata labels only (NOT spec.template.labels) to reflect the new owner.
	// Updating spec.template.labels would trigger a Kubernetes rolling update, restarting the pod
	// and making the agent-provisioner unavailable during the critical /provision window.
	currentDep, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to get stock deployment for label update: %v", err)
	} else {
		currentDep.Labels = newLabels
		if _, err := m.client.AppsV1().Deployments(m.namespace).Update(ctx, currentDep, metav1.UpdateOptions{}); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to update stock deployment labels for session %s: %v", stockID, err)
		}
	}

	// Start background watch — Pod is already running so /provision is sent immediately.
	go m.watchStockSession(sessionCtx, session)

	// Log session start.
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
	}
	if err := m.logger.LogSessionStart(stockID, repository); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session start for stock session %s: %v", stockID, err)
	}

	log.Printf("[K8S_SESSION] Stock session %s adopted successfully", stockID)
	return session, nil
}

// watchStockSession monitors a stock-adopted session.
// Unlike watchSession, it skips the ReadyReplicas wait (Pod is already running)
// and immediately sends /provision to the agent-provisioner.
func (m *KubernetesSessionManager) watchStockSession(ctx context.Context, session *KubernetesSession) {
	defer func() {
		log.Printf("[K8S_SESSION] Stock session %s watch ended", session.id)
	}()

	session.SetStatus("starting")

	provisionerURL := fmt.Sprintf("http://%s:%d", session.ServiceDNS(), provisionerPort)

	// Wait for the pod to be ready before sending /provision.
	// Although stock sessions are pre-warmed, the pod may not yet be ready if
	// the session was adopted immediately after creation (e.g. inventory was
	// just replenished). Reuse the same ready-wait loop used by watchSession.
	timeout := time.After(time.Duration(m.k8sConfig.PodStartTimeout) * time.Second)
	readyTicker := time.NewTicker(2 * time.Second)
	podReady := false
	for !podReady {
		select {
		case <-ctx.Done():
			readyTicker.Stop()
			log.Printf("[K8S_SESSION] Stock session %s context cancelled while waiting for pod", session.id)
			return
		case <-timeout:
			readyTicker.Stop()
			log.Printf("[K8S_SESSION] Stock session %s startup timeout", session.id)
			session.SetStatus("timeout")
			return
		case <-readyTicker.C:
			dep, err := m.client.AppsV1().Deployments(m.namespace).Get(
				context.Background(), session.DeploymentName(), metav1.GetOptions{})
			if err == nil && dep.Status.ReadyReplicas > 0 {
				podReady = true
			}
		}
	}
	readyTicker.Stop()
	log.Printf("[K8S_SESSION] Stock session %s: Pod is ready, sending /provision", session.id)

	// POST /provision with retry (postProvision already handles transient errors).
	if err := m.postProvision(ctx, provisionerURL, session.ProvisionPayload()); err != nil {
		log.Printf("[K8S_SESSION] Failed to POST /provision for stock session %s: %v", session.id, err)
		session.SetStatus("error")
		return
	}

	// Wait for provisioner to finish setup and start agentapi.
	log.Printf("[K8S_SESSION] Waiting for agent-provisioner to become ready for stock session %s", session.id)
	if err := m.waitForProvisioner(ctx, provisionerURL); err != nil {
		log.Printf("[K8S_SESSION] Provisioner error for stock session %s: %v", session.id, err)
		session.SetStatus("error")
		return
	}

	// Persist settings Secret for automatic re-provisioning on Pod restart.
	if ps := session.ProvisionSettings(); ps != nil {
		if err := m.createSessionSettingsSecretFromSettings(ctx, session, session.Request(), ps); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create settings secret for stock session %s: %v", session.id, err)
		}
	}

	session.SetStatus("active")
	log.Printf("[K8S_SESSION] Stock session %s is now active", session.id)

	// Continue watching deployment status for the lifetime of the session.
	m.watchDeploymentStatus(ctx, session)
}

// GetSession returns a session by ID
// If the session is not in memory, it attempts to restore from Kubernetes Service
func (m *KubernetesSessionManager) GetSession(id string) entities.Session {
	// First, check memory
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	// Try to restore from Kubernetes Service to check for stale user-id even when
	// the session is in memory.  A session cached while it was a stock pod will have
	// an empty user-id; once the Service is updated with the real owner after
	// adoption, we repair the in-memory entry here so authorization passes without
	// requiring a proxy restart.
	if exists && session.UserID() != "" {
		return session
	}

	// Try to restore from Kubernetes Service
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	svc, err := m.client.CoreV1().Services(m.namespace).Get(
		context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("[K8S_SESSION] Failed to get service %s: %v", serviceName, err)
		}
		// If we can't reach K8s but have the session in memory, still return it
		if exists {
			return session
		}
		return nil
	}

	// Don't restore if Service is being deleted
	if svc.DeletionTimestamp != nil {
		if exists {
			return session
		}
		return nil
	}

	// If session is already in memory but had a stale empty user-id, repair it
	// from the Service labels instead of doing a full re-restore (which would
	// start duplicate goroutines).
	if exists && session.UserID() == "" {
		if svcUserID := svc.Labels["agentapi.proxy/user-id"]; svcUserID != "" {
			log.Printf("[K8S_SESSION] GetSession: repairing stale user-id for session %s (was empty, now %s)", id, svcUserID)
			session.SetUserID(svcUserID)
		}
		return session
	}

	// Restore session from Service
	return m.restoreSessionFromService(svc)
}

// ListSessions returns all sessions matching the filter
// Sessions are retrieved from Kubernetes Services to survive proxy restarts
func (m *KubernetesSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	// Build label selector for Kubernetes API filtering
	labelSelector := m.buildLabelSelector(filter)

	// Get services from Kubernetes API
	services, err := m.client.CoreV1().Services(m.namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list services: %v", err)
		return []entities.Session{}
	}

	// Batch fetch all deployments to avoid N+1 API calls (using same filter)
	deployments, err := m.client.AppsV1().Deployments(m.namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list deployments: %v", err)
		// Continue without deployment info - sessions will have "unknown" status
	}

	// Build deployment map by session ID for O(1) lookup
	deploymentMap := make(map[string]*appsv1.Deployment)
	if deployments != nil {
		for i := range deployments.Items {
			dep := &deployments.Items[i]
			if sessionID := dep.Labels["agentapi.proxy/session-id"]; sessionID != "" {
				deploymentMap[sessionID] = dep
			}
		}
	}

	var result []entities.Session
	for i := range services.Items {
		svc := &services.Items[i]

		// Skip Services that are being deleted to avoid "ghost sessions"
		// that appear in list results but are already deleted
		// (GetSession also has this check for consistency)
		if svc.DeletionTimestamp != nil {
			continue
		}

		// Extract session info from Service labels
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}

		userID := svc.Labels["agentapi.proxy/user-id"]

		// Apply UserID filter
		if filter.UserID != "" && userID != filter.UserID {
			continue
		}

		// Get or restore session using pre-fetched deployment
		session := m.getOrRestoreSessionWithDeployment(svc, deploymentMap[sessionID])
		if session == nil {
			continue
		}

		// Apply Status filter
		if filter.Status != "" && session.Status() != filter.Status {
			continue
		}

		// Apply Scope filter
		if filter.Scope != "" && session.Scope() != filter.Scope {
			continue
		}

		// Apply TeamID filter
		if filter.TeamID != "" && session.TeamID() != filter.TeamID {
			continue
		}

		// Apply TeamIDs filter (for team-scoped sessions, check if session's team is in user's teams)
		if len(filter.TeamIDs) > 0 && session.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if session.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}

		// Apply Tag filters
		// Tags are compared after sanitization since they are stored as sanitized labels in Kubernetes
		if len(filter.Tags) > 0 {
			matchAllTags := true
			sessionTags := session.Tags()
			for tagKey, tagValue := range filter.Tags {
				sessionTagValue, exists := sessionTags[tagKey]
				// Compare sanitized values since Kubernetes labels are sanitized
				if !exists || sanitizeLabelValue(sessionTagValue) != sanitizeLabelValue(tagValue) {
					matchAllTags = false
					break
				}
			}
			if !matchAllTags {
				continue
			}
		}

		result = append(result, session)
	}

	return result
}

// getOrRestoreSessionWithDeployment gets a session from memory or restores it from Service
// using a pre-fetched deployment to avoid additional API calls
func (m *KubernetesSessionManager) getOrRestoreSessionWithDeployment(svc *corev1.Service, deployment *appsv1.Deployment) *KubernetesSession {
	// Don't restore if Service is being deleted (same guard as GetSession)
	if svc.DeletionTimestamp != nil {
		return nil
	}

	sessionID := svc.Labels["agentapi.proxy/session-id"]

	// Check if session exists in memory
	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()

	if exists {
		// If the in-memory session was cached when this Service was still a stock
		// session (user-id was empty at restore time), and the Service now has a
		// real owner, repair the user-id in-place so authorization checks pass.
		// This avoids the need for a proxy restart to recover from the race where
		// another replica restored the session before stock adoption completed.
		if session.UserID() == "" {
			if svcUserID := svc.Labels["agentapi.proxy/user-id"]; svcUserID != "" {
				log.Printf("[K8S_SESSION] Repairing stale user-id for session %s (was empty, now %s)", sessionID, svcUserID)
				session.SetUserID(svcUserID)
			}
		}
		return session
	}

	// Restore session from Service with pre-fetched deployment
	return m.restoreSessionFromServiceWithDeployment(svc, deployment)
}

// DeleteSession stops and removes a session
// If the session is not in memory, it attempts to restore from Kubernetes Service first
func (m *KubernetesSessionManager) DeleteSession(id string) error {
	// First, check memory
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	// If not in memory, try to get from GetSession (which will restore from Service)
	if !exists {
		if restored := m.GetSession(id); restored != nil {
			m.mutex.RLock()
			session, exists = m.sessions[id]
			m.mutex.RUnlock()
		}
	}

	if !exists || session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	log.Printf("[K8S_SESSION] Deleting session %s", id)

	// Invoke registered handlers BEFORE cancelling the context or removing Kubernetes resources.
	// At this point the session's Service endpoint is still reachable (e.g. for GetMessages).
	m.handlersMutex.RLock()
	handlers := make([]SessionDeletedHandler, len(m.onSessionDeletedHandlers))
	copy(handlers, m.onSessionDeletedHandlers)
	m.handlersMutex.RUnlock()

	if len(handlers) > 0 {
		handlerCtx, handlerCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer handlerCancel()
		for _, h := range handlers {
			h(handlerCtx, session)
		}
	}

	// Cancel context to trigger cleanup
	if session != nil {
		session.Cancel()
	}

	// Delete Kubernetes resources
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.k8sConfig.PodStopTimeout)*time.Second)
	defer cancel()

	if err := m.deleteSessionResources(ctx, session); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to delete session resources: %v", err)
	}

	// Remove session from map
	m.cleanupSession(id)

	// Log session end
	if err := m.logger.LogSessionEnd(id, 0); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session end: %v", err)
	}

	log.Printf("[K8S_SESSION] Session %s deleted successfully", id)
	return nil
}

// Shutdown gracefully stops all sessions
// Note: This does NOT delete Kubernetes resources (Deployment, Service, PVC, Secret).
// Resources are preserved so sessions can be restored when the proxy restarts.
// Use DeleteSession to explicitly delete a session and its resources.
func (m *KubernetesSessionManager) Shutdown(timeout time.Duration) error {
	m.mutex.Lock()
	sessionCount := len(m.sessions)
	// Clear in-memory sessions (resources remain in Kubernetes)
	m.sessions = make(map[string]*KubernetesSession)
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Shutting down, preserving %d session(s) in Kubernetes for recovery", sessionCount)
	return nil
}

// SendMessage sends a message to an existing session
func (m *KubernetesSessionManager) SendMessage(ctx context.Context, id string, message string) error {
	// Get session
	session := m.GetSession(id)
	if session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	// Check session status
	status := session.Status()
	if status != "active" && status != "starting" {
		return fmt.Errorf("session is not active: status=%s", status)
	}

	// Build service name and endpoint URL
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/message",
		serviceName,
		m.namespace,
		m.k8sConfig.BasePort,
	)

	// Create payload
	payload := map[string]interface{}{
		"content": message,
		"type":    "user",
	}

	// Marshal JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request with retry logic
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			// Update last-message-at in memory and on the Kubernetes Service annotation.
			now := time.Now()
			if ks, ok := session.(*KubernetesSession); ok {
				ks.SetLastMessageAt(now)
			}
			svcName := fmt.Sprintf("agentapi-session-%s-svc", id)
			if patchErr := m.patchLastMessageAt(context.Background(), svcName, now); patchErr != nil {
				log.Printf("[K8S_SESSION] Failed to update last-message-at for session %s: %v", id, patchErr)
			}
			log.Printf("[K8S_SESSION] Successfully sent message to session %s", id)
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			_ = resp.Body.Close()
		}
		if i < 2 {
			time.Sleep(1 * time.Second)
		}
	}

	return fmt.Errorf("failed to send message after 3 retries: %w", lastErr)
}

// StopAgent sends a stop_agent action to the running agent in the session via the
// claude-agentapi POST /action endpoint. This terminates the running agent task
// without deleting the session.
func (m *KubernetesSessionManager) StopAgent(ctx context.Context, id string) error {
	// Get session
	session := m.GetSession(id)
	if session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	// Check session status
	status := session.Status()
	if status != "active" && status != "starting" {
		return fmt.Errorf("session is not active: status=%s", status)
	}

	// Build service name and endpoint URL for the claude-agentapi /action endpoint
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/action",
		serviceName,
		m.namespace,
		m.k8sConfig.BasePort,
	)

	// Send stop_agent action as defined in the claude-agentapi OpenAPI spec
	payload := map[string]interface{}{
		"type": "stop_agent",
	}

	// Marshal JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal stop_agent payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send stop_agent signal: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code from stop_agent signal: %d", resp.StatusCode)
	}

	log.Printf("[K8S_SESSION] Successfully sent stop_agent signal to session %s", id)
	return nil
}

// GetMessages retrieves conversation history from a session
func (m *KubernetesSessionManager) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	// Get session
	session := m.GetSession(id)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}

	// Build service name and endpoint URL
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/messages",
		serviceName,
		m.namespace,
		m.k8sConfig.BasePort,
	)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse response
	var response struct {
		Messages []portrepos.Message `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("[K8S_SESSION] Successfully retrieved %d messages from session %s", len(response.Messages), id)
	return response.Messages, nil
}

// createPVC creates a PersistentVolumeClaim for the session
func (m *KubernetesSessionManager) createPVC(ctx context.Context, session *KubernetesSession) error {
	storageSize := resource.MustParse(m.k8sConfig.PVCStorageSize)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.PVCName(),
			Namespace: m.namespace,
			Labels:    m.buildLabels(session),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}

	// Set storage class if specified
	if m.k8sConfig.PVCStorageClass != "" {
		pvc.Spec.StorageClassName = &m.k8sConfig.PVCStorageClass
	}

	_, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return err
}

// createDeployment creates a Deployment for the session
func (m *KubernetesSessionManager) createDeployment(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) error {
	labels := m.buildLabels(session)
	envVars := m.buildEnvVars(session, req)
	replicas := int32(1)

	// Parse resource requirements
	cpuRequest := resource.MustParse(m.k8sConfig.CPURequest)
	cpuLimit := resource.MustParse(m.k8sConfig.CPULimit)
	memoryRequest := resource.MustParse(m.k8sConfig.MemoryRequest)
	memoryLimit := resource.MustParse(m.k8sConfig.MemoryLimit)

	// No init containers — setup is performed by the main container on startup

	// Determine working directory
	// Always use /home/agentapi/workdir as base; clone-repo will create /home/agentapi/workdir/repo
	// Setting workingDir to repo path would cause Kubernetes to pre-create the dir as root
	workingDir := "/home/agentapi/workdir"

	// Build envFrom for GitHub secrets
	// Two secrets are used:
	// - GitHubSecretName: Contains GITHUB_TOKEN, GITHUB_APP_PEM, GITHUB_APP_ID, GITHUB_INSTALLATION_ID (authentication)
	// - GitHubConfigSecretName: Contains GITHUB_API, GITHUB_URL (configuration for Enterprise Server)
	var envFrom []corev1.EnvFromSource

	if req.GithubToken != "" {
		// When params.github_token is provided:
		// - GITHUB_TOKEN is embedded directly in session-settings env (no per-session secret)
		// - Mount GitHubConfigSecretName for GITHUB_API/GITHUB_URL settings only
		if m.k8sConfig.GitHubConfigSecretName != "" {
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: m.k8sConfig.GitHubConfigSecretName,
					},
					Optional: boolPtr(true),
				},
			})
			log.Printf("[K8S_SESSION] Mounting GitHub config Secret %s for session %s", m.k8sConfig.GitHubConfigSecretName, session.id)
		}
	} else if m.k8sConfig.GitHubSecretName != "" {
		// When params.github_token is NOT provided:
		// - Mount GitHubSecretName for full GitHub App authentication
		// - Also mount GitHubConfigSecretName (config values will override auth secret if same keys exist)
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.GitHubSecretName,
				},
				Optional: boolPtr(true),
			},
		})

		// Mount GitHub config Secret if available (for any additional config)
		if m.k8sConfig.GitHubConfigSecretName != "" {
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: m.k8sConfig.GitHubConfigSecretName,
					},
					Optional: boolPtr(true),
				},
			})
		}
	}

	// Build container spec.
	// The container runs agent-provisioner, which starts the HTTP server on
	// provisionerPort and waits for POST /provision from the proxy server.
	container := corev1.Container{
		Name:            "agentapi",
		Image:           m.k8sConfig.Image,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		WorkingDir:      workingDir,
		Ports: []corev1.ContainerPort{
			{
				// agentapi port – available only after provisioning completes.
				Name:          "http",
				ContainerPort: int32(m.k8sConfig.BasePort),
				Protocol:      corev1.ProtocolTCP,
			},
			{
				// agent-provisioner port – available immediately on Pod start.
				Name:          "provisioner",
				ContainerPort: provisionerPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:     envVars,
		EnvFrom: envFrom,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuRequest,
				corev1.ResourceMemory: memoryRequest,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    cpuLimit,
				corev1.ResourceMemory: memoryLimit,
			},
		},
		VolumeMounts: m.buildMainContainerVolumeMounts(session),
		// Run agent-provisioner instead of the inline shell setup+agentapi script.
		Command: []string{"agentapi-proxy"},
		Args:    []string{"agent-provisioner"},
		// Probes target /healthz on the provisioner port (always-200) so that
		// the pod becomes Ready as soon as agent-provisioner is listening.
		// The proxy's watchSession goroutine handles waiting for the actual
		// provisioning completion (GET /status → "ready").
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(provisionerPort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(provisionerPort),
				},
			},
			InitialDelaySeconds: 2,
			PeriodSeconds:       2,
		},
	}

	// Build volumes
	volumes := m.buildVolumes(session)

	// Build containers list (main container only)
	// Note: credentials-sync is now handled as a goroutine inside agent-provisioner
	// (pkg/provisioner/provision.go) after user context is established, so the
	// UserID is always set correctly even for stock pool pods.
	containers := []corev1.Container{container}

	// Note: Initial message is now sent by agent-provisioner internally after agentapi
	// becomes ready. The initial-message-sender sidecar has been removed.

	// Note: The slack-integration sidecar (claude-posts) has been removed.
	// claude-posts is now launched as a subprocess inside the agentapi container
	// by agent-provisioner (pkg/provisioner/provision.go) after agentapi starts.
	// This avoids running claude-posts twice when stock sessions are used.

	// otelcol runs as a subprocess inside the agentapi container (in-process mode).
	// No sidecar is added here; the provisioner starts otelcol after user context
	// is established so that metrics labels are correct even with stock sessions.

	// Convert config tolerations to corev1 tolerations
	var tolerations []corev1.Toleration
	for _, t := range m.k8sConfig.Tolerations {
		toleration := corev1.Toleration{
			Key:      t.Key,
			Operator: corev1.TolerationOperator(t.Operator),
			Value:    t.Value,
			Effect:   corev1.TaintEffect(t.Effect),
		}
		if t.TolerationSeconds != nil {
			toleration.TolerationSeconds = t.TolerationSeconds
		}
		tolerations = append(tolerations, toleration)
	}

	// Build pod annotations
	podAnnotations := make(map[string]string)

	// Add Prometheus scrape annotations for otelcol sidecar if enabled
	if m.k8sConfig.OtelCollectorEnabled {
		exporterPort := 9090
		if m.k8sConfig.OtelCollectorExporterPort > 0 {
			exporterPort = m.k8sConfig.OtelCollectorExporterPort
		}

		podAnnotations["prometheus.io/scrape"] = "true"
		podAnnotations["prometheus.io/port"] = fmt.Sprintf("%d", exporterPort)
		podAnnotations["prometheus.io/path"] = "/metrics"
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.DeploymentName(),
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"agentapi.proxy/session-id": session.id,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "agentapi-proxy-session",
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    int64Ptr(999),
						RunAsUser:  int64Ptr(999),
						RunAsGroup: int64Ptr(999),
					},
					Containers:   containers,
					Volumes:      volumes,
					NodeSelector: m.k8sConfig.NodeSelector,
					Tolerations:  tolerations,
				},
			},
		},
	}

	_, err := m.client.AppsV1().Deployments(m.namespace).Create(ctx, deployment, metav1.CreateOptions{})
	return err
}

// NOTE: The credentialsSyncScript / credentialsSyncSidecarImage / buildCredentialsSyncSidecar
// have been removed. Credentials sync is now handled as a goroutine inside agent-provisioner
// (pkg/provisioner/provision.go) after user context is established. This ensures the
// UserID is always available (stock pool pods have an empty UserID at pod creation time).

// NOTE: The initialMessageSenderScript and buildInitialMessageSenderSidecar have been
// removed. Initial message sending is now handled inside agent-provisioner
// (pkg/provisioner/provision.go) after agentapi starts.

// NOTE: buildSlackSidecar (claude-posts sidecar) has been removed.
// claude-posts is launched as a subprocess inside the agentapi container by
// agent-provisioner (pkg/provisioner/provision.go). Running it as a sidecar
// caused duplicate execution alongside the in-process launch.

// defaultSlackBotTokenSecretKey is the default key within the Secret that holds the Slack bot token
const defaultSlackBotTokenSecretKey = "bot-token"

// createWebhookPayloadSecret creates a Secret containing the webhook payload JSON
func (m *KubernetesSessionManager) createWebhookPayloadSecret(
	ctx context.Context,
	session *KubernetesSession,
	payload []byte,
) error {
	secretName := fmt.Sprintf("%s-webhook-payload", session.ServiceName())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.Request().UserID),
				"agentapi.proxy/resource":   "webhook-payload",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"payload.json": payload,
		},
	}

	_, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create webhook payload secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created webhook payload Secret %s for session %s", secretName, session.id)
	return nil
}

// deleteWebhookPayloadSecret deletes the webhook payload Secret for a session
func (m *KubernetesSessionManager) deleteWebhookPayloadSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-webhook-payload", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete webhook payload secret: %w", err)
	}
	return nil
}

// getInitialMessageFromSecret retrieves the initial message from the session-settings Secret.
// The initial message is stored as the "initial_message" field inside "settings.yaml".
// serviceName follows the pattern "agentapi-session-{id}-svc"; the settings secret is "agentapi-session-{id}-settings".
func (m *KubernetesSessionManager) getInitialMessageFromSecret(ctx context.Context, serviceName string) string {
	settingsSecretName := strings.TrimSuffix(serviceName, "-svc") + "-settings"
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	yamlData, ok := secret.Data["settings.yaml"]
	if !ok {
		return ""
	}
	settings, err := sessionsettings.LoadSettingsFromBytes(yamlData)
	if err != nil {
		return ""
	}
	return settings.InitialMessage
}

// getSessionMetaFromSecret reads the settings secret and returns the SessionMeta
// (including MemoryKey, Teams, AgentType, Oneshot etc.) for session restore.
func (m *KubernetesSessionManager) getSessionMetaFromSecret(ctx context.Context, serviceName string) *sessionsettings.SessionMeta {
	settingsSecretName := strings.TrimSuffix(serviceName, "-svc") + "-settings"
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	yamlData, ok := secret.Data["settings.yaml"]
	if !ok {
		return nil
	}
	settings, err := sessionsettings.LoadSettingsFromBytes(yamlData)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to parse settings.yaml from secret %s: %v", settingsSecretName, err)
		return nil
	}
	return &settings.Session
}

// createOneshotSettingsSecret creates a Secret containing settings.json with Stop hook
// This is used when oneshot is enabled to automatically delete the session after stopping
func (m *KubernetesSessionManager) createOneshotSettingsSecret(
	ctx context.Context,
	session *KubernetesSession,
) error {
	secretName := fmt.Sprintf("%s-oneshot-settings", session.ServiceName())

	// Create settings.json with Stop hook
	settingsJSON := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []map[string]interface{}{
				{
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": "agentapi-proxy client delete-session --confirm",
						},
					},
				},
			},
		},
	}

	settingsData, err := json.Marshal(settingsJSON)
	if err != nil {
		return fmt.Errorf("failed to marshal oneshot settings: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.Request().UserID),
				"agentapi.proxy/resource":   "oneshot-settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"settings.json": settingsData,
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create oneshot settings secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created oneshot settings Secret %s for session %s", secretName, session.id)
	return nil
}

// buildVolumes builds the volume configuration for the session pod
func (m *KubernetesSessionManager) buildVolumes(session *KubernetesSession) []corev1.Volume {
	// Build workdir volume - use PVC if enabled, otherwise EmptyDir
	var workdirVolume corev1.Volume
	if m.isPVCEnabled() {
		workdirVolume = corev1.Volume{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: session.PVCName(),
				},
			},
		}
	} else {
		workdirVolume = corev1.Volume{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
	}

	// Note: credentials are no longer mounted as a volume. Instead they are
	// embedded in SessionSettings.Credentials and written to ~/.codex/auth.json
	// by the provisioner at startup. The runCredentialsSync goroutine then
	// watches the file and syncs any changes back to the Secret.

	volumes := []corev1.Volume{
		// Workdir volume (PVC or EmptyDir based on configuration)
		workdirVolume,
		// dot-claude EmptyDir – used by main container for Claude Code settings
		{
			Name: "dot-claude",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Add notification subscription Secret volume (source for init container)
	// Secret name follows the pattern: notification-subscriptions-{userID}
	notificationSecretName := fmt.Sprintf("notification-subscriptions-%s", sanitizeLabelValue(session.Request().UserID))
	volumes = append(volumes, corev1.Volume{
		Name: "notification-subscriptions-source",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: notificationSecretName,
				Optional:   boolPtr(true), // Optional - user may not have subscriptions
			},
		},
	})

	// session-settings Secret – optional volume for Pod restart auto-provisioning.
	// Created after successful provisioning; not present on first startup.
	sessionSettingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)
	optionalTrue := true
	volumes = append(volumes, corev1.Volume{
		Name: "session-settings",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: sessionSettingsSecretName,
				Optional:   &optionalTrue,
			},
		},
	})

	// Note: The "initial-message-state" EmptyDir volume is no longer needed because
	// the initial-message-sender sidecar has been removed. Initial message sending
	// is now handled internally by agent-provisioner.

	// Add webhook payload volume if webhook payload is provided
	if len(session.WebhookPayload()) > 0 {
		webhookPayloadSecretName := fmt.Sprintf("%s-webhook-payload", session.ServiceName())
		volumes = append(volumes, corev1.Volume{
			Name: "webhook-payload",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: webhookPayloadSecretName,
					Optional:   boolPtr(true),
				},
			},
		})
	}

	// No otelcol ConfigMap volume needed: otelcol runs as an in-process subprocess
	// and generates its config file at /tmp/otelcol-config.yaml at provisioning time.

	// Add EmptyDir for claude-agentapi history output
	volumes = append(volumes, corev1.Volume{
		Name: "claude-agentapi-history",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumes
}

// createService creates a Service for the session
func (m *KubernetesSessionManager) createService(ctx context.Context, session *KubernetesSession) error {
	annotations := map[string]string{
		"agentapi.proxy/created-at": session.startedAt.Format(time.RFC3339),
		"agentapi.proxy/updated-at": session.startedAt.Format(time.RFC3339),
		"agentapi.proxy/team-id":    session.Request().TeamID, // Store original team_id (unsanitized)
	}
	if session.Request().AgentType != "" {
		annotations["agentapi.proxy/agent-type"] = session.Request().AgentType
	}
	// Store initial message in annotation so all proxy replicas can read it immediately,
	// without waiting for the settings Secret (which is created asynchronously).
	if session.Request().InitialMessage != "" {
		annotations["agentapi.proxy/initial-message"] = session.Request().InitialMessage
	}
	// Record the initial message time as the last message time for all sessions.
	// This annotation is updated by SendMessage when follow-up messages arrive.
	annotations["agentapi.proxy/last-message-at"] = session.startedAt.UTC().Format(time.RFC3339)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        session.ServiceName(),
			Namespace:   m.namespace,
			Labels:      m.buildLabels(session),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"agentapi.proxy/session-id": session.id,
			},
			Ports: m.buildServicePorts(session),
		},
	}

	_, err := m.client.CoreV1().Services(m.namespace).Create(ctx, service, metav1.CreateOptions{})
	return err
}

// watchSession monitors the session deployment status
func (m *KubernetesSessionManager) watchSession(ctx context.Context, session *KubernetesSession) {
	defer func() {
		log.Printf("[K8S_SESSION] Session %s watch ended", session.id)
	}()

	// Wait for deployment to be ready
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(time.Duration(m.k8sConfig.PodStartTimeout) * time.Second)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[K8S_SESSION] Session %s context cancelled", session.id)
			return

		case <-timeout:
			log.Printf("[K8S_SESSION] Session %s startup timeout", session.id)
			session.SetStatus("timeout")
			return

		case <-ticker.C:
			deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
				context.Background(), session.DeploymentName(), metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					log.Printf("[K8S_SESSION] Deployment %s not found, session may have been deleted", session.DeploymentName())
					return
				}
				log.Printf("[K8S_SESSION] Error getting deployment: %v", err)
				continue
			}

			// Check deployment status
			if deployment.Status.ReadyReplicas > 0 {
				session.SetStatus("starting")
				log.Printf("[K8S_SESSION] Session %s Pod is ready, sending /provision to agent-provisioner", session.id)

				provisionerURL := fmt.Sprintf("http://%s:%d", session.ServiceDNS(), provisionerPort)

				// POST /provision to agent-provisioner (with retry).
				if err := m.postProvision(ctx, provisionerURL, session.ProvisionPayload()); err != nil {
					log.Printf("[K8S_SESSION] Failed to POST /provision for session %s: %v – will retry", session.id, err)
					// Retry on next ticker tick.
					continue
				}

				// Wait for provisioner to complete (agentapi running + initial message sent).
				log.Printf("[K8S_SESSION] Waiting for agent-provisioner to become ready for session %s", session.id)
				if err := m.waitForProvisioner(ctx, provisionerURL); err != nil {
					log.Printf("[K8S_SESSION] Provisioner error for session %s: %v", session.id, err)
					session.SetStatus("error")
					return
				}

				// Create settings Secret for Pod restart recovery (after successful provisioning).
				if ps := session.ProvisionSettings(); ps != nil {
					if err := m.createSessionSettingsSecretFromSettings(ctx, session, session.Request(), ps); err != nil {
						log.Printf("[K8S_SESSION] Warning: failed to create settings secret for session %s: %v", session.id, err)
						// Non-fatal: session works without it, but Pod restart will require re-provisioning
					}
				}

				session.SetStatus("active")
				log.Printf("[K8S_SESSION] Session %s is now active", session.id)

				// Continue watching for changes.
				m.watchDeploymentStatus(ctx, session)
				return
			}

			session.SetStatus("starting")
		}
	}
}

// postProvision POSTs the provision payload to the agent-provisioner's /provision endpoint.
// It retries up to 10 times with 2-second intervals to handle the race between
// the Service routing and the provisioner being ready to accept connections.
func (m *KubernetesSessionManager) postProvision(ctx context.Context, provisionerURL string, payload []byte) error {
	client := &http.Client{Timeout: 10 * time.Second}
	url := provisionerURL + "/provision"

	const maxRetries = 10
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("failed to build /provision request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusAccepted, http.StatusOK:
				// 202 = provisioning started, 200 = already ready (idempotent)
				return nil
			case http.StatusConflict:
				// 409 = provisioning already in progress – treat as success
				return nil
			}
			log.Printf("[K8S_SESSION] POST /provision returned HTTP %d, retrying (%d/%d)", resp.StatusCode, i+1, maxRetries)
		} else {
			log.Printf("[K8S_SESSION] POST /provision error: %v, retrying (%d/%d)", err, i+1, maxRetries)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("failed to POST /provision after %d retries", maxRetries)
}

// waitForProvisioner polls the agent-provisioner's /status endpoint until the
// provisioning state is "ready" or "error", or until ctx is cancelled.
func (m *KubernetesSessionManager) waitForProvisioner(ctx context.Context, provisionerURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	statusURL := provisionerURL + "/status"

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for provisioner")

		case <-ticker.C:
			resp, err := client.Get(statusURL)
			if err != nil {
				log.Printf("[K8S_SESSION] GET /status error: %v", err)
				continue
			}

			var statusResp struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&statusResp); decodeErr != nil {
				_ = resp.Body.Close()
				log.Printf("[K8S_SESSION] Failed to decode /status response: %v", decodeErr)
				continue
			}
			_ = resp.Body.Close()

			switch statusResp.Status {
			case "ready":
				return nil
			case "error":
				return fmt.Errorf("provisioner reported error: %s", statusResp.Message)
			default:
				log.Printf("[K8S_SESSION] Provisioner status: %s", statusResp.Status)
			}
		}
	}
}

// watchDeploymentStatus continuously watches the deployment status after it becomes ready
func (m *KubernetesSessionManager) watchDeploymentStatus(ctx context.Context, session *KubernetesSession) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
				context.Background(), session.DeploymentName(), metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					session.SetStatus("stopped")
					return
				}
				continue
			}

			if deployment.Status.ReadyReplicas == 0 {
				session.SetStatus("unhealthy")
			} else {
				session.SetStatus("active")
			}
		}
	}
}

// deleteSessionResources deletes all Kubernetes resources for a session
func (m *KubernetesSessionManager) deleteSessionResources(ctx context.Context, session *KubernetesSession) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	var errs []string

	// Delete Service
	err := m.client.CoreV1().Services(m.namespace).Delete(ctx, session.ServiceName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("service: %v", err))
	}

	// Delete Deployment
	err = m.client.AppsV1().Deployments(m.namespace).Delete(ctx, session.DeploymentName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("deployment: %v", err))
	}

	// Delete PVC if enabled
	if m.isPVCEnabled() {
		err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.PVCName(), deleteOptions)
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Sprintf("pvc: %v", err))
		}
	}

	// Delete webhook payload Secret
	if err := m.deleteWebhookPayloadSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("webhook-payload-secret: %v", err))
	}

	// Delete session settings Secret
	if err := m.deleteSessionSettingsSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("session-settings-secret: %v", err))
	}

	// Delete oneshot settings Secret (bug fix - was not being deleted before)
	if err := m.deleteOneshotSettingsSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("oneshot-settings-secret: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete resources: %s", strings.Join(errs, ", "))
	}

	return nil
}

// deleteDeployment deletes the deployment for a session
func (m *KubernetesSessionManager) deleteDeployment(ctx context.Context, session *KubernetesSession) error {
	return m.client.AppsV1().Deployments(m.namespace).Delete(ctx, session.DeploymentName(), metav1.DeleteOptions{})
}

// deletePVC deletes the PVC for a session
func (m *KubernetesSessionManager) deletePVC(ctx context.Context, session *KubernetesSession) error {
	return m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.PVCName(), metav1.DeleteOptions{})
}

// cleanupSession removes a session from the internal map
func (m *KubernetesSessionManager) cleanupSession(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.sessions, id)
}

// buildLabelSelector builds a Kubernetes label selector string from entities.SessionFilter
// This allows filtering at the API level for better performance
func (m *KubernetesSessionManager) buildLabelSelector(filter entities.SessionFilter) string {
	// Base selector for agentapi sessions.
	// "!agentapi.proxy/stock" excludes Services that are still in stock state
	// (stock=true: unclaimed, stock=claiming: being adopted).  This prevents
	// other proxy replicas from restoring a stock/claiming Service with an empty
	// user-id during the adoption window, which would cause 403 errors and
	// sessions appearing/disappearing across replicas.
	selector := "app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session,!agentapi.proxy/stock"

	// Add UserID filter
	if filter.UserID != "" {
		selector += ",agentapi.proxy/user-id=" + sanitizeLabelValue(filter.UserID)
	}

	// Add Scope filter (only for team scope to maintain backward compatibility)
	// Note: scope=user is not added to LabelSelector because old sessions may not have
	// the scope label set. These sessions should be treated as user-scoped by default.
	// Go-level filtering handles scope=user cases properly via session.Scope() method.
	if filter.Scope == entities.ScopeTeam {
		selector += ",agentapi.proxy/scope=" + string(filter.Scope)
	}

	// Add TeamID filter using sha256 hash for consistent matching
	if filter.TeamID != "" {
		selector += ",agentapi.proxy/team-id-hash=" + hashTeamID(filter.TeamID)
	}

	return selector
}

// buildLabels creates standard labels for Kubernetes resources
func (m *KubernetesSessionManager) buildLabels(session *KubernetesSession) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/instance":   session.id,
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    session.id,
		"agentapi.proxy/user-id":       sanitizeLabelValue(session.Request().UserID),
	}

	// Add scope and team_id labels for filtering
	// Always set scope label (default to "user" if not specified)
	scope := session.Request().Scope
	if scope == "" {
		scope = entities.ScopeUser
	}
	labels["agentapi.proxy/scope"] = string(scope)
	if session.Request().TeamID != "" {
		// Use sha256 hash for team-id label to avoid sanitization issues with "/" in team IDs
		// The original team_id is stored in annotations for restoration
		labels["agentapi.proxy/team-id-hash"] = hashTeamID(session.Request().TeamID)
	}

	// Add tags as labels (sanitized for Kubernetes)
	for k, v := range session.Request().Tags {
		labelKey := fmt.Sprintf("agentapi.proxy/tag-%s", sanitizeLabelKey(k))
		labels[labelKey] = sanitizeLabelValue(v)
	}

	if session.isStock {
		labels["agentapi.proxy/stock"] = "true"
	}

	return labels
}

// buildEnvVars creates environment variables for the session pod
func (m *KubernetesSessionManager) buildEnvVars(session *KubernetesSession, req *entities.RunServerRequest) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "AGENTAPI_PORT", Value: fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		{Name: "AGENTAPI_SESSION_ID", Value: session.id},
		{Name: "AGENTAPI_USER_ID", Value: req.UserID},
		{Name: "HOME", Value: "/home/agentapi"},
		// GitHub App PEM path (file is written by setup directly to container FS)
		{Name: "GITHUB_APP_PEM_PATH", Value: "/tmp/github-app/app.pem"},
	}

	// Add Claude Code telemetry configuration
	if m.k8sConfig.OtelCollectorEnabled {
		envVars = append(envVars,
			corev1.EnvVar{Name: "CLAUDE_CODE_ENABLE_TELEMETRY", Value: "1"},
			corev1.EnvVar{Name: "OTEL_METRICS_EXPORTER", Value: "prometheus"},
		)
	}

	// Add Team ID if in team scope
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_TEAM_ID", Value: req.TeamID})
	}

	// Add Agent Type if specified
	if req.AgentType != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_AGENT_TYPE", Value: req.AgentType})

		// Add claude-agentapi / codex-agentapi specific environment variables
		if req.AgentType == "claude-agentapi" || req.AgentType == "codex-agentapi" {
			envVars = append(envVars, corev1.EnvVar{Name: "HOST", Value: "0.0.0.0"})
			envVars = append(envVars, corev1.EnvVar{Name: "PORT", Value: fmt.Sprintf("%d", m.k8sConfig.BasePort)})
		}
	}

	// Add CLAUDE_ARGS from request environment or proxy's environment
	claudeArgs := ""
	if req.Environment != nil {
		if v, ok := req.Environment["CLAUDE_ARGS"]; ok {
			claudeArgs = v
		}
	}
	if claudeArgs == "" {
		claudeArgs = os.Getenv("CLAUDE_ARGS")
	}
	if claudeArgs != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "CLAUDE_ARGS", Value: claudeArgs})
	}

	// Add repository info if available
	if req.RepoInfo != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "AGENTAPI_REPO_FULLNAME", Value: req.RepoInfo.FullName},
			corev1.EnvVar{Name: "AGENTAPI_CLONE_DIR", Value: "/home/agentapi/workdir/repo"},
		)
	}

	// Add environment variables from request (except CLAUDE_ARGS which is already handled)
	for k, v := range req.Environment {
		if k != "CLAUDE_ARGS" {
			envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
		}
	}

	// Add VAPID environment variables for push notifications
	vapidEnvVars := []string{"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_CONTACT_EMAIL"}
	for _, envName := range vapidEnvVars {
		if value := os.Getenv(envName); value != "" {
			envVars = append(envVars, corev1.EnvVar{Name: envName, Value: value})
		}
	}

	// Add notification base URL so session pods can construct correct notification URLs
	if value := os.Getenv("NOTIFICATION_BASE_URL"); value != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "NOTIFICATION_BASE_URL", Value: value})
	}

	// Note: Bedrock settings are now loaded via envFrom from agent-env-{name} Secret
	// which is synced by CredentialsSecretSyncer when settings are updated via API

	return envVars
}

// sanitizeLabelKey sanitizes a string to be used as a Kubernetes label key
// patchLastMessageAt applies a MergePatch to update the last-message-at annotation.
func (m *KubernetesSessionManager) patchLastMessageAt(ctx context.Context, svcName string, t time.Time) error {
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				"agentapi.proxy/last-message-at": t.UTC().Format(time.RFC3339),
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}
	_, err = m.client.CoreV1().Services(m.namespace).Patch(
		ctx, svcName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func sanitizeLabelKey(s string) string {
	// Label keys must be 63 characters or less
	// Must start and end with alphanumeric character
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Trim non-alphanumeric characters from start and end
	sanitized = strings.Trim(sanitized, "-_.")
	return sanitized
}

// hashTeamID creates a sha256 hash of the team ID for use as a Kubernetes label value
// This allows querying by team_id without sanitization issues (e.g., "/" in team IDs)
// The hash is truncated to 63 characters to fit within Kubernetes label value limits
func hashTeamID(teamID string) string {
	if teamID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(teamID))
	hexHash := hex.EncodeToString(hash[:])
	// Truncate to 63 characters (Kubernetes label value limit)
	if len(hexHash) > 63 {
		hexHash = hexHash[:63]
	}
	return hexHash
}

// int64Ptr returns a pointer to an int64
func int64Ptr(i int64) *int64 {
	return &i
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool {
	return &b
}

// isPVCEnabled returns whether PVC is enabled for session workdir
// Returns true by default if not explicitly set
func (m *KubernetesSessionManager) isPVCEnabled() bool {
	if m.k8sConfig.PVCEnabled == nil {
		return true // Default to enabled
	}
	return *m.k8sConfig.PVCEnabled
}

// GetClient returns the Kubernetes client (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetClient() kubernetes.Interface {
	return m.client
}

// GetNamespace returns the Kubernetes namespace (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetNamespace() string {
	return m.namespace
}

// SetSettingsRepository sets the settings repository for Bedrock configuration
func (m *KubernetesSessionManager) SetSettingsRepository(repo portrepos.SettingsRepository) {
	m.settingsRepo = repo
}

// SetTeamConfigRepository sets the team config repository for service account configuration
func (m *KubernetesSessionManager) SetTeamConfigRepository(repo portrepos.TeamConfigRepository) {
	m.teamConfigRepo = repo
}

// SetServiceAccountEnsurer sets the service account ensurer for team-scoped session creation
func (m *KubernetesSessionManager) SetServiceAccountEnsurer(ensurer ServiceAccountEnsurer) {
	m.serviceAccountEnsurer = ensurer
}

// AddSessionDeletedHandler registers a handler that is invoked when a session is deleted,
// before its Kubernetes resources are removed. Multiple handlers can be registered and
// they are called in registration order.
func (m *KubernetesSessionManager) AddSessionDeletedHandler(handler SessionDeletedHandler) {
	m.handlersMutex.Lock()
	defer m.handlersMutex.Unlock()
	m.onSessionDeletedHandlers = append(m.onSessionDeletedHandlers, handler)
}

// SetPersonalAPIKeyRepository sets the personal API key repository
func (m *KubernetesSessionManager) SetPersonalAPIKeyRepository(repo portrepos.PersonalAPIKeyRepository) {
	m.personalAPIKeyRepo = repo
}

// GetPersonalAPIKeyRepository returns the personal API key repository
func (m *KubernetesSessionManager) GetPersonalAPIKeyRepository() portrepos.PersonalAPIKeyRepository {
	return m.personalAPIKeyRepo
}

// getSessionStatusFromDeployment determines session status from Deployment state
func (m *KubernetesSessionManager) getSessionStatusFromDeployment(sessionID string) string {
	deploymentName := fmt.Sprintf("agentapi-session-%s", sessionID)
	deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
		context.Background(), deploymentName, metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			return "stopped"
		}
		return "unknown"
	}

	if deployment.Status.ReadyReplicas > 0 {
		return "active"
	}
	if deployment.Status.Replicas > 0 {
		return "starting"
	}
	return "unhealthy"
}

// buildMainContainerVolumeMounts builds the volume mounts for the main container
func (m *KubernetesSessionManager) buildMainContainerVolumeMounts(session *KubernetesSession) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "workdir",
			MountPath: "/home/agentapi/workdir",
		},
		// dot-claude EmptyDir – used by main container for Claude Code settings
		{
			Name:      "dot-claude",
			MountPath: "/home/agentapi/.claude",
		},
		// notification subscriptions source – read by setup on startup
		{
			Name:      "notification-subscriptions-source",
			MountPath: "/notification-subscriptions-source",
			ReadOnly:  true,
		},
	}

	// session-settings Secret – optional mount for Pod restart auto-provisioning.
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "session-settings",
		MountPath: "/session-settings",
		ReadOnly:  true,
	})

	// Add webhook payload volume mount if webhook payload is provided
	if len(session.WebhookPayload()) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "webhook-payload",
			MountPath: "/opt/webhook/payload.json",
			SubPath:   "payload.json",
			ReadOnly:  true,
		})
	}

	// Add claude-agentapi history volume mount
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "claude-agentapi-history",
		MountPath: "/opt/claude-agentapi",
	})

	return volumeMounts
}

// restoreSessionFromService restores a session from Kubernetes Service
// This is used to recover sessions after agentapi-proxy restart
func (m *KubernetesSessionManager) restoreSessionFromService(svc *corev1.Service) *KubernetesSession {
	sessionID := svc.Labels["agentapi.proxy/session-id"]
	userID := svc.Labels["agentapi.proxy/user-id"]

	// Restore tags from labels
	tags := make(map[string]string)
	for k, v := range svc.Labels {
		if strings.HasPrefix(k, "agentapi.proxy/tag-") {
			tagKey := strings.TrimPrefix(k, "agentapi.proxy/tag-")
			tags[tagKey] = v
		}
	}

	// Restore scope from labels
	scope := entities.ResourceScope(svc.Labels["agentapi.proxy/scope"])
	if scope == "" {
		scope = entities.ScopeUser // Default to user scope for backward compatibility
	}
	// Restore team_id from annotations (original unsanitized value)
	// Labels contain only the hash for querying purposes
	teamID := svc.Annotations["agentapi.proxy/team-id"]

	// Restore initial message: prefer Service annotation (written at creation, immediately
	// available across all proxy replicas) and fall back to the settings Secret (written
	// asynchronously after provisioning completes).
	restoreCtx := context.Background()
	initialMessage := svc.Annotations["agentapi.proxy/initial-message"]
	if initialMessage == "" {
		initialMessage = m.getInitialMessageFromSecret(restoreCtx, svc.Name)
	}
	sessionMeta := m.getSessionMetaFromSecret(restoreCtx, svc.Name)

	// Extract MemoryKey and Teams from session meta if available
	var memoryKey map[string]string
	var teams []string
	if sessionMeta != nil {
		memoryKey = sessionMeta.MemoryKey
		teams = sessionMeta.Teams
	}

	// Parse created-at from annotations
	createdAt := time.Now()
	if createdAtStr, ok := svc.Annotations["agentapi.proxy/created-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = parsed
		}
	}

	// Parse updated-at from annotations
	updatedAt := createdAt // Default to createdAt if not set
	if updatedAtStr, ok := svc.Annotations["agentapi.proxy/updated-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			updatedAt = parsed
		}
	}

	// Parse last-message-at from annotations (fallback to slack-last-message-at for backward compat)
	lastMessageAt := createdAt // Default to createdAt if not set
	for _, key := range []string{"agentapi.proxy/last-message-at", "agentapi.proxy/slack-last-message-at"} {
		if v, ok := svc.Annotations[key]; ok && v != "" {
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				lastMessageAt = parsed
				break
			}
		}
	}

	// Extract service port
	servicePort := m.k8sConfig.BasePort
	if len(svc.Spec.Ports) > 0 {
		servicePort = int(svc.Spec.Ports[0].Port)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create session using constructor
	session := NewKubernetesSession(
		sessionID,
		&entities.RunServerRequest{
			UserID:         userID,
			Tags:           tags,
			Scope:          scope,
			TeamID:         teamID,
			InitialMessage: initialMessage,
			MemoryKey:      memoryKey,
			Teams:          teams,
		},
		fmt.Sprintf("agentapi-session-%s", sessionID),
		svc.Name,
		fmt.Sprintf("agentapi-session-%s-pvc", sessionID),
		m.namespace,
		servicePort,
		cancel,
		nil, // No webhook payload for restored sessions
	)
	// Set restored values
	session.SetStartedAt(createdAt)
	session.SetUpdatedAt(updatedAt)
	session.SetLastMessageAt(lastMessageAt)
	session.SetStatus(m.getSessionStatusFromDeployment(sessionID))
	session.SetDescription(initialMessage) // Cache initial message as description

	// Add to memory map
	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Start watching deployment status
	go m.watchDeploymentStatus(ctx, session)

	log.Printf("[K8S_SESSION] Restored session %s from Service", sessionID)

	return session
}

// restoreSessionFromServiceWithDeployment restores a session from Kubernetes Service
// using a pre-fetched deployment to avoid additional API calls
func (m *KubernetesSessionManager) restoreSessionFromServiceWithDeployment(svc *corev1.Service, deployment *appsv1.Deployment) *KubernetesSession {
	sessionID := svc.Labels["agentapi.proxy/session-id"]
	userID := svc.Labels["agentapi.proxy/user-id"]

	// Restore tags from labels
	tags := make(map[string]string)
	for k, v := range svc.Labels {
		if strings.HasPrefix(k, "agentapi.proxy/tag-") {
			tagKey := strings.TrimPrefix(k, "agentapi.proxy/tag-")
			tags[tagKey] = v
		}
	}

	// Restore scope from labels
	scope := entities.ResourceScope(svc.Labels["agentapi.proxy/scope"])
	if scope == "" {
		scope = entities.ScopeUser // Default to user scope for backward compatibility
	}
	// Restore team_id from annotations (original unsanitized value)
	// Labels contain only the hash for querying purposes
	teamID := svc.Annotations["agentapi.proxy/team-id"]

	// Restore initial message: prefer Service annotation (written at creation, immediately
	// available across all proxy replicas) and fall back to the settings Secret (written
	// asynchronously after provisioning completes).
	restoreCtx := context.Background()
	initialMessage := svc.Annotations["agentapi.proxy/initial-message"]
	if initialMessage == "" {
		initialMessage = m.getInitialMessageFromSecret(restoreCtx, svc.Name)
	}
	sessionMeta := m.getSessionMetaFromSecret(restoreCtx, svc.Name)

	// Extract MemoryKey and Teams from session meta if available
	var memoryKey map[string]string
	var teams []string
	if sessionMeta != nil {
		memoryKey = sessionMeta.MemoryKey
		teams = sessionMeta.Teams
	}

	// Parse created-at from annotations
	createdAt := time.Now()
	if createdAtStr, ok := svc.Annotations["agentapi.proxy/created-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = parsed
		}
	}

	// Parse updated-at from annotations
	updatedAt := createdAt // Default to createdAt if not set
	if updatedAtStr, ok := svc.Annotations["agentapi.proxy/updated-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			updatedAt = parsed
		}
	}

	// Parse last-message-at from annotations (fallback to slack-last-message-at for backward compat)
	lastMessageAt := createdAt // Default to createdAt if not set
	for _, key := range []string{"agentapi.proxy/last-message-at", "agentapi.proxy/slack-last-message-at"} {
		if v, ok := svc.Annotations[key]; ok && v != "" {
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				lastMessageAt = parsed
				break
			}
		}
	}

	// Extract service port
	servicePort := m.k8sConfig.BasePort
	if len(svc.Spec.Ports) > 0 {
		servicePort = int(svc.Spec.Ports[0].Port)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create session using constructor
	session := NewKubernetesSession(
		sessionID,
		&entities.RunServerRequest{
			UserID:         userID,
			Tags:           tags,
			Scope:          scope,
			TeamID:         teamID,
			InitialMessage: initialMessage,
			MemoryKey:      memoryKey,
			Teams:          teams,
		},
		fmt.Sprintf("agentapi-session-%s", sessionID),
		svc.Name,
		fmt.Sprintf("agentapi-session-%s-pvc", sessionID),
		m.namespace,
		servicePort,
		cancel,
		nil, // No webhook payload for restored sessions
	)
	// Set restored values
	session.SetStartedAt(createdAt)
	session.SetUpdatedAt(updatedAt)
	session.SetLastMessageAt(lastMessageAt)
	session.SetStatus(m.getStatusFromDeploymentObject(deployment))
	session.SetDescription(initialMessage) // Cache initial message as description

	// Add to memory map
	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Start watching deployment status
	go m.watchDeploymentStatus(ctx, session)

	log.Printf("[K8S_SESSION] Restored session %s from Service (with pre-fetched deployment)", sessionID)

	return session
}

// getStatusFromDeploymentObject determines session status from a pre-fetched Deployment object
func (m *KubernetesSessionManager) getStatusFromDeploymentObject(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return "stopped"
	}

	if deployment.Status.ReadyReplicas > 0 {
		return "active"
	}
	if deployment.Status.Replicas > 0 {
		return "starting"
	}
	return "unhealthy"
}

// ensureOtelcolConfigMap creates or updates the OpenTelemetry Collector ConfigMap
func (m *KubernetesSessionManager) ensureOtelcolConfigMap(ctx context.Context) error {
	if !m.k8sConfig.OtelCollectorEnabled {
		return nil
	}

	configMapName := "otelcol-config"
	scrapeInterval := "15s"
	if m.k8sConfig.OtelCollectorScrapeInterval != "" {
		scrapeInterval = m.k8sConfig.OtelCollectorScrapeInterval
	}
	claudeCodePort := 9464
	if m.k8sConfig.OtelCollectorClaudeCodePort > 0 {
		claudeCodePort = m.k8sConfig.OtelCollectorClaudeCodePort
	}
	exporterPort := 9090
	if m.k8sConfig.OtelCollectorExporterPort > 0 {
		exporterPort = m.k8sConfig.OtelCollectorExporterPort
	}

	otelConfig := fmt.Sprintf(`receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: 'claude-code'
          scrape_interval: %s
          static_configs:
            - targets: ['localhost:%d']

processors:
  resource:
    attributes:
      - key: user_id
        action: delete
      - key: session_id
        action: delete
  transform:
    error_mode: ignore
    metric_statements:
      - context: datapoint
        statements:
          # Rename claude-code's native labels
          - set(attributes["claude_user_id"], attributes["user_id"]) where attributes["user_id"] != nil
          - set(attributes["claude_session_id"], attributes["session_id"]) where attributes["session_id"] != nil
          - delete_key(attributes, "user_id")
          - delete_key(attributes, "session_id")
          # Remove user_email label to prevent it from being scraped by Prometheus
          - delete_key(attributes, "user_email")
          # Add agentapi labels
          - set(attributes["agentapi_session_id"], "${env:SESSION_ID}")
          - set(attributes["agentapi_user_id"], "${env:USER_ID}")
          - set(attributes["agentapi_team_id"], "${env:TEAM_ID}")
          - set(attributes["agentapi_schedule_id"], "${env:SCHEDULE_ID}")
          - set(attributes["agentapi_webhook_id"], "${env:WEBHOOK_ID}")
          - set(attributes["agentapi_agent_type"], "${env:AGENT_TYPE}")

exporters:
  prometheus:
    endpoint: "0.0.0.0:%d"
    resource_to_telemetry_conversion:
      enabled: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [resource, transform]
      exporters: [prometheus]`, scrapeInterval, claudeCodePort, exporterPort)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "otelcol",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/component":  "telemetry",
			},
		},
		Data: map[string]string{
			"otel-collector-config.yaml": otelConfig,
		},
	}

	// Try to get existing ConfigMap
	existingCM, err := m.client.CoreV1().ConfigMaps(m.namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			_, err = m.client.CoreV1().ConfigMaps(m.namespace).Create(ctx, configMap, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create otelcol ConfigMap: %w", err)
			}
			log.Printf("[K8S_SESSION] Created otelcol ConfigMap: %s", configMapName)
			return nil
		}
		return fmt.Errorf("failed to get otelcol ConfigMap: %w", err)
	}

	// Update existing ConfigMap
	existingCM.Data = configMap.Data
	existingCM.Labels = configMap.Labels
	_, err = m.client.CoreV1().ConfigMaps(m.namespace).Update(ctx, existingCM, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update otelcol ConfigMap: %w", err)
	}
	log.Printf("[K8S_SESSION] Updated otelcol ConfigMap: %s", configMapName)
	return nil
}

// buildServicePorts builds the service ports for the session
func (m *KubernetesSessionManager) buildServicePorts(session *KubernetesSession) []corev1.ServicePort {
	ports := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       int32(session.ServicePort()),
			TargetPort: intstr.FromInt(m.k8sConfig.BasePort),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			// Expose agent-provisioner so the proxy server can call POST /provision.
			Name:       "provisioner",
			Port:       provisionerPort,
			TargetPort: intstr.FromInt(provisionerPort),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	// Add metrics port if otelcol is enabled
	if m.k8sConfig.OtelCollectorEnabled {
		exporterPort := 9090
		if m.k8sConfig.OtelCollectorExporterPort > 0 {
			exporterPort = m.k8sConfig.OtelCollectorExporterPort
		}
		ports = append(ports, corev1.ServicePort{
			Name:       "metrics",
			Port:       int32(exporterPort),
			TargetPort: intstr.FromInt(exporterPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return ports
}

// UpdateServiceAnnotation updates a specific annotation on a session's Service
func (m *KubernetesSessionManager) UpdateServiceAnnotation(ctx context.Context, sessionID, key, value string) error {
	session := m.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	ks, ok := session.(*KubernetesSession)
	if !ok {
		return fmt.Errorf("session is not a KubernetesSession")
	}

	serviceName := ks.ServiceName()

	// Get the current Service
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get service: %w", err)
	}

	// Update the annotation
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}
	svc.Annotations[key] = value

	// Update the Service
	_, err = m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service annotation: %w", err)
	}

	return nil
}

// GetInitialMessage retrieves the initial message from Secret for a given session
func (m *KubernetesSessionManager) GetInitialMessage(ctx context.Context, session *KubernetesSession) string {
	return m.getInitialMessageFromSecret(ctx, session.ServiceName())
}

// generatePersonalAPIKey generates a random API key for personal use
// This uses the same format as team service account keys
func generatePersonalAPIKey() (string, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", err
	}
	return "ap_" + hex.EncodeToString(keyBytes), nil
}

// buildSessionSettings constructs SessionSettings from RunServerRequest and session state.
// This consolidates buildEnvVars and envFrom logic into a single unified structure.
func (m *KubernetesSessionManager) buildSessionSettings(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
	webhookPayload []byte,
) *sessionsettings.SessionSettings {
	settings := &sessionsettings.SessionSettings{}

	// Session metadata
	scope := string(req.Scope)
	if scope == "" {
		scope = "user"
	}
	settings.Session = sessionsettings.SessionMeta{
		ID:        session.id,
		UserID:    req.UserID,
		Scope:     scope,
		TeamID:    req.TeamID,
		AgentType: req.AgentType,
		Oneshot:   req.Oneshot,
		Teams:     req.Teams,
		MemoryKey: req.MemoryKey,
	}

	// Build env vars (mirrors buildEnvVars logic from line 2695)
	env := map[string]string{
		"AGENTAPI_PORT":       fmt.Sprintf("%d", m.k8sConfig.BasePort),
		"AGENTAPI_SESSION_ID": session.id,
		"AGENTAPI_USER_ID":    req.UserID,
		"HOME":                "/home/agentapi",
		"GITHUB_APP_PEM_PATH": "/tmp/github-app/app.pem",
	}

	// Add Claude Code telemetry configuration
	if m.k8sConfig.OtelCollectorEnabled {
		env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
		env["OTEL_METRICS_EXPORTER"] = "prometheus"
	}

	// Add Team ID if in team scope
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		env["AGENTAPI_TEAM_ID"] = req.TeamID
	}

	// Add Agent Type if specified
	if req.AgentType != "" {
		env["AGENTAPI_AGENT_TYPE"] = req.AgentType

		// Add claude-agentapi / codex-agentapi specific environment variables
		if req.AgentType == "claude-agentapi" || req.AgentType == "codex-agentapi" {
			env["HOST"] = "0.0.0.0"
			env["PORT"] = fmt.Sprintf("%d", m.k8sConfig.BasePort)
		}
	}

	// Add CLAUDE_ARGS from request environment or proxy's environment
	claudeArgs := ""
	if req.Environment != nil {
		if v, ok := req.Environment["CLAUDE_ARGS"]; ok {
			claudeArgs = v
		}
	}
	if claudeArgs == "" {
		claudeArgs = os.Getenv("CLAUDE_ARGS")
	}
	if claudeArgs != "" {
		env["CLAUDE_ARGS"] = claudeArgs
	}

	// Add repository info if available
	if req.RepoInfo != nil {
		env["AGENTAPI_REPO_FULLNAME"] = req.RepoInfo.FullName
		env["AGENTAPI_CLONE_DIR"] = "/home/agentapi/workdir/repo"
	}

	// Add environment variables from request (except CLAUDE_ARGS which is already handled)
	for k, v := range req.Environment {
		if k != "CLAUDE_ARGS" {
			env[k] = v
		}
	}

	// Add VAPID environment variables for push notifications
	vapidEnvVars := []string{"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_CONTACT_EMAIL"}
	for _, envName := range vapidEnvVars {
		if value := os.Getenv(envName); value != "" {
			env[envName] = value
		}
	}

	// Build list of secret names to expand into env map
	// Pod does not have permission to read secrets, so we need to expand them here
	var secretNames []string

	if req.GithubToken != "" {
		// When params.github_token is provided: embed token directly, no per-session secret needed
		if m.k8sConfig.GitHubConfigSecretName != "" {
			secretNames = append(secretNames, m.k8sConfig.GitHubConfigSecretName)
		}
		env["GITHUB_TOKEN"] = req.GithubToken
	} else if m.k8sConfig.GitHubSecretName != "" {
		// When params.github_token is NOT provided
		secretNames = append(secretNames, m.k8sConfig.GitHubSecretName)
		if m.k8sConfig.GitHubConfigSecretName != "" {
			secretNames = append(secretNames, m.k8sConfig.GitHubConfigSecretName)
		}
	}

	// Expand secrets into env map (GitHub secrets only)
	for _, secretName := range secretNames {
		secret, err := m.client.CoreV1().Secrets(m.namespace).Get(
			ctx,
			secretName,
			metav1.GetOptions{},
		)
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Printf("[K8S_SESSION] Warning: failed to read secret %s for session settings: %v", secretName, err)
			}
			// Skip secrets that don't exist (they are all optional)
			continue
		}

		// Merge secret data into env map (later secrets override earlier ones due to iteration order)
		for k, v := range secret.Data {
			env[k] = string(v)
		}
	}

	// Expand personal API key directly from repository for user-scoped sessions
	// Treat empty scope as user scope (default behavior)
	if (req.Scope == entities.ScopeUser || req.Scope == "") && m.personalAPIKeyRepo != nil {
		apiKey, err := m.personalAPIKeyRepo.FindByUserID(ctx, entities.UserID(req.UserID))
		if err != nil {
			// If no API key exists, create a new one automatically
			log.Printf("[K8S_SESSION] No personal API key found for user %s, creating new one", req.UserID)
			generatedKey, genErr := generatePersonalAPIKey()
			if genErr != nil {
				log.Printf("[K8S_SESSION] Warning: failed to generate personal API key: %v", genErr)
			} else {
				apiKey = entities.NewPersonalAPIKey(entities.UserID(req.UserID), generatedKey)
				if saveErr := m.personalAPIKeyRepo.Save(ctx, apiKey); saveErr != nil {
					log.Printf("[K8S_SESSION] Warning: failed to save personal API key for user %s: %v", req.UserID, saveErr)
					apiKey = nil
				}
			}
		}
		if apiKey != nil {
			env["AGENTAPI_KEY"] = apiKey.APIKey()
			log.Printf("[K8S_SESSION] Added personal API key to session settings env for user %s", req.UserID)
		}
	}

	// Expand team env vars directly from repository for team-scoped sessions
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.teamConfigRepo != nil {
		teamConfig, err := m.teamConfigRepo.FindByTeamID(ctx, req.TeamID)
		if err != nil {
			log.Printf("[K8S_SESSION] Team config not found for team %s in session settings: %v", req.TeamID, err)
		} else {
			if sa := teamConfig.ServiceAccount(); sa != nil {
				env["AGENTAPI_KEY"] = sa.APIKey()
				log.Printf("[K8S_SESSION] Added service account API key to session settings env for team %s", req.TeamID)
			}
			for k, v := range teamConfig.EnvVars() {
				env[k] = v
			}
		}
	}

	// Resolve and materialize settings from agentapi-settings-* Secrets.
	// This merges env_vars, bedrock credentials, oauth token, MCP servers,
	// marketplaces, plugins, and hooks from base → team → user → oneshot layers.
	materialized := m.resolveSettings(ctx, session, req)
	for k, v := range materialized.EnvVars {
		env[k] = v
	}

	// Memory integration: generate MEMORY_KEY_FLAGS and AGENTAPI_SCOPE for startup script
	// and memory-sync sidecar. Flags are sorted for deterministic shell script expansion.
	if len(req.MemoryKey) > 0 {
		keys := make([]string, 0, len(req.MemoryKey))
		for k := range req.MemoryKey {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var flags []string
		for _, k := range keys {
			flags = append(flags, fmt.Sprintf("--tag %s=%s", k, req.MemoryKey[k]))
		}
		env["MEMORY_KEY_FLAGS"] = strings.Join(flags, " ")

		memoryScope := "user"
		if req.Scope == entities.ScopeTeam {
			memoryScope = "team"
		}
		env["AGENTAPI_SCOPE"] = memoryScope
	}

	// Cache the resolved API key in the session for use by the memory-sync sidecar
	if apiKey, ok := env["AGENTAPI_KEY"]; ok && apiKey != "" {
		session.SetResolvedAPIKey(apiKey)
	}

	settings.Env = env

	// Claude config
	settings.Claude = sessionsettings.ClaudeConfig{
		ClaudeJSON: map[string]interface{}{
			"hasCompletedOnboarding":        true,
			"bypassPermissionsModeAccepted": true,
		},
		SettingsJSON: materialized.SettingsJSON,
		MCPServers:   materialized.MCPServers,
	}

	// Repository info
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		settings.Repository = &sessionsettings.RepositoryConfig{
			FullName: req.RepoInfo.FullName,
			CloneDir: "/home/agentapi/workdir/repo",
		}
	}

	// Initial message
	settings.InitialMessage = req.InitialMessage

	// Webhook payload
	if len(webhookPayload) > 0 {
		settings.WebhookPayload = string(webhookPayload)
	}

	// GitHub config
	if req.GithubToken != "" {
		settings.Github = &sessionsettings.GithubConfig{
			Token:            req.GithubToken,
			ConfigSecretName: m.k8sConfig.GitHubConfigSecretName,
		}
	} else if m.k8sConfig.GitHubSecretName != "" {
		settings.Github = &sessionsettings.GithubConfig{
			SecretName:       m.k8sConfig.GitHubSecretName,
			ConfigSecretName: m.k8sConfig.GitHubConfigSecretName,
		}
	}

	// Startup command (simplified version for now - full command logic in pod)
	if req.AgentType == "claude-agentapi" {
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{"claude-agentapi"},
		}
	} else {
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--allowed-hosts", "*", "--allowed-origins", "*", "--port", fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		}
	}

	// Slack integration: embed SlackParams so the provisioner can launch
	// claude-posts as a subprocess. This enables stock sessions (which have no
	// slack-integration sidecar) to forward agent output to Slack.
	// Use per-bot token secret if provided, fall back to server default.
	if req.SlackParams != nil && req.SlackParams.Channel != "" {
		slackSecretName := req.SlackParams.BotTokenSecretName
		if slackSecretName == "" {
			slackSecretName = m.k8sConfig.SlackBotTokenSecretName
		}
		if slackSecretName != "" {
			botTokenSecretKey := req.SlackParams.BotTokenSecretKey
			if botTokenSecretKey == "" {
				botTokenSecretKey = m.k8sConfig.SlackBotTokenSecretKey
			}
			if botTokenSecretKey == "" {
				botTokenSecretKey = defaultSlackBotTokenSecretKey
			}
			secret, err := m.client.CoreV1().Secrets(m.namespace).Get(
				ctx,
				slackSecretName,
				metav1.GetOptions{},
			)
			if err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to read Slack bot token secret %s for session %s: %v",
					slackSecretName, session.id, err)
			} else {
				botToken := string(secret.Data[botTokenSecretKey])
				if botToken != "" {
					settings.SlackParams = &sessionsettings.SlackParams{
						Channel:  req.SlackParams.Channel,
						ThreadTS: req.SlackParams.ThreadTS,
						BotToken: botToken,
					}
					log.Printf("[K8S_SESSION] SlackParams embedded in session settings for session %s (channel: %s, secret: %s)",
						session.id, req.SlackParams.Channel, slackSecretName)
				} else {
					log.Printf("[K8S_SESSION] Warning: Slack bot token secret %s key %s is empty for session %s",
						slackSecretName, botTokenSecretKey, session.id)
				}
			}
		}
	}

	// OtelCollector in-process config: otelcol always runs as a subprocess inside
	// the agentapi container, started by the provisioner after user context is known.
	// This ensures metrics labels are correct even with the stock inventory feature.
	if m.k8sConfig.OtelCollectorEnabled {
		scrapeInterval := "15s"
		if m.k8sConfig.OtelCollectorScrapeInterval != "" {
			scrapeInterval = m.k8sConfig.OtelCollectorScrapeInterval
		}
		claudeCodePort := 9464
		if m.k8sConfig.OtelCollectorClaudeCodePort > 0 {
			claudeCodePort = m.k8sConfig.OtelCollectorClaudeCodePort
		}
		exporterPort := 9090
		if m.k8sConfig.OtelCollectorExporterPort > 0 {
			exporterPort = m.k8sConfig.OtelCollectorExporterPort
		}

		scheduleID := "-"
		webhookID := "-"
		if req.Tags != nil {
			if v := req.Tags["schedule_id"]; v != "" {
				scheduleID = v
			}
			if v := req.Tags["webhook_id"]; v != "" {
				webhookID = v
			}
		}
		agentType := req.AgentType
		if agentType == "" {
			agentType = "-"
		}
		teamID := "-"
		if req.Scope == entities.ScopeTeam && req.TeamID != "" {
			teamID = req.TeamID
		}

		settings.OtelCollector = &sessionsettings.OtelCollectorConfig{
			Enabled:        true,
			ScrapeInterval: scrapeInterval,
			ClaudeCodePort: claudeCodePort,
			ExporterPort:   exporterPort,
			SessionID:      session.id,
			UserID:         req.UserID,
			TeamID:         teamID,
			ScheduleID:     scheduleID,
			WebhookID:      webhookID,
			AgentType:      agentType,
		}
		log.Printf("[K8S_SESSION] OtelCollector in-process config embedded for session %s", session.id)
	}

	// Embed managed files from the user's agentapi-agent-files-{userID} Secret so that
	// stock pool pods (which have no user-specific volume mounts) can restore files
	// on startup via the provision endpoint payload.
	// Only applies to user-scoped sessions with a known UserID.
	if (req.Scope == entities.ScopeUser || req.Scope == "") && req.UserID != "" {
		filesSecretName := fmt.Sprintf("agentapi-agent-files-%s", sanitizeLabelValue(req.UserID))
		filesSecret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, filesSecretName, metav1.GetOptions{})
		if err == nil && len(filesSecret.Data) > 0 {
			settings.Files = sessionsettings.SecretDataToFiles(filesSecret.Data)
			if len(settings.Files) == 0 {
				log.Printf("[K8S_SESSION] WARNING: Secret %s has %d data entries but SecretDataToFiles returned empty for session %s",
					filesSecretName, len(filesSecret.Data), session.id)
			} else {
				log.Printf("[K8S_SESSION] Embedded %d managed file(s) from Secret %s for session %s",
					len(settings.Files), filesSecretName, session.id)
			}
		}
		// Not found or no data is normal (user hasn't logged in yet); skip silently.
	}

	return settings
}

// createSessionSettingsSecretFromSettings creates the unified session settings Secret
// from a pre-built SessionSettings struct.
// This Secret is used by agent-provisioner for auto-provisioning on Pod restart.
func (m *KubernetesSessionManager) createSessionSettingsSecretFromSettings(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
	settings *sessionsettings.SessionSettings,
) error {
	yamlData, err := sessionsettings.MarshalYAML(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal session settings to YAML: %w", err)
	}

	secretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(req.UserID),
				"agentapi.proxy/resource":   "session-settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"settings.yaml": yamlData,
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create session settings secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created session settings Secret %s for session %s", secretName, session.id)
	return nil
}

// deleteSessionSettingsSecret deletes the unified session settings Secret.
func (m *KubernetesSessionManager) deleteSessionSettingsSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete session settings secret: %w", err)
	}
	return nil
}

// deleteOneshotSettingsSecret deletes the oneshot settings Secret.
// This fixes the existing bug where oneshot-settings secrets were not being cleaned up.
func (m *KubernetesSessionManager) deleteOneshotSettingsSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-oneshot-settings", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete oneshot settings secret: %w", err)
	}
	return nil
}

// readSettingsPatch reads the settings.json from an agentapi-settings-* Secret
// and returns it as a SettingsPatch. Returns nil if the secret does not exist or cannot be parsed.
func (m *KubernetesSessionManager) readSettingsPatch(ctx context.Context, secretName string) *settingspatch.SettingsPatch {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("[K8S_SESSION] Warning: failed to read settings secret %s: %v", secretName, err)
		}
		return nil
	}
	data, ok := secret.Data["settings.json"]
	if !ok {
		return nil
	}
	patch, err := settingspatch.FromJSON(data)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to parse settings.json from secret %s: %v", secretName, err)
		return nil
	}
	return &patch
}

// resolveSettings reads settings patches from the relevant Kubernetes Secrets
// (base → team[] → user → oneshot) and returns materialized session configuration.
//
// This is the single entry point for all settings merging. It replaces the previous
// dual-path approach (readAgentapiSettingsSecret + expandSettingsToEnv for env vars,
// and mergeSettingsAndMCP for Claude settings JSON).
//
// When the user has preferred_team_id set in their personal settings, only that team's
// settings are applied (instead of all teams). This allows users to explicitly choose
// which team's settings (Bedrock, MCP servers, env vars, etc.) to use.
func (m *KubernetesSessionManager) resolveSettings(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
) settingspatch.MaterializedSettings {
	var layers []settingspatch.SettingsPatch

	appendIfExists := func(secretName string) {
		if p := m.readSettingsPatch(ctx, secretName); p != nil {
			layers = append(layers, *p)
		}
	}

	// 1. base (lowest priority)
	if m.k8sConfig.SettingsBaseSecret != "" {
		appendIfExists(m.k8sConfig.SettingsBaseSecret)
	}

	// 2. teams (in order)
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		// Team-scoped session: always use the specified team only (preferred_team_id is ignored)
		appendIfExists(fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.TeamID)))
	} else {
		// User-scoped session: check if the user has a preferred team set
		preferredTeamID := m.resolvePreferredTeamID(ctx, req)
		if preferredTeamID != "" {
			// Use only the preferred team's settings
			log.Printf("[K8S_SESSION] Using preferred team settings: %s", preferredTeamID)
			appendIfExists(fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(preferredTeamID)))
		} else {
			// Default: apply all teams in order
			for _, team := range req.Teams {
				appendIfExists(fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(team)))
			}
		}
		// 3. user
		if req.UserID != "" {
			appendIfExists(fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.UserID)))
		}
	}

	// 4. oneshot (highest priority)
	if req.Oneshot {
		appendIfExists(fmt.Sprintf("%s-oneshot-settings", session.ServiceName()))
	}

	resolved := settingspatch.Resolve(layers...)
	materialized, err := settingspatch.Materialize(resolved)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to materialize settings: %v", err)
	}
	return materialized
}

// resolvePreferredTeamID reads the user's personal settings to determine if a preferred team
// is configured. Returns the preferred team ID if it is set and the user belongs to that team,
// otherwise returns "".
func (m *KubernetesSessionManager) resolvePreferredTeamID(ctx context.Context, req *entities.RunServerRequest) string {
	if req.UserID == "" {
		return ""
	}
	userSecretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.UserID))
	userPatch := m.readSettingsPatch(ctx, userSecretName)
	if userPatch == nil || userPatch.PreferredTeamID == "" {
		return ""
	}
	// Security check: ensure the preferred team is one the user actually belongs to
	for _, team := range req.Teams {
		if team == userPatch.PreferredTeamID {
			return userPatch.PreferredTeamID
		}
	}
	log.Printf("[K8S_SESSION] Warning: preferred_team_id %q is not in user's team list, falling back to all teams", userPatch.PreferredTeamID)
	return ""
}

// BuildRemoteProvisionSettings implements portrepos.RemoteProvisionSettingsBuilder.
// It builds a fully-resolved SessionSettings for forwarding to an external session manager (Proxy B).
// All settings layers (base → team → user) are resolved so that Proxy B can create the session
// without needing to re-resolve secrets from its own cluster.
func (m *KubernetesSessionManager) BuildRemoteProvisionSettings(
	ctx context.Context,
	sessionID string,
	req *entities.RunServerRequest,
) (*sessionsettings.SessionSettings, error) {
	// Create a temporary session with the provided ID to satisfy buildSessionSettings
	tempSession := &KubernetesSession{
		id:          sessionID,
		serviceName: fmt.Sprintf("agentapi-session-%s-svc", sessionID),
	}
	settings := m.buildSessionSettings(ctx, tempSession, req, nil)
	return settings, nil
}
