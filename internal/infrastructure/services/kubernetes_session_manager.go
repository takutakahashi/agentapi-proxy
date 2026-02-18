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
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/mcp"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settings"
)

// KubernetesSessionManager manages sessions using Kubernetes Deployments
// ServiceAccountEnsurer ensures a service account exists for a team.
// Implementations must be safe to call concurrently.
type ServiceAccountEnsurer interface {
	EnsureServiceAccount(ctx context.Context, teamID string) error
}

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

// CreateSession creates a new session with a Kubernetes Deployment
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
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

	// Ensure Base Secret exists (create if not present)
	if err := m.ensureBaseSecret(ctx); err != nil {
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to ensure base Secret: %w", err)
	}

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

	// Create initial message Secret if initial message is provided
	if req.InitialMessage != "" {
		if err := m.createInitialMessageSecret(ctx, session, req.InitialMessage); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create initial message secret: %v", err)
			// Continue anyway - sidecar will handle missing secret gracefully
		}
		// Cache initial message as description
		session.SetDescription(req.InitialMessage)
	}

	// Create GitHub token Secret if github_token is provided via params
	if req.GithubToken != "" {
		if err := m.createGithubTokenSecret(ctx, session, req.GithubToken); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create github token secret: %v", err)
			// Continue anyway - will fall back to GitHub App authentication if available
		}
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

	// Create team env Secret for team-scoped sessions
	if err := m.createTeamEnvSecret(ctx, session, req); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to create team env secret: %v", err)
		// Continue anyway - session will work without team environment variables
	}

	// Create personal API key Secret for user-scoped sessions
	if req.Scope == entities.ScopeUser {
		if err := m.createPersonalAPIKeySecret(ctx, session, req.UserID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create personal API key secret: %v", err)
			// Continue anyway - session will work without personal API key
		}
	}

	// Create oneshot settings Secret if oneshot is enabled
	if req.Oneshot {
		if err := m.createOneshotSettingsSecret(ctx, session); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to create oneshot settings secret: %v", err)
			// Continue anyway - session will work without oneshot hook
		}
	}

	// Create unified session settings Secret (for future migration)
	if err := m.createSessionSettingsSecret(ctx, session, req, webhookPayload); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to create session settings secret: %v", err)
		// Continue anyway - this is additive and not required for current operation
	}

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

// GetSession returns a session by ID
// If the session is not in memory, it attempts to restore from Kubernetes Service
func (m *KubernetesSessionManager) GetSession(id string) entities.Session {
	// First, check memory
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	if exists {
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
		return nil
	}

	// Don't restore if Service is being deleted
	if svc.DeletionTimestamp != nil {
		return nil
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
	sessionID := svc.Labels["agentapi.proxy/session-id"]

	// Check if session exists in memory
	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()

	if exists {
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

// defaultClaudeJSON is the default claude.json content with required settings
const defaultClaudeJSON = `{
  "hasCompletedOnboarding": true,
  "bypassPermissionsModeAccepted": true
}`

// defaultSettingsJSON is the default settings.json content
const defaultSettingsJSON = `{
  "workspaceFolders": [],
  "recentWorkspaces": [],
  "settings": {
    "mcp.enabled": true
  }
}`

// ensureBaseSecret ensures the base Secret exists, creating it if necessary
func (m *KubernetesSessionManager) ensureBaseSecret(ctx context.Context) error {
	secretName := m.k8sConfig.ClaudeConfigBaseSecret
	if secretName == "" {
		secretName = "claude-config-base"
	}

	// Check if Secret already exists
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		// Secret already exists
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check Secret existence: %w", err)
	}

	// Create the base Secret with default settings
	// Note: GITHUB_TOKEN is added dynamically by init container per session
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "agentapi-proxy",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/component":  "claude-config",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"claude.json":   defaultClaudeJSON,
			"settings.json": defaultSettingsJSON,
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		// If another process created it concurrently, that's fine
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create base Secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created base Secret %s in namespace %s", secretName, m.namespace)
	return nil
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

	// Build user-specific ConfigMap name
	userConfigMapName := fmt.Sprintf("%s-%s",
		m.k8sConfig.ClaudeConfigUserConfigMapPrefix,
		sanitizeLabelValue(req.UserID))

	// No init containers — setup is performed by the main container on startup

	// Determine working directory based on whether repository is specified
	workingDir := "/home/agentapi/workdir"
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		workingDir = "/home/agentapi/workdir/repo"
	}

	// Build envFrom for GitHub secrets
	// Two secrets are used:
	// - GitHubSecretName: Contains GITHUB_TOKEN, GITHUB_APP_PEM, GITHUB_APP_ID, GITHUB_INSTALLATION_ID (authentication)
	// - GitHubConfigSecretName: Contains GITHUB_API, GITHUB_URL (configuration for Enterprise Server)
	var envFrom []corev1.EnvFromSource

	if req.GithubToken != "" {
		// When params.github_token is provided:
		// - Do NOT mount GitHubSecretName (to avoid exposing GITHUB_APP_PEM and other auth credentials)
		// - Mount GitHubConfigSecretName for GITHUB_API/GITHUB_URL settings
		// - Mount session-specific Secret for GITHUB_TOKEN

		// Mount GitHub config Secret (GITHUB_API, GITHUB_URL) if available
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

		// Mount session-specific Secret for GITHUB_TOKEN
		githubTokenSecretName := fmt.Sprintf("%s-github-token", session.ServiceName())
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: githubTokenSecretName,
				},
				Optional: boolPtr(true),
			},
		})
		log.Printf("[K8S_SESSION] Using session-specific GitHub token Secret %s for session %s", githubTokenSecretName, session.id)
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

	// Add personal API key Secret as envFrom for user-scoped sessions
	if req.Scope == entities.ScopeUser {
		personalAPIKeySecretName := fmt.Sprintf("%s-personal-api-key", session.ServiceName())
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: personalAPIKeySecretName,
				},
				Optional: boolPtr(true), // Optional - user may not have generated a personal API key yet
			},
		})
		log.Printf("[K8S_SESSION] Adding personal API key Secret %s for user-scoped session %s", personalAPIKeySecretName, session.id)
	}

	// Add credentials Secrets based on scope
	// For team-scoped sessions: only mount the team's secret (not user secrets)
	// For user-scoped sessions: mount all team secrets and user secret
	if req.Scope == entities.ScopeTeam {
		// Team-scoped: only mount the specific team's credentials
		if req.TeamID != "" {
			secretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(req.TeamID))
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Optional: boolPtr(true),
				},
			})
			log.Printf("[K8S_SESSION] Adding team credentials Secret %s for team-scoped session %s", secretName, session.id)

			// Mount team-specific env vars from TeamConfig (includes service account API key)
			teamEnvSecretName := fmt.Sprintf("agentapi-session-%s-team-env", session.id)
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: teamEnvSecretName,
					},
					Optional: boolPtr(true), // Secret may not exist if team has no config
				},
			})
			log.Printf("[K8S_SESSION] Adding team env Secret %s for team-scoped session %s", teamEnvSecretName, session.id)
		}
		// Note: user-specific credentials are NOT mounted for team-scoped sessions
	} else {
		// User-scoped (or unspecified): mount all team secrets and user secret
		// Add team-based credentials Secrets (agent-env-{org}-{team})
		for _, team := range req.Teams {
			secretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(team))
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Optional: boolPtr(true), // Secret may not exist for all teams
				},
			})
			log.Printf("[K8S_SESSION] Adding team credentials Secret %s for session %s", secretName, session.id)
		}

		// Add user-specific credentials Secret (agent-env-{user-id})
		// This is added last so user-specific values override team values
		if req.UserID != "" {
			userSecretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(req.UserID))
			envFrom = append(envFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: userSecretName,
					},
					Optional: boolPtr(true), // Secret may not exist for all users
				},
			})
			log.Printf("[K8S_SESSION] Adding user credentials Secret %s for session %s", userSecretName, session.id)
		}
	}

	// Build container spec
	container := corev1.Container{
		Name:            "agentapi",
		Image:           m.k8sConfig.Image,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		WorkingDir:      workingDir,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: int32(m.k8sConfig.BasePort),
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
		Command:      []string{"sh", "-c"},
		Args: []string{
			m.buildClaudeStartCommand(),
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/status",
					Port: intstr.FromInt(m.k8sConfig.BasePort),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/status",
					Port: intstr.FromInt(m.k8sConfig.BasePort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}

	// Build volumes
	volumes := m.buildVolumes(session, userConfigMapName)

	// Build containers list (main container + credentials sync sidecar)
	containers := []corev1.Container{container}

	// Add credentials sync sidecar
	// This sidecar watches for credential file changes and syncs them to the user-level Secret
	if sidecar := m.buildCredentialsSyncSidecar(session); sidecar != nil {
		containers = append(containers, *sidecar)
		log.Printf("[K8S_SESSION] Added credentials-sync sidecar for session %s", session.id)
	}

	// Add initial message sender sidecar
	// This sidecar sends the initial message to the agentapi server after it becomes ready
	if sidecar := m.buildInitialMessageSenderSidecar(session); sidecar != nil {
		containers = append(containers, *sidecar)
		log.Printf("[K8S_SESSION] Added initial-message-sender sidecar for session %s", session.id)
	}

	// Add Slack integration sidecar
	// This sidecar handles Slack integration (currently just waits with sleep infinity)
	if sidecar := m.buildSlackSidecar(session); sidecar != nil {
		containers = append(containers, *sidecar)
		log.Printf("[K8S_SESSION] Added slack-integration sidecar for session %s", session.id)
	}

	// Add OpenTelemetry Collector sidecar
	if m.k8sConfig.OtelCollectorEnabled {
		otelcolContainer := m.buildOtelcolSidecar(session, req)
		containers = append(containers, otelcolContainer)
		log.Printf("[K8S_SESSION] Added otelcol sidecar for session %s", session.id)
	}

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

// credentialsSyncScript is the shell script for the credentials sync sidecar
// It watches for changes to .credentials.json and syncs them to the Kubernetes Secret
// Note: This script does NOT require secrets:get permission. It uses patch-first approach
// and falls back to create if the secret doesn't exist.
const credentialsSyncScript = `
#!/bin/sh

CREDENTIALS_PATH="${CREDENTIALS_FILE_PATH:-/home/agentapi/.claude/.credentials.json}"
SECRET_NAME="${SECRET_NAME}"
NAMESPACE="${SECRET_NAMESPACE}"
INTERVAL="${SYNC_INTERVAL:-10}"
LAST_HASH=""

log() {
    echo "[$(date -Iseconds)] [credentials-sync] $1"
}

sync_to_secret() {
    if [ ! -f "$CREDENTIALS_PATH" ]; then
        return
    fi

    CURRENT_HASH=$(sha256sum "$CREDENTIALS_PATH" 2>/dev/null | cut -d' ' -f1 || echo "")

    if [ -z "$CURRENT_HASH" ]; then
        return
    fi

    if [ "$CURRENT_HASH" = "$LAST_HASH" ]; then
        return
    fi

    log "Credentials file changed, syncing to Secret $SECRET_NAME..."

    # Base64 encode the file content
    ENCODED=$(base64 -w0 "$CREDENTIALS_PATH")

    # Generate Secret manifest
    SECRET_YAML=$(cat <<EOFYAML
apiVersion: v1
kind: Secret
metadata:
  name: $SECRET_NAME
  namespace: $NAMESPACE
  labels:
    app.kubernetes.io/name: agentapi-agent-credentials
    app.kubernetes.io/managed-by: agentapi-proxy
type: Opaque
data:
  credentials.json: $ENCODED
EOFYAML
)

    # Try to create the secret first (works if secret does not exist)
    RESULT=$(echo "$SECRET_YAML" | kubectl create -f - 2>&1)
    if [ $? -eq 0 ]; then
        log "Successfully created Secret"
        LAST_HASH="$CURRENT_HASH"
        return
    fi

    # Check if the error is "AlreadyExists" - if so, replace the secret
    if echo "$RESULT" | grep -q "AlreadyExists\|already exists"; then
        RESULT=$(echo "$SECRET_YAML" | kubectl replace -f - 2>&1)
        if [ $? -eq 0 ]; then
            log "Successfully updated Secret"
            LAST_HASH="$CURRENT_HASH"
        else
            log "ERROR: Failed to replace Secret: $RESULT"
        fi
    else
        log "ERROR: Failed to create Secret: $RESULT"
    fi
}

log "Starting credentials sync sidecar"
log "Watching: $CREDENTIALS_PATH"
log "Target Secret: $SECRET_NAME in $NAMESPACE"
log "Sync interval: ${INTERVAL}s"

while true; do
    sync_to_secret
    sleep "$INTERVAL"
done
`

// credentialsSyncSidecarImage is the image used for the credentials sync sidecar
// This image must contain kubectl for interacting with Kubernetes API
const credentialsSyncSidecarImage = "mirror.gcr.io/bitnami/kubectl:latest"

// buildCredentialsSyncSidecar builds the sidecar container for syncing credentials to Secret
func (m *KubernetesSessionManager) buildCredentialsSyncSidecar(session *KubernetesSession) *corev1.Container {
	// Use bitnami/kubectl image which contains kubectl
	sidecarImage := credentialsSyncSidecarImage

	// Secret name is per-user, not per-session
	// Format: agentapi-agent-env-{userID}
	credentialsSecretName := fmt.Sprintf("agentapi-agent-env-%s", sanitizeLabelValue(session.Request().UserID))

	return &corev1.Container{
		Name:            "credentials-sync",
		Image:           sidecarImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{credentialsSyncScript},
		Env: []corev1.EnvVar{
			{Name: "CREDENTIALS_FILE_PATH", Value: "/home/agentapi/.claude/.credentials.json"},
			{Name: "SECRET_NAME", Value: credentialsSecretName},
			{
				Name: "SECRET_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{Name: "SYNC_INTERVAL", Value: "10"},
		},
		VolumeMounts: []corev1.VolumeMount{
			// Mount shared .claude directory (written by setup, read by credentials-sync)
			{
				Name:      "dot-claude",
				MountPath: "/home/agentapi/.claude",
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// initialMessageSenderScript is the shell script for the initial message sender sidecar
// It waits for agentapi to be ready and sends the initial message from the mounted Secret
const initialMessageSenderScript = `
#!/bin/sh
set -e

AGENTAPI_URL="http://localhost:${AGENTAPI_PORT}"
MESSAGE_FILE="/initial-message/message"
SENT_FLAG="/initial-message-state/sent"
MAX_READY_RETRIES=120
MAX_STABLE_RETRIES=60

echo "[INITIAL-MSG] Starting initial message sender sidecar"

# Check if already sent (container restart case)
if [ -f "$SENT_FLAG" ]; then
    echo "[INITIAL-MSG] Initial message already sent (flag file exists), skipping"
    exec sleep infinity
fi

# Check if message file exists
if [ ! -f "$MESSAGE_FILE" ]; then
    echo "[INITIAL-MSG] No initial message file found, nothing to send"
    touch "$SENT_FLAG"
    exec sleep infinity
fi

echo "[INITIAL-MSG] Waiting for agentapi to be ready..."

# Wait for agentapi server to respond
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_READY_RETRIES ]; do
    if curl -sf "${AGENTAPI_URL}/status" > /dev/null 2>&1; then
        echo "[INITIAL-MSG] agentapi is responding"
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    if [ $RETRY_COUNT -eq $MAX_READY_RETRIES ]; then
        echo "[INITIAL-MSG] ERROR: agentapi not ready after ${MAX_READY_RETRIES} retries"
        exec sleep infinity
    fi
    sleep 0.5
done

# Check if user messages already exist (Pod recreated case)
# Only skip if there are messages from the user (role: "user"), not just agent welcome messages
USER_MSG_COUNT=$(curl -sf "${AGENTAPI_URL}/messages" 2>/dev/null | jq '[.messages[] | select(.role == "user")] | length' 2>/dev/null || echo "0")
if [ "$USER_MSG_COUNT" -gt 0 ]; then
    echo "[INITIAL-MSG] User messages already exist (count: ${USER_MSG_COUNT}), skipping initial message"
    touch "$SENT_FLAG"
    exec sleep infinity
fi

# Wait for agent status to be "stable"
echo "[INITIAL-MSG] Waiting for agent status to be stable..."
STABLE_COUNT=0
while [ $STABLE_COUNT -lt $MAX_STABLE_RETRIES ]; do
    STATUS=$(curl -sf "${AGENTAPI_URL}/status" 2>/dev/null | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "")
    if [ "$STATUS" = "stable" ]; then
        echo "[INITIAL-MSG] Agent is stable, ready to send message"
        break
    fi
    STABLE_COUNT=$((STABLE_COUNT + 1))
    if [ $STABLE_COUNT -eq $MAX_STABLE_RETRIES ]; then
        echo "[INITIAL-MSG] ERROR: Agent not stable after ${MAX_STABLE_RETRIES} retries (status: ${STATUS})"
        exec sleep infinity
    fi
    sleep 1
done

# Double-check user message count before sending (race condition prevention)
USER_MSG_COUNT=$(curl -sf "${AGENTAPI_URL}/messages" 2>/dev/null | jq '[.messages[] | select(.role == "user")] | length' 2>/dev/null || echo "0")
if [ "$USER_MSG_COUNT" -gt 0 ]; then
    echo "[INITIAL-MSG] User messages appeared during wait (count: ${USER_MSG_COUNT}), skipping"
    touch "$SENT_FLAG"
    exec sleep infinity
fi

# Read and send message
echo "[INITIAL-MSG] Sending initial message..."
MESSAGE_CONTENT=$(cat "$MESSAGE_FILE")

# Build JSON payload with proper escaping using jq
PAYLOAD=$(printf '%s' "$MESSAGE_CONTENT" | jq -Rs '{content: ., type: "user"}')

MAX_SEND_RETRIES=5
SEND_RETRY_INTERVAL=3
SEND_SUCCESS=false

SEND_COUNT=0
while [ $SEND_COUNT -lt $MAX_SEND_RETRIES ]; do
    SEND_COUNT=$((SEND_COUNT + 1))
    echo "[INITIAL-MSG] Send attempt ${SEND_COUNT}/${MAX_SEND_RETRIES}"

    RESPONSE=$(curl -sf -w "\n%{http_code}" -X POST "${AGENTAPI_URL}/message" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" 2>&1) || true

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | sed '$d')

    if [ "$HTTP_CODE" = "200" ]; then
        echo "[INITIAL-MSG] Initial message sent successfully (HTTP 200)"
        SEND_SUCCESS=true
        break
    fi

    echo "[INITIAL-MSG] Send failed (HTTP ${HTTP_CODE}), response: ${BODY}"

    if [ $SEND_COUNT -lt $MAX_SEND_RETRIES ]; then
        echo "[INITIAL-MSG] Retrying in ${SEND_RETRY_INTERVAL} seconds..."
        sleep $SEND_RETRY_INTERVAL

        # Re-check agent status before retry
        STATUS=$(curl -sf "${AGENTAPI_URL}/status" 2>/dev/null | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "")
        if [ "$STATUS" != "stable" ]; then
            echo "[INITIAL-MSG] Agent status is '${STATUS}', waiting for stable..."
            WAIT_COUNT=0
            while [ $WAIT_COUNT -lt 30 ] && [ "$STATUS" != "stable" ]; do
                sleep 1
                STATUS=$(curl -sf "${AGENTAPI_URL}/status" 2>/dev/null | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "")
                WAIT_COUNT=$((WAIT_COUNT + 1))
            done
            if [ "$STATUS" != "stable" ]; then
                echo "[INITIAL-MSG] Agent not stable after waiting (status: ${STATUS})"
            fi
        fi
    fi
done

if [ "$SEND_SUCCESS" = "true" ]; then
    touch "$SENT_FLAG"
    # Verify the message was actually stored
    sleep 1
    USER_MSG_COUNT=$(curl -sf "${AGENTAPI_URL}/messages" 2>/dev/null | jq '[.messages[] | select(.role == "user")] | length' 2>/dev/null || echo "0")
    if [ "$USER_MSG_COUNT" -gt 0 ]; then
        echo "[INITIAL-MSG] Verified: user message exists (count: ${USER_MSG_COUNT})"
    else
        echo "[INITIAL-MSG] WARNING: Message sent but verification shows no user messages"
    fi
else
    echo "[INITIAL-MSG] ERROR: Failed to send initial message after ${MAX_SEND_RETRIES} attempts"
fi

# Keep container running (prevents restart loop)
exec sleep infinity
`

// buildInitialMessageSenderSidecar builds the sidecar container for sending initial messages
func (m *KubernetesSessionManager) buildInitialMessageSenderSidecar(session *KubernetesSession) *corev1.Container {
	// Only create sidecar if there's an initial message
	if session.Request().InitialMessage == "" {
		return nil
	}

	return &corev1.Container{
		Name:            "initial-message-sender",
		Image:           m.k8sConfig.Image,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "initial-message",
				MountPath: "/initial-message",
				ReadOnly:  true,
			},
			{
				Name:      "initial-message-state",
				MountPath: "/initial-message-state",
			},
		},
		Env: []corev1.EnvVar{
			{Name: "AGENTAPI_PORT", Value: fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		},
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{initialMessageSenderScript},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// buildSlackSidecar builds the sidecar container for Slack integration
// This sidecar currently just waits with sleep infinity
func (m *KubernetesSessionManager) buildSlackSidecar(session *KubernetesSession) *corev1.Container {
	// Only create sidecar if Slack parameters are provided
	req := session.Request()
	if req.SlackParams == nil || req.SlackParams.Channel == "" {
		return nil
	}

	// Use alpine as a lightweight image for sleep infinity
	sidecarImage := "alpine:latest"

	return &corev1.Container{
		Name:            "slack-integration",
		Image:           sidecarImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c"},
		Args:            []string{"sleep infinity"},
		Env: []corev1.EnvVar{
			{Name: "SLACK_CHANNEL", Value: req.SlackParams.Channel},
			{Name: "SLACK_THREAD_TS", Value: req.SlackParams.ThreadTS},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "claude-agentapi-history",
				MountPath: "/opt/claude-agentapi",
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// createInitialMessageSecret creates a Secret containing the initial message
func (m *KubernetesSessionManager) createInitialMessageSecret(
	ctx context.Context,
	session *KubernetesSession,
	message string,
) error {
	secretName := fmt.Sprintf("%s-initial-message", session.ServiceName())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.Request().UserID),
				"agentapi.proxy/resource":   "initial-message",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"message": []byte(message),
		},
	}

	_, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create initial message secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created initial message Secret %s for session %s", secretName, session.id)
	return nil
}

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

// deleteInitialMessageSecret deletes the initial message Secret for a session
func (m *KubernetesSessionManager) deleteInitialMessageSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-initial-message", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete initial message secret: %w", err)
	}
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

// deleteTeamEnvSecret deletes the team env Secret for a session
func (m *KubernetesSessionManager) deleteTeamEnvSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("agentapi-session-%s-team-env", session.id)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete team env secret: %w", err)
	}
	return nil
}

// getInitialMessageFromSecret retrieves the initial message from Secret for session restoration
func (m *KubernetesSessionManager) getInitialMessageFromSecret(ctx context.Context, serviceName string) string {
	secretName := fmt.Sprintf("%s-initial-message", serviceName)
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		// Secret may not exist if no initial message was provided
		return ""
	}
	if message, ok := secret.Data["message"]; ok {
		return string(message)
	}
	return ""
}

// deleteGithubTokenSecret deletes the GitHub token Secret for a session
func (m *KubernetesSessionManager) deleteGithubTokenSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-github-token", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete github token secret: %w", err)
	}
	return nil
}

// createGithubTokenSecret creates a Secret containing the GitHub token
// This is used when params.github_token is provided to override GITHUB_TOKEN
// from GitHubSecretName. Other GitHub settings (GITHUB_API, GITHUB_URL) are
// still read from GitHubSecretName.
func (m *KubernetesSessionManager) createGithubTokenSecret(
	ctx context.Context,
	session *KubernetesSession,
	token string,
) error {
	secretName := fmt.Sprintf("%s-github-token", session.ServiceName())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.Request().UserID),
				"agentapi.proxy/resource":   "github-token",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"GITHUB_TOKEN": token,
		},
	}

	_, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create github token secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created GitHub token Secret %s for session %s", secretName, session.id)
	return nil
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

// createPersonalAPIKeySecret creates a Secret containing the personal API key
// This is used for user-scoped sessions to provide the user's personal API key
func (m *KubernetesSessionManager) createPersonalAPIKeySecret(
	ctx context.Context,
	session *KubernetesSession,
	userID string,
) error {
	// Skip if personal API key repository is not set
	if m.personalAPIKeyRepo == nil {
		log.Printf("[K8S_SESSION] Personal API key repository not set, skipping secret creation")
		return nil
	}

	// Try to get existing personal API key
	apiKey, err := m.personalAPIKeyRepo.FindByUserID(ctx, entities.UserID(userID))
	if err != nil {
		// If no API key exists, create a new one automatically
		log.Printf("[K8S_SESSION] No personal API key found for user %s, creating new one", userID)

		// Generate API key
		generatedKey, err := generatePersonalAPIKey()
		if err != nil {
			return fmt.Errorf("failed to generate personal API key: %w", err)
		}

		// Create new PersonalAPIKey entity
		apiKey = entities.NewPersonalAPIKey(entities.UserID(userID), generatedKey)

		// Save to repository
		if err := m.personalAPIKeyRepo.Save(ctx, apiKey); err != nil {
			return fmt.Errorf("failed to save personal API key for user %s: %w", userID, err)
		}

		log.Printf("[K8S_SESSION] Created personal API key for user %s", userID)
	}

	secretName := fmt.Sprintf("%s-personal-api-key", session.ServiceName())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(userID),
				"agentapi.proxy/resource":   "personal-api-key",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"AGENTAPI_KEY": apiKey.APIKey(),
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create personal API key secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created personal API key Secret %s for session %s", secretName, session.id)
	return nil
}

// buildVolumes builds the volume configuration for the session pod
func (m *KubernetesSessionManager) buildVolumes(session *KubernetesSession, userConfigMapName string) []corev1.Volume {
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

	// Build credentials volume based on scope
	// For team-scoped sessions: use EmptyDir (no user credentials)
	// For user-scoped sessions: mount user's credentials Secret
	var credentialsVolume corev1.Volume
	if session.Request().Scope == entities.ScopeTeam {
		// Team-scoped: do not mount user credentials, use EmptyDir
		credentialsVolume = corev1.Volume{
			Name: "claude-credentials",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
		log.Printf("[K8S_SESSION] Using EmptyDir for credentials volume (team-scoped session %s)", session.id)
	} else {
		// User-scoped: mount user's credentials Secret
		// Credentials Secret name follows the pattern: agentapi-agent-env-{userID}
		credentialsSecretName := fmt.Sprintf("agentapi-agent-env-%s", sanitizeLabelValue(session.Request().UserID))
		credentialsVolume = corev1.Volume{
			Name: "claude-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: credentialsSecretName,
					Optional:   boolPtr(true), // Optional - user may not have logged in yet
				},
			},
		}
	}

	volumes := []corev1.Volume{
		// Workdir volume (PVC or EmptyDir based on configuration)
		workdirVolume,
		// Credentials volume (Secret for user-scoped, EmptyDir for team-scoped)
		// This Secret is managed by the credentials-sync sidecar for user-scoped sessions
		credentialsVolume,
		// dot-claude EmptyDir – shared between main container and credentials-sync sidecar
		// setup writes ~/.claude/ here; credentials-sync reads .credentials.json from here
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

	// session-settings Secret – the single source of truth consumed by the setup init container
	sessionSettingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)
	volumes = append(volumes, corev1.Volume{
		Name: "session-settings",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: sessionSettingsSecretName,
			},
		},
	})

	// Add initial message volumes if initial message is provided
	if session.Request() != nil && session.Request().InitialMessage != "" {
		initialMsgSecretName := fmt.Sprintf("%s-initial-message", session.ServiceName())
		volumes = append(volumes,
			// Secret containing the initial message content
			corev1.Volume{
				Name: "initial-message",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: initialMsgSecretName,
						Optional:   boolPtr(true),
					},
				},
			},
			// EmptyDir for tracking sent state (prevents double-send on container restart)
			corev1.Volume{
				Name: "initial-message-state",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		)
	}

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

	// Note: Personal API key is now mounted as environment variable via envFrom
	// No need to mount as volume

	// Add MCP server configuration volumes if enabled
	// mcp-config EmptyDir is written by setup on main container startup (via Compile)
	if m.k8sConfig.MCPServersEnabled {
		volumes = append(volumes, m.buildMCPVolumes(session)...)
	}

	// Add OpenTelemetry Collector ConfigMap volume
	if m.k8sConfig.OtelCollectorEnabled {
		volumes = append(volumes, corev1.Volume{
			Name: "otelcol-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "otelcol-config",
					},
				},
			},
		})
	}

	// Add EmptyDir for claude-agentapi history output
	volumes = append(volumes, corev1.Volume{
		Name: "claude-agentapi-history",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumes
}

// buildMCPVolumes builds the volumes for MCP server configuration
func (m *KubernetesSessionManager) buildMCPVolumes(session *KubernetesSession) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Build projected volume sources for mcp-config-source
	var projectedSources []corev1.VolumeProjection

	// Add base MCP config Secret
	if m.k8sConfig.MCPServersBaseSecret != "" {
		projectedSources = append(projectedSources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.MCPServersBaseSecret,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  "mcp-servers.json",
						Path: "base/mcp-servers.json",
					},
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add team MCP config Secrets
	if m.k8sConfig.MCPServersEnabled && session.Request() != nil {
		for i, team := range session.Request().Teams {
			secretName := fmt.Sprintf("mcp-servers-%s", sanitizeSecretName(team))
			projectedSources = append(projectedSources, corev1.VolumeProjection{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "mcp-servers.json",
							Path: fmt.Sprintf("team/%d/mcp-servers.json", i),
						},
					},
					Optional: boolPtr(true),
				},
			})
		}
	}

	// Add user MCP config Secret
	if m.k8sConfig.MCPServersEnabled && session.Request() != nil && session.Request().UserID != "" {
		userSecretName := fmt.Sprintf("mcp-servers-%s", sanitizeSecretName(session.Request().UserID))
		projectedSources = append(projectedSources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: userSecretName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  "mcp-servers.json",
						Path: "user/mcp-servers.json",
					},
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add mcp-config-source as projected volume
	if len(projectedSources) > 0 {
		volumes = append(volumes, corev1.Volume{
			Name: "mcp-config-source",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: projectedSources,
				},
			},
		})
	} else {
		// Use an EmptyDir if no secrets configured
		volumes = append(volumes, corev1.Volume{
			Name: "mcp-config-source",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

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

// createTeamEnvSecret creates a Secret with team configuration environment variables
// for team-scoped sessions. This includes service account API key and team-specific env vars.
func (m *KubernetesSessionManager) createTeamEnvSecret(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) error {
	// Only create for team-scoped sessions
	if req.Scope != entities.ScopeTeam || req.TeamID == "" {
		return nil
	}

	// Skip if team config repository is not set
	if m.teamConfigRepo == nil {
		log.Printf("[K8S_SESSION] TeamConfigRepository not set, skipping team env secret creation for session %s", session.id)
		return nil
	}

	// Fetch team config
	teamConfig, err := m.teamConfigRepo.FindByTeamID(ctx, req.TeamID)
	if err != nil {
		// Team config not found is not an error - team may not have configuration yet
		log.Printf("[K8S_SESSION] Team config not found for team %s, skipping team env secret: %v", req.TeamID, err)
		return nil
	}

	// Build environment variables map
	envData := make(map[string][]byte)

	// Add service account API key if present
	if sa := teamConfig.ServiceAccount(); sa != nil {
		envData["AGENTAPI_KEY"] = []byte(sa.APIKey())
		log.Printf("[K8S_SESSION] Adding service account API key (AGENTAPI_KEY) to team env secret for session %s", session.id)
	}

	// Add team-specific environment variables
	for key, value := range teamConfig.EnvVars() {
		envData[key] = []byte(value)
	}

	// Skip if no environment variables to add
	if len(envData) == 0 {
		log.Printf("[K8S_SESSION] No environment variables in team config for team %s, skipping secret creation", req.TeamID)
		return nil
	}

	// Create Secret
	secretName := fmt.Sprintf("agentapi-session-%s-team-env", session.id)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/team-id":    sanitizeLabelValue(req.TeamID),
				"agentapi.proxy/managed-by": "session-manager",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: envData,
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create team env secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created team env secret %s for session %s with %d environment variables", secretName, session.id, len(envData))
	return nil
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
				session.SetStatus("active")
				log.Printf("[K8S_SESSION] Session %s is now active", session.id)

				// Note: Initial message is now sent by the initial-message-sender sidecar
				// within the Pod, not by the proxy

				// Continue watching for changes
				m.watchDeploymentStatus(ctx, session)
				return
			}

			session.SetStatus("starting")
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

	// Delete initial message Secret
	if err := m.deleteInitialMessageSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("initial-message-secret: %v", err))
	}

	// Delete GitHub token Secret
	if err := m.deleteGithubTokenSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("github-token-secret: %v", err))
	}

	// Delete webhook payload Secret
	if err := m.deleteWebhookPayloadSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("webhook-payload-secret: %v", err))
	}

	// Delete team env Secret (for team-scoped sessions)
	if err := m.deleteTeamEnvSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("team-env-secret: %v", err))
	}

	// Delete session settings Secret
	if err := m.deleteSessionSettingsSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("session-settings-secret: %v", err))
	}

	// Delete personal API key Secret (bug fix - was not being deleted before)
	if err := m.deletePersonalAPIKeySecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("personal-api-key-secret: %v", err))
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
	// Base selector for agentapi sessions
	selector := "app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session"

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

		// Add claude-agentapi specific environment variables
		if req.AgentType == "claude-agentapi" {
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
			corev1.EnvVar{Name: "AGENTAPI_CLONE_DIR", Value: req.RepoInfo.CloneDir},
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

	// Note: Bedrock settings are now loaded via envFrom from agent-env-{name} Secret
	// which is synced by CredentialsSecretSyncer when settings are updated via API

	return envVars
}

// buildOtelcolEnvVars creates environment variables for the otelcol sidecar
func (m *KubernetesSessionManager) buildOtelcolEnvVars(session *KubernetesSession, req *entities.RunServerRequest) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "SESSION_ID", Value: session.id},
		{Name: "USER_ID", Value: req.UserID},
	}

	// Team ID - use "-" as placeholder for empty values
	teamID := "-"
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		teamID = req.TeamID
	}
	envVars = append(envVars, corev1.EnvVar{Name: "TEAM_ID", Value: teamID})

	// Schedule ID (from tags) - use "-" as placeholder for empty values
	scheduleID := "-"
	if req.Tags != nil {
		if val, ok := req.Tags["schedule_id"]; ok && val != "" {
			scheduleID = val
		}
	}
	envVars = append(envVars, corev1.EnvVar{Name: "SCHEDULE_ID", Value: scheduleID})

	// Webhook ID (from tags) - use "-" as placeholder for empty values
	webhookID := "-"
	if req.Tags != nil {
		if val, ok := req.Tags["webhook_id"]; ok && val != "" {
			webhookID = val
		}
	}
	envVars = append(envVars, corev1.EnvVar{Name: "WEBHOOK_ID", Value: webhookID})

	// Agent Type - use "-" as placeholder for empty values
	agentType := "-"
	if req.AgentType != "" {
		agentType = req.AgentType
	}
	envVars = append(envVars, corev1.EnvVar{Name: "AGENT_TYPE", Value: agentType})

	return envVars
}

// sanitizeLabelKey sanitizes a string to be used as a Kubernetes label key
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
		// dot-claude EmptyDir – setup writes .claude/ here; shared with credentials-sync sidecar
		{
			Name:      "dot-claude",
			MountPath: "/home/agentapi/.claude",
		},
		// session-settings Secret – read by setup on startup
		{
			Name:      "session-settings",
			MountPath: "/session-settings",
			ReadOnly:  true,
		},
		// credentials-config – read by setup on startup
		{
			Name:      "claude-credentials",
			MountPath: "/credentials-config",
			ReadOnly:  true,
		},
		// notification subscriptions source – read by setup on startup
		{
			Name:      "notification-subscriptions-source",
			MountPath: "/notification-subscriptions-source",
			ReadOnly:  true,
		},
	}

	// Add webhook payload volume mount if webhook payload is provided
	if len(session.WebhookPayload()) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "webhook-payload",
			MountPath: "/opt/webhook/payload.json",
			SubPath:   "payload.json",
			ReadOnly:  true,
		})
	}

	// Note: Personal API key is now mounted as environment variable via envFrom
	// No need to mount as volume

	// Add claude-agentapi history volume mount
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "claude-agentapi-history",
		MountPath: "/opt/claude-agentapi",
	})

	return volumeMounts
}

// buildClaudeStartCommand builds the agent start command based on agent type
// For agent_type == "claude-agentapi": runs claude-agentapi with CLAUDE_ARGS as CLI options
// For default/agentapi: uses resume fallback pattern "claude -c [args] || claude [args]"
func (m *KubernetesSessionManager) buildClaudeStartCommand() string {
	baseCmd := `
# Run session setup (write-pem, clone-repo, compile settings, sync extra)
echo "[STARTUP] Running session setup"
agentapi-proxy helpers setup \
  --input /session-settings/settings.yaml \
  --credentials-file /credentials-config/credentials.json \
  --notification-subscriptions /notification-subscriptions-source \
  --notifications-dir /home/agentapi/.notifications \
  --register-marketplaces
echo "[STARTUP] Session setup complete"

# Source session env file generated by setup
if [ -f /home/agentapi/.session/env ]; then
    echo "[STARTUP] Sourcing session env file"
    set -a
    . /home/agentapi/.session/env
    set +a
fi

# Determine which agent to start based on AGENTAPI_AGENT_TYPE
if [ "$AGENTAPI_AGENT_TYPE" = "claude-agentapi" ]; then
    # Update claude-agentapi to the latest version
    echo "[STARTUP] Updating claude-agentapi to the latest version"
    if bun install -g @takutakahashi/claude-agentapi; then
        echo "[STARTUP] claude-agentapi update successful"
    else
        echo "[STARTUP] Warning: Failed to update claude-agentapi, continuing with existing version"
    fi

    # Start claude-agentapi
    echo "[STARTUP] Starting claude-agentapi on $HOST:$PORT"

    # Build claude-agentapi options
    CLAUDE_AGENTAPI_OPTS=""

    # Add --output-file for history logging
    CLAUDE_AGENTAPI_OPTS="--output-file /opt/claude-agentapi/history.jsonl"
    echo "[STARTUP] Using history output file: /opt/claude-agentapi/history.jsonl"

    # Add --mcp-config if MCP config file exists
    if [ -f /home/agentapi/.mcp-config/merged.json ]; then
        CLAUDE_AGENTAPI_OPTS="$CLAUDE_AGENTAPI_OPTS --mcp-config /home/agentapi/.mcp-config/merged.json"
        echo "[STARTUP] Using MCP config: /home/agentapi/.mcp-config/merged.json"
    fi

    # Append CLAUDE_ARGS if set (as CLI options)
    if [ -n "$CLAUDE_ARGS" ]; then
        CLAUDE_AGENTAPI_OPTS="$CLAUDE_AGENTAPI_OPTS $CLAUDE_ARGS"
        echo "[STARTUP] Using CLAUDE_ARGS: $CLAUDE_ARGS"
    fi

    echo "[STARTUP] Executing: claude-agentapi $CLAUDE_AGENTAPI_OPTS"
    exec claude-agentapi $CLAUDE_AGENTAPI_OPTS
else
    # Start agentapi with Claude (original behavior)
    echo "[STARTUP] Starting agentapi"

    CLAUDE_ARGS_FULL=""

    # Add --mcp-config if MCP config file exists
    if [ -f /home/agentapi/.mcp-config/merged.json ]; then
        CLAUDE_ARGS_FULL="--mcp-config /home/agentapi/.mcp-config/merged.json"
        echo "[STARTUP] Using MCP config: /home/agentapi/.mcp-config/merged.json"
    fi

    # Add CLAUDE_ARGS if set
    if [ -n "$CLAUDE_ARGS" ]; then
        CLAUDE_ARGS_FULL="$CLAUDE_ARGS_FULL $CLAUDE_ARGS"
    fi

    # Build command with resume fallback (claude -c || claude)
    # This attempts to resume an existing session first, falling back to a new session if not available
    if [ -n "$CLAUDE_ARGS_FULL" ]; then
        CLAUDE_CMD="claude -c $CLAUDE_ARGS_FULL || claude $CLAUDE_ARGS_FULL"
    else
        CLAUDE_CMD="claude -c || claude"
    fi

    echo "[STARTUP] Starting agentapi with resume fallback: $CLAUDE_CMD"
    exec agentapi server --allowed-hosts '*' --allowed-origins '*' --port $AGENTAPI_PORT -- sh -c "$CLAUDE_CMD"
fi
`
	return baseCmd
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

	// Restore initial message from Secret
	initialMessage := m.getInitialMessageFromSecret(context.Background(), svc.Name)

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

	// Restore initial message from Secret
	initialMessage := m.getInitialMessageFromSecret(context.Background(), svc.Name)

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

// buildOtelcolSidecar builds the OpenTelemetry Collector sidecar container
func (m *KubernetesSessionManager) buildOtelcolSidecar(session *KubernetesSession, req *entities.RunServerRequest) corev1.Container {
	image := m.k8sConfig.OtelCollectorImage
	if image == "" {
		image = "otel/opentelemetry-collector-contrib:0.143.1"
	}

	// Parse resource limits
	cpuRequest := "100m"
	if m.k8sConfig.OtelCollectorCPURequest != "" {
		cpuRequest = m.k8sConfig.OtelCollectorCPURequest
	}
	cpuLimit := "200m"
	if m.k8sConfig.OtelCollectorCPULimit != "" {
		cpuLimit = m.k8sConfig.OtelCollectorCPULimit
	}
	memoryRequest := "128Mi"
	if m.k8sConfig.OtelCollectorMemoryRequest != "" {
		memoryRequest = m.k8sConfig.OtelCollectorMemoryRequest
	}
	memoryLimit := "256Mi"
	if m.k8sConfig.OtelCollectorMemoryLimit != "" {
		memoryLimit = m.k8sConfig.OtelCollectorMemoryLimit
	}

	exporterPort := 9090
	if m.k8sConfig.OtelCollectorExporterPort > 0 {
		exporterPort = m.k8sConfig.OtelCollectorExporterPort
	}

	return corev1.Container{
		Name:            "otelcol",
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Args: []string{
			"--config=/etc/otelcol/otel-collector-config.yaml",
		},
		Env: m.buildOtelcolEnvVars(session, req),
		Ports: []corev1.ContainerPort{
			{
				Name:          "prometheus",
				ContainerPort: int32(exporterPort),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "otelcol-config",
				MountPath: "/etc/otelcol",
				ReadOnly:  true,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuRequest),
				corev1.ResourceMemory: resource.MustParse(memoryRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuLimit),
				corev1.ResourceMemory: resource.MustParse(memoryLimit),
			},
		},
	}
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

		// Add claude-agentapi specific environment variables
		if req.AgentType == "claude-agentapi" {
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
		env["AGENTAPI_CLONE_DIR"] = req.RepoInfo.CloneDir
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
		// When params.github_token is provided
		if m.k8sConfig.GitHubConfigSecretName != "" {
			secretNames = append(secretNames, m.k8sConfig.GitHubConfigSecretName)
		}
		githubTokenSecretName := fmt.Sprintf("%s-github-token", session.ServiceName())
		secretNames = append(secretNames, githubTokenSecretName)
	} else if m.k8sConfig.GitHubSecretName != "" {
		// When params.github_token is NOT provided
		secretNames = append(secretNames, m.k8sConfig.GitHubSecretName)
		if m.k8sConfig.GitHubConfigSecretName != "" {
			secretNames = append(secretNames, m.k8sConfig.GitHubConfigSecretName)
		}
	}

	// Add personal API key Secret for user-scoped sessions
	if req.Scope == entities.ScopeUser {
		personalAPIKeySecretName := fmt.Sprintf("%s-personal-api-key", session.ServiceName())
		secretNames = append(secretNames, personalAPIKeySecretName)
	}

	// Add credentials Secrets based on scope
	if req.Scope == entities.ScopeTeam {
		// Team-scoped: only mount the specific team's credentials
		if req.TeamID != "" {
			secretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(req.TeamID))
			secretNames = append(secretNames, secretName)

			// Mount team-specific env vars from TeamConfig
			teamEnvSecretName := fmt.Sprintf("agentapi-session-%s-team-env", session.id)
			secretNames = append(secretNames, teamEnvSecretName)
		}
	} else {
		// User-scoped: mount all team secrets and user secret
		for _, team := range req.Teams {
			secretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(team))
			secretNames = append(secretNames, secretName)
		}

		// Add user-specific credentials Secret (added last for highest priority)
		if req.UserID != "" {
			userSecretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(req.UserID))
			secretNames = append(secretNames, userSecretName)
		}
	}

	// Expand secrets into env map
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

	settings.Env = env

	// Merge settings.json from base/team/user/oneshot Secrets (proxy-side, before Pod starts)
	mergedSettingsJSON, mergedMCPServers := m.mergeSettingsAndMCP(ctx, session, req)

	// Claude config
	settings.Claude = sessionsettings.ClaudeConfig{
		ClaudeJSON: map[string]interface{}{
			"hasCompletedOnboarding":        true,
			"bypassPermissionsModeAccepted": true,
		},
		SettingsJSON: mergedSettingsJSON,
		MCPServers:   mergedMCPServers,
	}

	// Repository info
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		settings.Repository = &sessionsettings.RepositoryConfig{
			FullName: req.RepoInfo.FullName,
			CloneDir: req.RepoInfo.CloneDir,
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

	return settings
}

// createSessionSettingsSecret creates the unified session settings Secret.
// This Secret consolidates all session configuration into a single YAML file.
func (m *KubernetesSessionManager) createSessionSettingsSecret(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
	webhookPayload []byte,
) error {
	settings := m.buildSessionSettings(ctx, session, req, webhookPayload)

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

// deletePersonalAPIKeySecret deletes the personal API key Secret.
// This fixes the existing bug where personal-api-key secrets were not being cleaned up.
func (m *KubernetesSessionManager) deletePersonalAPIKeySecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-personal-api-key", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete personal API key secret: %w", err)
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

// mergeSettingsAndMCP reads settings.json and mcp-servers.json from the relevant
// Kubernetes Secrets (base, team[], user, oneshot) and merges them on the proxy side,
// returning the merged structures ready to embed into SessionSettings.
// This eliminates the need for separate merge-settings and setup-mcp init containers.
func (m *KubernetesSessionManager) mergeSettingsAndMCP(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
) (settingsJSON map[string]interface{}, mcpServers map[string]interface{}) {
	// --- settings.json merge ---
	settingsDirs := []settings.SettingsConfig{}

	// Helper: read settings.json from a Secret key and unmarshal
	readSettingsSecret := func(secretName, key string) *settings.SettingsConfig {
		secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Printf("[K8S_SESSION] Warning: failed to read settings secret %s: %v", secretName, err)
			}
			return nil
		}
		data, ok := secret.Data[key]
		if !ok {
			return nil
		}
		var cfg settings.SettingsConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to parse settings.json from secret %s: %v", secretName, err)
			return nil
		}
		return &cfg
	}

	// 1. base
	if m.k8sConfig.SettingsBaseSecret != "" {
		if cfg := readSettingsSecret(m.k8sConfig.SettingsBaseSecret, "settings.json"); cfg != nil {
			settingsDirs = append(settingsDirs, *cfg)
		}
	}

	// 2. team (in order)
	for _, team := range req.Teams {
		secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(team))
		if cfg := readSettingsSecret(secretName, "settings.json"); cfg != nil {
			settingsDirs = append(settingsDirs, *cfg)
		}
	}
	// team-scoped single team
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.TeamID))
		if cfg := readSettingsSecret(secretName, "settings.json"); cfg != nil {
			settingsDirs = append(settingsDirs, *cfg)
		}
	}

	// 3. user
	if req.UserID != "" {
		secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(req.UserID))
		if cfg := readSettingsSecret(secretName, "settings.json"); cfg != nil {
			settingsDirs = append(settingsDirs, *cfg)
		}
	}

	// 4. oneshot (highest priority)
	if req.Oneshot {
		oneshotSecretName := fmt.Sprintf("%s-oneshot-settings", session.ServiceName())
		if cfg := readSettingsSecret(oneshotSecretName, "settings.json"); cfg != nil {
			settingsDirs = append(settingsDirs, *cfg)
		}
	}

	// Merge all settings configs
	mergedSettings := settings.MergeInMemory(settingsDirs)

	// Convert merged SettingsConfig → map[string]interface{} for SessionSettings.Claude.SettingsJSON
	if mergedSettings != nil {
		raw, err := json.Marshal(mergedSettings)
		if err == nil {
			if err := json.Unmarshal(raw, &settingsJSON); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to convert merged settings to map: %v", err)
				settingsJSON = nil
			}
		}
	}

	// --- mcp-servers.json merge ---
	if m.k8sConfig.MCPServersEnabled {
		readMCPSecret := func(secretName, key string) *mcp.MCPConfig {
			secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					log.Printf("[K8S_SESSION] Warning: failed to read mcp secret %s: %v", secretName, err)
				}
				return nil
			}
			data, ok := secret.Data[key]
			if !ok {
				return nil
			}
			var cfg mcp.MCPConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to parse mcp-servers.json from secret %s: %v", secretName, err)
				return nil
			}
			return &cfg
		}

		mcpConfigs := []*mcp.MCPConfig{}

		// base MCP
		if m.k8sConfig.SettingsBaseSecret != "" {
			if cfg := readMCPSecret(m.k8sConfig.SettingsBaseSecret, "mcp-servers.json"); cfg != nil {
				mcpConfigs = append(mcpConfigs, cfg)
			}
		}

		// team MCP (in order)
		for i, team := range req.Teams {
			secretName := fmt.Sprintf("mcp-servers-%s", sanitizeSecretName(team))
			_ = i
			if cfg := readMCPSecret(secretName, "mcp-servers.json"); cfg != nil {
				mcpConfigs = append(mcpConfigs, cfg)
			}
		}
		if req.Scope == entities.ScopeTeam && req.TeamID != "" {
			secretName := fmt.Sprintf("mcp-servers-%s", sanitizeSecretName(req.TeamID))
			if cfg := readMCPSecret(secretName, "mcp-servers.json"); cfg != nil {
				mcpConfigs = append(mcpConfigs, cfg)
			}
		}

		// user MCP
		if req.UserID != "" {
			secretName := fmt.Sprintf("mcp-servers-%s", sanitizeSecretName(req.UserID))
			if cfg := readMCPSecret(secretName, "mcp-servers.json"); cfg != nil {
				mcpConfigs = append(mcpConfigs, cfg)
			}
		}

		// Merge all MCP configs (later overrides earlier)
		merged := &mcp.MCPConfig{MCPServers: make(map[string]mcp.MCPServer)}
		for _, cfg := range mcpConfigs {
			for name, server := range cfg.MCPServers {
				merged.MCPServers[name] = server
			}
		}

		if len(merged.MCPServers) > 0 {
			raw, err := json.Marshal(merged.MCPServers)
			if err == nil {
				if err := json.Unmarshal(raw, &mcpServers); err != nil {
					log.Printf("[K8S_SESSION] Warning: failed to convert merged MCP servers to map: %v", err)
					mcpServers = nil
				}
			}
		}
	}

	return settingsJSON, mcpServers
}
