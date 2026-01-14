package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
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
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// KubernetesSessionManager manages sessions using Kubernetes Deployments
type KubernetesSessionManager struct {
	config       *config.Config
	k8sConfig    *config.KubernetesSessionConfig
	client       kubernetes.Interface
	verbose      bool
	logger       *logger.Logger
	sessions     map[string]*KubernetesSession
	mutex        sync.RWMutex
	namespace    string
	settingsRepo repositories.SettingsRepository
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
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest) (entities.Session, error) {
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
		if len(filter.Tags) > 0 {
			matchAllTags := true
			sessionTags := session.Tags()
			for tagKey, tagValue := range filter.Tags {
				if sessionTagValue, exists := sessionTags[tagKey]; !exists || sessionTagValue != tagValue {
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

// setupMCPServersScript is the shell script executed by the init container to merge MCP server configurations
// It uses the agentapi-proxy helpers merge-mcp-config command to merge configurations from multiple directories
const setupMCPServersScript = `
set -e

# Check if any MCP config directories exist
MCP_DIRS=""
if [ -d "/mcp-config-source/base" ] && [ "$(ls -A /mcp-config-source/base 2>/dev/null)" ]; then
    MCP_DIRS="/mcp-config-source/base"
fi

# Add team directories (numbered: /mcp-config-source/team/0, /mcp-config-source/team/1, etc.)
for team_dir in /mcp-config-source/team/*/; do
    if [ -d "$team_dir" ] && [ "$(ls -A "$team_dir" 2>/dev/null)" ]; then
        if [ -n "$MCP_DIRS" ]; then
            MCP_DIRS="$MCP_DIRS,$team_dir"
        else
            MCP_DIRS="$team_dir"
        fi
    fi
done

if [ -d "/mcp-config-source/user" ] && [ "$(ls -A /mcp-config-source/user 2>/dev/null)" ]; then
    if [ -n "$MCP_DIRS" ]; then
        MCP_DIRS="$MCP_DIRS,/mcp-config-source/user"
    else
        MCP_DIRS="/mcp-config-source/user"
    fi
fi

if [ -z "$MCP_DIRS" ]; then
    echo "[MCP] No MCP server configurations found, skipping"
    exit 0
fi

echo "[MCP] Merging MCP configurations from: $MCP_DIRS"

# Create output directory
mkdir -p /mcp-config

# Merge MCP configurations using agentapi-proxy helper
agentapi-proxy helpers merge-mcp-config \
    --input-dirs "$MCP_DIRS" \
    --output /mcp-config/merged.json \
    --expand-env \
    --verbose

echo "[MCP] MCP server configuration complete"
`

// mergeSettingsScript is the shell script executed by the init container to merge settings configurations
// It uses the agentapi-proxy helpers merge-settings-config command to merge configurations from multiple directories
const mergeSettingsScript = `
set -e

# Check if any settings config directories exist
SETTINGS_DIRS=""
if [ -d "/settings-config-source/base" ] && [ "$(ls -A /settings-config-source/base 2>/dev/null)" ]; then
    SETTINGS_DIRS="/settings-config-source/base"
fi

# Add team directories (numbered: /settings-config-source/team/0, /settings-config-source/team/1, etc.)
for team_dir in /settings-config-source/team/*/; do
    if [ -d "$team_dir" ] && [ "$(ls -A "$team_dir" 2>/dev/null)" ]; then
        if [ -n "$SETTINGS_DIRS" ]; then
            SETTINGS_DIRS="$SETTINGS_DIRS,$team_dir"
        else
            SETTINGS_DIRS="$team_dir"
        fi
    fi
done

if [ -d "/settings-config-source/user" ] && [ "$(ls -A /settings-config-source/user 2>/dev/null)" ]; then
    if [ -n "$SETTINGS_DIRS" ]; then
        SETTINGS_DIRS="$SETTINGS_DIRS,/settings-config-source/user"
    else
        SETTINGS_DIRS="/settings-config-source/user"
    fi
fi

if [ -z "$SETTINGS_DIRS" ]; then
    echo "[SETTINGS] No settings configurations found, skipping merge"
    exit 0
fi

echo "[SETTINGS] Merging settings from: $SETTINGS_DIRS"

# Create output directory
mkdir -p /settings-config

# Merge settings configurations using agentapi-proxy helper
agentapi-proxy helpers merge-settings-config \
    --input-dirs "$SETTINGS_DIRS" \
    --output /settings-config/settings.json \
    --verbose

echo "[SETTINGS] Settings configuration merge complete"
`

// syncScript is the shell script executed by the init container to sync Claude configuration
// It reads from the Settings Secret and generates ~/.claude.json, ~/.claude/settings.json, ~/.claude/.credentials.json, and ~/.claude/CLAUDE.md
// It also clones marketplaces to ~/.claude/plugins/marketplaces/ and installs enabled plugins
// It also copies notification subscriptions to the notifications directory
const syncScript = `
set -e

echo "[SYNC] Starting Claude configuration sync"

# Build sync command with required arguments
SYNC_ARGS="--settings-file /settings-config/settings.json --output-dir /home/agentapi"

# Add credentials file if it exists
if [ -f "/credentials-config/credentials.json" ]; then
    echo "[SYNC] Found credentials file, including in sync"
    SYNC_ARGS="$SYNC_ARGS --credentials-file /credentials-config/credentials.json"
fi

# Add CLAUDE.md file (default path in Docker image)
SYNC_ARGS="$SYNC_ARGS --claude-md-file /tmp/config/CLAUDE.md"

# Add notification subscriptions if source directory exists and has files
if [ -d "/notification-subscriptions-source" ] && [ "$(ls -A /notification-subscriptions-source 2>/dev/null)" ]; then
    echo "[SYNC] Found notification subscriptions, including in sync"
    SYNC_ARGS="$SYNC_ARGS --notification-subscriptions /notification-subscriptions-source --notifications-dir /notifications"
fi

# Enable marketplace registration
SYNC_ARGS="$SYNC_ARGS --register-marketplaces"

# Run sync command to generate Claude configuration
agentapi-proxy helpers sync $SYNC_ARGS

echo "[SYNC] Claude configuration sync complete"
`

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

	// Build init containers
	var initContainers []corev1.Container

	// Add clone-repo init container if repository info is provided
	if cloneRepoInitContainer := m.buildCloneRepoInitContainer(session, req); cloneRepoInitContainer != nil {
		initContainers = append(initContainers, *cloneRepoInitContainer)
	}

	// Add settings merge init container
	// This merges base, team, and user settings into a single configuration
	initContainers = append(initContainers, m.buildSettingsMergeInitContainer(session))
	log.Printf("[K8S_SESSION] Added settings merge init container for session %s", session.id)

	// Add sync init container
	// This generates ~/.claude.json, ~/.claude/settings.json, and clones marketplace repositories
	initContainers = append(initContainers, m.buildSyncInitContainer(session, req))
	log.Printf("[K8S_SESSION] Added sync init container for session %s", session.id)

	// Add MCP servers setup init container if enabled
	if m.k8sConfig.MCPServersEnabled {
		initContainers = append(initContainers, m.buildMCPSetupInitContainer(session, req))
		log.Printf("[K8S_SESSION] Added MCP setup init container for session %s", session.id)
	}

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
		VolumeMounts: m.buildMainContainerVolumeMounts(),
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
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "agentapi-proxy-session",
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    int64Ptr(999),
						RunAsUser:  int64Ptr(999),
						RunAsGroup: int64Ptr(999),
					},
					InitContainers: initContainers,
					Containers:     containers,
					Volumes:        volumes,
					NodeSelector:   m.k8sConfig.NodeSelector,
					Tolerations:    tolerations,
				},
			},
		},
	}

	_, err := m.client.AppsV1().Deployments(m.namespace).Create(ctx, deployment, metav1.CreateOptions{})
	return err
}

// cloneRepoScript is the shell script executed by the init container to clone the repository
// The repository is cloned to /home/agentapi/workdir/repo
const cloneRepoScript = `
set -e

CLONE_DIR="/home/agentapi/workdir/repo"

# Skip if no repository is specified
if [ -z "$AGENTAPI_REPO_FULLNAME" ]; then
    echo "No repository specified, skipping clone"
    exit 0
fi

echo "Setting up repository clone for: $AGENTAPI_REPO_FULLNAME"

# Write PEM to emptyDir if provided via environment variable
# This file will be shared with main container via emptyDir volume
if [ -n "$GITHUB_APP_PEM" ]; then
    echo "$GITHUB_APP_PEM" > /github-app/app.pem
    chmod 600 /github-app/app.pem
    export GITHUB_APP_PEM_PATH=/github-app/app.pem
    echo "GitHub App PEM file created at /github-app/app.pem"
fi

# Setup GitHub authentication
# Always run setup-gh to configure git credential helper properly
# This is required for GitHub Enterprise Server (GHES) even when GITHUB_TOKEN is set
echo "Setting up GitHub authentication..."
agentapi-proxy helpers setup-gh --repo-fullname "$AGENTAPI_REPO_FULLNAME"

# Clone or update repository
if [ -d "$CLONE_DIR/.git" ]; then
    echo "Repository already exists, pulling latest changes..."
    cd "$CLONE_DIR"
    git pull || echo "Warning: git pull failed, continuing with existing repository"
else
    echo "Cloning repository to $CLONE_DIR..."
    gh repo clone "$AGENTAPI_REPO_FULLNAME" "$CLONE_DIR"
fi

echo "Repository setup completed"
`

// buildCloneRepoInitContainer builds the init container for repository cloning
func (m *KubernetesSessionManager) buildCloneRepoInitContainer(session *KubernetesSession, req *entities.RunServerRequest) *corev1.Container {
	// Skip if no repository info is provided
	if req.RepoInfo == nil || req.RepoInfo.FullName == "" {
		return nil
	}

	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	// Build environment variables
	env := []corev1.EnvVar{
		{Name: "AGENTAPI_REPO_FULLNAME", Value: req.RepoInfo.FullName},
		{Name: "HOME", Value: "/home/agentapi"},
	}

	// Build envFrom for GitHub secrets
	// When params.github_token is provided, use session-specific Secret instead of GitHubSecretName
	// to avoid exposing GITHUB_APP_PEM and other auth credentials
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

	return &corev1.Container{
		Name:            "clone-repo",
		Image:           initImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{cloneRepoScript},
		Env:             env,
		EnvFrom:         envFrom,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workdir",
				MountPath: "/home/agentapi/workdir",
			},
			// Mount emptyDir for GitHub App PEM file
			{
				Name:      "github-app",
				MountPath: "/github-app",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
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
			// Mount .claude directory (shared with main container)
			{
				Name:      "claude-config",
				MountPath: "/home/agentapi/.claude",
				SubPath:   ".claude",
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

// deleteInitialMessageSecret deletes the initial message Secret for a session
func (m *KubernetesSessionManager) deleteInitialMessageSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-initial-message", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete initial message secret: %w", err)
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
		// Base Claude configuration Secret (contains claude.json, settings.json with GITHUB_TOKEN)
		{
			Name: "claude-config-base",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: m.k8sConfig.ClaudeConfigBaseSecret,
					Optional:   boolPtr(true),
				},
			},
		},
		// User-specific Claude configuration ConfigMap
		{
			Name: "claude-config-user",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: userConfigMapName,
					},
					Optional: boolPtr(true), // User config is optional
				},
			},
		},
		// EmptyDir for merged Claude configuration
		{
			Name: "claude-config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		// Credentials volume (Secret for user-scoped, EmptyDir for team-scoped)
		// This Secret is managed by the credentials-sync sidecar for user-scoped sessions
		credentialsVolume,
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

	// EmptyDir for notifications (writable, populated by init container from Secret)
	volumes = append(volumes, corev1.Volume{
		Name: "notifications",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	// EmptyDir for GitHub App PEM file (shared between init container and main container)
	volumes = append(volumes, corev1.Volume{
		Name: "github-app",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
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

	// Add MCP server configuration volumes if enabled
	if m.k8sConfig.MCPServersEnabled {
		volumes = append(volumes, m.buildMCPVolumes(session)...)
	}

	// Add sync configuration volumes (settings secret)
	volumes = append(volumes, m.buildSyncVolumes(session)...)

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

	// Add mcp-config EmptyDir for merged output
	volumes = append(volumes, corev1.Volume{
		Name: "mcp-config",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumes
}

// buildSyncVolumes builds the volumes for sync configuration
// Uses projected volume to merge base, team, and user settings
func (m *KubernetesSessionManager) buildSyncVolumes(session *KubernetesSession) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Build projected volume sources for settings-config-source
	var projectedSources []corev1.VolumeProjection

	// Add base settings Secret (if configured)
	if m.k8sConfig.SettingsBaseSecret != "" {
		projectedSources = append(projectedSources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.SettingsBaseSecret,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  "settings.json",
						Path: "base/settings.json",
					},
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add team settings Secrets
	if session.Request() != nil {
		for i, team := range session.Request().Teams {
			secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(team))
			projectedSources = append(projectedSources, corev1.VolumeProjection{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "settings.json",
							Path: fmt.Sprintf("team/%d/settings.json", i),
						},
					},
					Optional: boolPtr(true),
				},
			})
		}
	}

	// Add user settings Secret
	if session.Request() != nil && session.Request().UserID != "" {
		userSecretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(session.Request().UserID))
		projectedSources = append(projectedSources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: userSecretName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  "settings.json",
						Path: "user/settings.json",
					},
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add settings-config-source as projected volume
	if len(projectedSources) > 0 {
		volumes = append(volumes, corev1.Volume{
			Name: "settings-config-source",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: projectedSources,
				},
			},
		})
	} else {
		// Use an EmptyDir if no secrets configured
		volumes = append(volumes, corev1.Volume{
			Name: "settings-config-source",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Add settings-config EmptyDir for merged output
	volumes = append(volumes, corev1.Volume{
		Name: "settings-config",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumes
}

// createService creates a Service for the session
func (m *KubernetesSessionManager) createService(ctx context.Context, session *KubernetesSession) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.ServiceName(),
			Namespace: m.namespace,
			Labels:    m.buildLabels(session),
			Annotations: map[string]string{
				"agentapi.proxy/created-at": session.startedAt.Format(time.RFC3339),
				"agentapi.proxy/team-id":    session.Request().TeamID, // Store original team_id (unsanitized)
			},
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
		// GitHub App PEM path (file is created by clone-repo init container in emptyDir)
		{Name: "GITHUB_APP_PEM_PATH", Value: "/github-app/app.pem"},
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

	// Team ID
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "TEAM_ID", Value: req.TeamID})
	} else {
		envVars = append(envVars, corev1.EnvVar{Name: "TEAM_ID", Value: ""})
	}

	// Schedule ID (from tags)
	scheduleID := ""
	if req.Tags != nil {
		if val, ok := req.Tags["schedule_id"]; ok {
			scheduleID = val
		}
	}
	envVars = append(envVars, corev1.EnvVar{Name: "SCHEDULE_ID", Value: scheduleID})

	// Webhook ID (from tags)
	webhookID := ""
	if req.Tags != nil {
		if val, ok := req.Tags["webhook_id"]; ok {
			webhookID = val
		}
	}
	envVars = append(envVars, corev1.EnvVar{Name: "WEBHOOK_ID", Value: webhookID})

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
func (m *KubernetesSessionManager) SetSettingsRepository(repo repositories.SettingsRepository) {
	m.settingsRepo = repo
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

// buildMCPSetupInitContainer builds the init container for MCP server configuration setup
func (m *KubernetesSessionManager) buildMCPSetupInitContainer(session *KubernetesSession, req *entities.RunServerRequest) corev1.Container {
	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	// Build envFrom for environment variables needed by MCP configs
	// When params.github_token is provided, use session-specific Secret instead of GitHubSecretName
	// to avoid exposing GITHUB_APP_PEM and other auth credentials
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

	// Add team-based credentials Secrets (for environment variable expansion)
	for _, team := range req.Teams {
		secretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(team))
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add user-specific credentials Secret
	if req.UserID != "" {
		userSecretName := fmt.Sprintf("agent-env-%s", sanitizeSecretName(req.UserID))
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: userSecretName,
				},
				Optional: boolPtr(true),
			},
		})
	}

	return corev1.Container{
		Name:            "setup-mcp",
		Image:           initImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{setupMCPServersScript},
		EnvFrom:         envFrom,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "mcp-config-source",
				MountPath: "/mcp-config-source",
				ReadOnly:  true,
			},
			{
				Name:      "mcp-config",
				MountPath: "/mcp-config",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// buildSyncInitContainer builds the init container for syncing Claude configuration
// This replaces both buildClaudeSetupInitContainer and buildMarketplaceSetupInitContainer
func (m *KubernetesSessionManager) buildSyncInitContainer(session *KubernetesSession, req *entities.RunServerRequest) corev1.Container {
	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	// Build envFrom for environment variables needed by sync (GITHUB_TOKEN for marketplace cloning)
	var envFrom []corev1.EnvFromSource

	if req.GithubToken != "" {
		// When params.github_token is provided:
		// - Mount GitHubConfigSecretName for GITHUB_API/GITHUB_URL settings
		// - Mount session-specific Secret for GITHUB_TOKEN
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
	} else if m.k8sConfig.GitHubSecretName != "" {
		// When params.github_token is NOT provided:
		// - Mount GitHubSecretName for full GitHub App authentication
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.GitHubSecretName,
				},
				Optional: boolPtr(true),
			},
		})

		// Mount GitHub config Secret if available
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

	return corev1.Container{
		Name:            "sync-config",
		Image:           initImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{syncScript},
		EnvFrom:         envFrom,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "settings-config",
				MountPath: "/settings-config",
				ReadOnly:  true,
			},
			{
				// Mount claude-config EmptyDir to /home/agentapi
				// Sync generates .claude.json and .claude/ directory here
				Name:      "claude-config",
				MountPath: "/home/agentapi",
			},
			{
				// Mount claude-credentials Secret for copying credentials.json
				Name:      "claude-credentials",
				MountPath: "/credentials-config",
				ReadOnly:  true,
			},
			{
				// Mount notification subscriptions Secret (source for copying)
				Name:      "notification-subscriptions-source",
				MountPath: "/notification-subscriptions-source",
				ReadOnly:  true,
			},
			{
				// Mount notifications EmptyDir (destination for copying)
				Name:      "notifications",
				MountPath: "/notifications",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// buildSettingsMergeInitContainer builds the init container for merging settings configurations
// It merges base, team, and user settings into a single configuration
func (m *KubernetesSessionManager) buildSettingsMergeInitContainer(session *KubernetesSession) corev1.Container {
	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	return corev1.Container{
		Name:            "merge-settings",
		Image:           initImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{mergeSettingsScript},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "settings-config-source",
				MountPath: "/settings-config-source",
				ReadOnly:  true,
			},
			{
				Name:      "settings-config",
				MountPath: "/settings-config",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// buildMainContainerVolumeMounts builds the volume mounts for the main container
func (m *KubernetesSessionManager) buildMainContainerVolumeMounts() []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "workdir",
			MountPath: "/home/agentapi/workdir",
		},
		{
			Name:      "claude-config",
			MountPath: "/home/agentapi/.claude.json",
			SubPath:   ".claude.json",
		},
		{
			Name:      "claude-config",
			MountPath: "/home/agentapi/.claude",
			SubPath:   ".claude",
		},
		// Mount notifications directory (EmptyDir, writable)
		{
			Name:      "notifications",
			MountPath: "/home/agentapi/notifications",
		},
		// Mount emptyDir for GitHub App PEM file (shared with clone-repo init container)
		{
			Name:      "github-app",
			MountPath: "/github-app",
			ReadOnly:  true,
		},
	}

	// Add MCP config volume mount if enabled
	if m.k8sConfig.MCPServersEnabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "mcp-config",
			MountPath: "/mcp-config",
			ReadOnly:  true,
		})
	}

	return volumeMounts
}

// buildClaudeStartCommand builds the Claude start command with optional MCP config
// Uses resume fallback pattern: "claude -c [args] || claude [args]"
// This attempts to resume an existing session first, falling back to a new session if not available
func (m *KubernetesSessionManager) buildClaudeStartCommand() string {
	// Base command that uses CLAUDE_ARGS if set
	baseCmd := `
# Start agentapi with Claude
CLAUDE_ARGS_FULL=""

# Add --mcp-config if MCP config file exists
if [ -f /mcp-config/merged.json ]; then
    CLAUDE_ARGS_FULL="--mcp-config /mcp-config/merged.json"
    echo "[STARTUP] Using MCP config: /mcp-config/merged.json"
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
	)
	// Set restored values
	session.SetStartedAt(createdAt)
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
	)
	// Set restored values
	session.SetStartedAt(createdAt)
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
      - key: session_id
        value: ${env:SESSION_ID}
        action: upsert
      - key: user_id
        value: ${env:USER_ID}
        action: upsert
      - key: team_id
        value: ${env:TEAM_ID}
        action: upsert
      - key: schedule_id
        value: ${env:SCHEDULE_ID}
        action: upsert
      - key: webhook_id
        value: ${env:WEBHOOK_ID}
        action: upsert

exporters:
  prometheus:
    endpoint: "0.0.0.0:%d"
    resource_to_telemetry_conversion:
      enabled: true

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [resource]
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
		image = "otel/opentelemetry-collector-contrib:0.95.0"
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
