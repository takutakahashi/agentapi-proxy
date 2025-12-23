package proxy

import (
	"context"
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
	sessions     map[string]*kubernetesSession
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
	restConfig := ctrl.GetConfigOrDie()

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

	return &KubernetesSessionManager{
		config:    cfg,
		k8sConfig: k8sConfig,
		client:    client,
		verbose:   verbose,
		logger:    lgr,
		sessions:  make(map[string]*kubernetesSession),
		namespace: namespace,
	}, nil
}

// CreateSession creates a new session with a Kubernetes Deployment
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error) {
	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Generate resource names
	deploymentName := fmt.Sprintf("agentapi-session-%s", id)
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)

	// Create kubernetesSession
	session := &kubernetesSession{
		id:             id,
		request:        req,
		deploymentName: deploymentName,
		serviceName:    serviceName,
		pvcName:        pvcName,
		servicePort:    m.k8sConfig.BasePort,
		namespace:      m.namespace,
		startedAt:      time.Now(),
		status:         "creating",
		cancelFunc:     cancel,
	}

	// Store session
	m.mutex.Lock()
	m.sessions[id] = session
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Creating session %s in namespace %s", id, m.namespace)

	// Ensure Base ConfigMap exists (create if not present)
	if err := m.ensureBaseConfigMap(ctx); err != nil {
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to ensure base ConfigMap: %w", err)
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
func (m *KubernetesSessionManager) GetSession(id string) Session {
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
func (m *KubernetesSessionManager) ListSessions(filter SessionFilter) []Session {
	// Get services from Kubernetes API
	services, err := m.client.CoreV1().Services(m.namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session",
		})
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list services: %v", err)
		return []Session{}
	}

	var result []Session
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

		// Get or restore session
		session := m.getOrRestoreSession(svc)
		if session == nil {
			continue
		}

		// Apply Status filter
		if filter.Status != "" && session.Status() != filter.Status {
			continue
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

// getOrRestoreSession gets a session from memory or restores it from Service
func (m *KubernetesSessionManager) getOrRestoreSession(svc *corev1.Service) *kubernetesSession {
	sessionID := svc.Labels["agentapi.proxy/session-id"]

	// Check if session exists in memory
	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()

	if exists {
		return session
	}

	// Restore session from Service
	return m.restoreSessionFromService(svc)
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
	if session.cancelFunc != nil {
		session.cancelFunc()
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
	m.sessions = make(map[string]*kubernetesSession)
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Shutting down, preserving %d session(s) in Kubernetes for recovery", sessionCount)
	return nil
}

// createPVC creates a PersistentVolumeClaim for the session
func (m *KubernetesSessionManager) createPVC(ctx context.Context, session *kubernetesSession) error {
	storageSize := resource.MustParse(m.k8sConfig.PVCStorageSize)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.pvcName,
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

// ensureBaseConfigMap ensures the base ConfigMap exists, creating it if necessary
func (m *KubernetesSessionManager) ensureBaseConfigMap(ctx context.Context) error {
	configMapName := m.k8sConfig.ClaudeConfigBaseConfigMap
	if configMapName == "" {
		configMapName = "claude-config-base"
	}

	// Check if ConfigMap already exists
	_, err := m.client.CoreV1().ConfigMaps(m.namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err == nil {
		// ConfigMap already exists
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check ConfigMap existence: %w", err)
	}

	// Create the base ConfigMap with default settings
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "agentapi-proxy",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/component":  "claude-config",
			},
		},
		Data: map[string]string{
			"claude.json":   defaultClaudeJSON,
			"settings.json": defaultSettingsJSON,
		},
	}

	_, err = m.client.CoreV1().ConfigMaps(m.namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		// If another process created it concurrently, that's fine
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create base ConfigMap: %w", err)
	}

	log.Printf("[K8S_SESSION] Created base ConfigMap %s in namespace %s", configMapName, m.namespace)
	return nil
}

// setupClaudeScript is the shell script executed by the init container to set up Claude configuration
// Uses Node.js for JSON merging since it's available in the agentapi-proxy image
const setupClaudeScript = `
set -e

# Create directory structure in EmptyDir
mkdir -p /claude-config/.claude

# Merge claude.json: base + user (user takes precedence)
# Using Node.js for JSON merging
if [ -f /claude-config-base/claude.json ]; then
    cp /claude-config-base/claude.json /tmp/base.json
else
    echo '{}' > /tmp/base.json
fi

if [ -f /claude-config-user/claude.json ]; then
    node -e "
const base = JSON.parse(require('fs').readFileSync('/tmp/base.json', 'utf8'));
const user = JSON.parse(require('fs').readFileSync('/claude-config-user/claude.json', 'utf8'));
const merged = { ...base, ...user };
require('fs').writeFileSync('/claude-config/.claude.json', JSON.stringify(merged, null, 2));
"
else
    cp /tmp/base.json /claude-config/.claude.json
fi

# Copy settings.json from base config
if [ -f /claude-config-base/settings.json ]; then
    cp /claude-config-base/settings.json /claude-config/.claude/settings.json
fi

# Copy CLAUDE.md from embedded location (from Docker image)
if [ -f /tmp/config/CLAUDE.md ]; then
    cp /tmp/config/CLAUDE.md /claude-config/.claude/CLAUDE.md
    chmod 644 /claude-config/.claude/CLAUDE.md
    echo "CLAUDE.md copied from Docker image"
fi

# Copy credentials.json from Secret if exists
if [ -f /claude-credentials/credentials.json ]; then
    cp /claude-credentials/credentials.json /claude-config/.claude/.credentials.json
    chmod 600 /claude-config/.claude/.credentials.json
    echo "Credentials file copied"
fi

# Copy notification subscriptions from Secret to writable EmptyDir
# Use -L to follow symlinks (Secret mounts use symlinks)
# Use find to avoid glob expansion issues when directory is empty
if [ -d /notification-subscriptions-source ]; then
    file_count=$(find /notification-subscriptions-source -maxdepth 1 -type f 2>/dev/null | wc -l)
    if [ "$file_count" -gt 0 ]; then
        cp -rL /notification-subscriptions-source/* /notifications/
        echo "Notification subscriptions copied"
    fi
fi

# Set permissions (running as user 999)
chmod 644 /claude-config/.claude.json
chmod -R 755 /claude-config/.claude
chmod 644 /claude-config/.claude/settings.json 2>/dev/null || true

echo "Claude configuration setup complete"
`

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

// createDeployment creates a Deployment for the session
func (m *KubernetesSessionManager) createDeployment(ctx context.Context, session *kubernetesSession, req *RunServerRequest) error {
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

	// Add Claude configuration setup init container
	initContainers = append(initContainers, m.buildClaudeSetupInitContainer(session))

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

	// Build envFrom for GitHub secret (used for GitHub authentication in session)
	var envFrom []corev1.EnvFromSource
	if m.k8sConfig.GitHubSecretName != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.GitHubSecretName,
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add team-based credentials Secrets (agent-credentials-{org}-{team})
	for _, team := range req.Teams {
		secretName := fmt.Sprintf("agent-credentials-%s", sanitizeSecretName(team))
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

	// Add user-specific credentials Secret (agent-credentials-{user-id})
	// This is added last so user-specific values override team values
	if req.UserID != "" {
		userSecretName := fmt.Sprintf("agent-credentials-%s", sanitizeSecretName(req.UserID))
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
			Name:      session.deploymentName,
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
					ServiceAccountName: "agentapi-proxy",
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

# Setup GitHub authentication (skip if GITHUB_TOKEN is already set, as gh CLI uses it automatically)
if [ -z "$GITHUB_TOKEN" ]; then
    echo "Setting up GitHub authentication..."
    agentapi-proxy helpers setup-gh --repo-fullname "$AGENTAPI_REPO_FULLNAME"
else
    echo "GITHUB_TOKEN is set, skipping setup-gh (gh CLI uses it automatically)"
fi

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
func (m *KubernetesSessionManager) buildCloneRepoInitContainer(session *kubernetesSession, req *RunServerRequest) *corev1.Container {
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

	// Build envFrom for GitHub secret
	var envFrom []corev1.EnvFromSource
	if m.k8sConfig.GitHubSecretName != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.GitHubSecretName,
				},
				Optional: boolPtr(true),
			},
		})
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

    # Check if secret exists
    if kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        # Patch existing secret - use add operation for upsert behavior
        RESULT=$(kubectl patch secret "$SECRET_NAME" -n "$NAMESPACE" \
            --type='merge' \
            -p="{\"data\":{\"credentials.json\":\"$ENCODED\"}}" 2>&1)

        if [ $? -eq 0 ]; then
            log "Successfully synced credentials to existing Secret"
            LAST_HASH="$CURRENT_HASH"
        else
            log "ERROR: Failed to patch Secret: $RESULT"
        fi
    else
        # Create new secret with labels
        log "Secret does not exist, creating..."
        RESULT=$(kubectl create secret generic "$SECRET_NAME" -n "$NAMESPACE" \
            --from-file=credentials.json="$CREDENTIALS_PATH" 2>&1)

        if [ $? -eq 0 ]; then
            log "Successfully created Secret"
            # Add labels to the secret
            kubectl label secret "$SECRET_NAME" -n "$NAMESPACE" \
                app.kubernetes.io/name=agentapi-agent-credentials \
                app.kubernetes.io/managed-by=agentapi-proxy \
                --overwrite >/dev/null 2>&1 || true
            LAST_HASH="$CURRENT_HASH"
        else
            log "ERROR: Failed to create Secret: $RESULT"
        fi
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
func (m *KubernetesSessionManager) buildCredentialsSyncSidecar(session *kubernetesSession) *corev1.Container {
	// Use bitnami/kubectl image which contains kubectl
	sidecarImage := credentialsSyncSidecarImage

	// Secret name is per-user, not per-session
	// Format: agentapi-agent-credentials-{userID}
	credentialsSecretName := fmt.Sprintf("agentapi-agent-credentials-%s", sanitizeLabelValue(session.request.UserID))

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

# Check if messages already exist (Pod recreated case)
MESSAGE_COUNT=$(curl -sf "${AGENTAPI_URL}/messages" 2>/dev/null | jq 'length' 2>/dev/null || echo "0")
if [ "$MESSAGE_COUNT" -gt 0 ]; then
    echo "[INITIAL-MSG] Messages already exist (count: ${MESSAGE_COUNT}), skipping initial message"
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

# Double-check message count before sending (race condition prevention)
MESSAGE_COUNT=$(curl -sf "${AGENTAPI_URL}/messages" 2>/dev/null | jq 'length' 2>/dev/null || echo "0")
if [ "$MESSAGE_COUNT" -gt 0 ]; then
    echo "[INITIAL-MSG] Messages appeared during wait (count: ${MESSAGE_COUNT}), skipping"
    touch "$SENT_FLAG"
    exec sleep infinity
fi

# Read and send message
echo "[INITIAL-MSG] Sending initial message..."
MESSAGE_CONTENT=$(cat "$MESSAGE_FILE")

# Build JSON payload with proper escaping using jq
PAYLOAD=$(printf '%s' "$MESSAGE_CONTENT" | jq -Rs '{content: ., type: "user"}')

RESPONSE=$(curl -sf -w "\n%{http_code}" -X POST "${AGENTAPI_URL}/message" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" 2>&1) || true

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
    echo "[INITIAL-MSG] Initial message sent successfully"
    touch "$SENT_FLAG"
else
    echo "[INITIAL-MSG] ERROR: Failed to send initial message (HTTP ${HTTP_CODE})"
    echo "[INITIAL-MSG] Response: ${BODY}"
fi

# Keep container running (prevents restart loop)
exec sleep infinity
`

// buildInitialMessageSenderSidecar builds the sidecar container for sending initial messages
func (m *KubernetesSessionManager) buildInitialMessageSenderSidecar(session *kubernetesSession) *corev1.Container {
	// Only create sidecar if there's an initial message
	if session.request.InitialMessage == "" {
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
	session *kubernetesSession,
	message string,
) error {
	secretName := fmt.Sprintf("%s-initial-message", session.serviceName)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.request.UserID),
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
func (m *KubernetesSessionManager) deleteInitialMessageSecret(ctx context.Context, session *kubernetesSession) error {
	secretName := fmt.Sprintf("%s-initial-message", session.serviceName)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete initial message secret: %w", err)
	}
	return nil
}

// buildClaudeSetupInitContainer builds the init container for Claude configuration setup
func (m *KubernetesSessionManager) buildClaudeSetupInitContainer(session *kubernetesSession) corev1.Container {
	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "claude-config-base",
			MountPath: "/claude-config-base",
			ReadOnly:  true,
		},
		{
			Name:      "claude-config-user",
			MountPath: "/claude-config-user",
			ReadOnly:  true,
		},
		{
			Name:      "claude-config",
			MountPath: "/claude-config",
		},
		// Credentials from user-level Secret (agentapi-agent-credentials-{userID})
		// This Secret is optional and managed by credentials-sync sidecar
		{
			Name:      "claude-credentials",
			MountPath: "/claude-credentials",
			ReadOnly:  true,
		},
		// Notification subscriptions source (Secret, read-only)
		{
			Name:      "notification-subscriptions-source",
			MountPath: "/notification-subscriptions-source",
			ReadOnly:  true,
		},
		// Notifications directory (EmptyDir, writable)
		{
			Name:      "notifications",
			MountPath: "/notifications",
		},
	}

	return corev1.Container{
		Name:            "setup-claude",
		Image:           initImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c"},
		Args:            []string{setupClaudeScript},
		VolumeMounts:    volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
}

// buildVolumes builds the volume configuration for the session pod
func (m *KubernetesSessionManager) buildVolumes(session *kubernetesSession, userConfigMapName string) []corev1.Volume {
	// Credentials Secret name follows the pattern: agentapi-agent-credentials-{userID}
	credentialsSecretName := fmt.Sprintf("agentapi-agent-credentials-%s", sanitizeLabelValue(session.request.UserID))

	// Build workdir volume - use PVC if enabled, otherwise EmptyDir
	var workdirVolume corev1.Volume
	if m.isPVCEnabled() {
		workdirVolume = corev1.Volume{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: session.pvcName,
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

	volumes := []corev1.Volume{
		// Workdir volume (PVC or EmptyDir based on configuration)
		workdirVolume,
		// Base Claude configuration ConfigMap
		{
			Name: "claude-config-base",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: m.k8sConfig.ClaudeConfigBaseConfigMap,
					},
					Optional: boolPtr(true),
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
		// User credentials Secret (per-user, not per-session)
		// This Secret is managed by the credentials-sync sidecar
		{
			Name: "claude-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: credentialsSecretName,
					Optional:   boolPtr(true), // Optional - user may not have logged in yet
				},
			},
		},
	}

	// Add notification subscription Secret volume (source for init container)
	// Secret name follows the pattern: notification-subscriptions-{userID}
	notificationSecretName := fmt.Sprintf("notification-subscriptions-%s", sanitizeLabelValue(session.request.UserID))
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
	if session.request != nil && session.request.InitialMessage != "" {
		initialMsgSecretName := fmt.Sprintf("%s-initial-message", session.serviceName)
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

	return volumes
}

// buildMCPVolumes builds the volumes for MCP server configuration
func (m *KubernetesSessionManager) buildMCPVolumes(session *kubernetesSession) []corev1.Volume {
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
	if m.k8sConfig.MCPServersTeamSecretPrefix != "" && session.request != nil {
		for i, team := range session.request.Teams {
			secretName := fmt.Sprintf("%s-%s", m.k8sConfig.MCPServersTeamSecretPrefix, sanitizeSecretName(team))
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
	if m.k8sConfig.MCPServersUserSecretPrefix != "" && session.request != nil && session.request.UserID != "" {
		userSecretName := fmt.Sprintf("%s-%s", m.k8sConfig.MCPServersUserSecretPrefix, sanitizeSecretName(session.request.UserID))
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

// createService creates a Service for the session
func (m *KubernetesSessionManager) createService(ctx context.Context, session *kubernetesSession) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.serviceName,
			Namespace: m.namespace,
			Labels:    m.buildLabels(session),
			Annotations: map[string]string{
				"agentapi.proxy/created-at": session.startedAt.Format(time.RFC3339),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"agentapi.proxy/session-id": session.id,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       int32(session.servicePort),
					TargetPort: intstr.FromInt(m.k8sConfig.BasePort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := m.client.CoreV1().Services(m.namespace).Create(ctx, service, metav1.CreateOptions{})
	return err
}

// watchSession monitors the session deployment status
func (m *KubernetesSessionManager) watchSession(ctx context.Context, session *kubernetesSession) {
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
			session.setStatus("timeout")
			return

		case <-ticker.C:
			deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
				context.Background(), session.deploymentName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					log.Printf("[K8S_SESSION] Deployment %s not found, session may have been deleted", session.deploymentName)
					return
				}
				log.Printf("[K8S_SESSION] Error getting deployment: %v", err)
				continue
			}

			// Check deployment status
			if deployment.Status.ReadyReplicas > 0 {
				session.setStatus("active")
				log.Printf("[K8S_SESSION] Session %s is now active", session.id)

				// Note: Initial message is now sent by the initial-message-sender sidecar
				// within the Pod, not by the proxy

				// Continue watching for changes
				m.watchDeploymentStatus(ctx, session)
				return
			}

			session.setStatus("starting")
		}
	}
}

// watchDeploymentStatus continuously watches the deployment status after it becomes ready
func (m *KubernetesSessionManager) watchDeploymentStatus(ctx context.Context, session *kubernetesSession) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
				context.Background(), session.deploymentName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					session.setStatus("stopped")
					return
				}
				continue
			}

			if deployment.Status.ReadyReplicas == 0 {
				session.setStatus("unhealthy")
			} else {
				session.setStatus("active")
			}
		}
	}
}

// deleteSessionResources deletes all Kubernetes resources for a session
func (m *KubernetesSessionManager) deleteSessionResources(ctx context.Context, session *kubernetesSession) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	var errs []string

	// Delete Service
	err := m.client.CoreV1().Services(m.namespace).Delete(ctx, session.serviceName, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("service: %v", err))
	}

	// Delete Deployment
	err = m.client.AppsV1().Deployments(m.namespace).Delete(ctx, session.deploymentName, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("deployment: %v", err))
	}

	// Delete PVC if enabled
	if m.isPVCEnabled() {
		err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.pvcName, deleteOptions)
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Sprintf("pvc: %v", err))
		}
	}

	// Delete initial message Secret
	if err := m.deleteInitialMessageSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("initial-message-secret: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete resources: %s", strings.Join(errs, ", "))
	}

	return nil
}

// deleteDeployment deletes the deployment for a session
func (m *KubernetesSessionManager) deleteDeployment(ctx context.Context, session *kubernetesSession) error {
	return m.client.AppsV1().Deployments(m.namespace).Delete(ctx, session.deploymentName, metav1.DeleteOptions{})
}

// deletePVC deletes the PVC for a session
func (m *KubernetesSessionManager) deletePVC(ctx context.Context, session *kubernetesSession) error {
	return m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.pvcName, metav1.DeleteOptions{})
}

// cleanupSession removes a session from the internal map
func (m *KubernetesSessionManager) cleanupSession(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.sessions, id)
}

// buildLabels creates standard labels for Kubernetes resources
func (m *KubernetesSessionManager) buildLabels(session *kubernetesSession) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/instance":   session.id,
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    session.id,
		"agentapi.proxy/user-id":       sanitizeLabelValue(session.request.UserID),
	}

	// Add tags as labels (sanitized for Kubernetes)
	for k, v := range session.request.Tags {
		labelKey := fmt.Sprintf("agentapi.proxy/tag-%s", sanitizeLabelKey(k))
		labels[labelKey] = sanitizeLabelValue(v)
	}

	return labels
}

// buildEnvVars creates environment variables for the session pod
func (m *KubernetesSessionManager) buildEnvVars(session *kubernetesSession, req *RunServerRequest) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "AGENTAPI_PORT", Value: fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		{Name: "AGENTAPI_SESSION_ID", Value: session.id},
		{Name: "AGENTAPI_USER_ID", Value: req.UserID},
		{Name: "HOME", Value: "/home/agentapi"},
		// GitHub App PEM path (file is created by clone-repo init container in emptyDir)
		{Name: "GITHUB_APP_PEM_PATH", Value: "/github-app/app.pem"},
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

	// Note: Bedrock settings are now loaded via envFrom from agent-credentials-{name} Secret
	// which is synced by CredentialsSecretSyncer when settings are updated via API

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

// sanitizeLabelValue sanitizes a string to be used as a Kubernetes label value
func sanitizeLabelValue(s string) string {
	// Label values must be 63 characters or less
	// Must start and end with alphanumeric character (or be empty)
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

// sanitizeSecretName sanitizes a string to be used as a Kubernetes Secret name
// Secret names must be lowercase, alphanumeric, and may contain dashes
// Example: "myorg/backend-team" -> "myorg-backend-team"
func sanitizeSecretName(s string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(s)
	// Replace non-alphanumeric characters (except dash) with dash
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Remove consecutive dashes
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	// Secret names must be 253 characters or less
	if len(sanitized) > 253 {
		sanitized = sanitized[:253]
	}
	// Trim dashes from start and end
	sanitized = strings.Trim(sanitized, "-")
	return sanitized
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
func (m *KubernetesSessionManager) buildMCPSetupInitContainer(session *kubernetesSession, req *RunServerRequest) corev1.Container {
	// Use the main container image if InitContainerImage is not specified
	initImage := m.k8sConfig.InitContainerImage
	if initImage == "" {
		initImage = m.k8sConfig.Image
	}

	// Build envFrom for environment variables needed by MCP configs
	var envFrom []corev1.EnvFromSource

	// Add GitHub secret environment (may contain tokens used in MCP configs)
	if m.k8sConfig.GitHubSecretName != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: m.k8sConfig.GitHubSecretName,
				},
				Optional: boolPtr(true),
			},
		})
	}

	// Add team-based credentials Secrets (for environment variable expansion)
	for _, team := range req.Teams {
		secretName := fmt.Sprintf("agent-credentials-%s", sanitizeSecretName(team))
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
		userSecretName := fmt.Sprintf("agent-credentials-%s", sanitizeSecretName(req.UserID))
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
func (m *KubernetesSessionManager) buildClaudeStartCommand() string {
	// Base command that uses CLAUDE_ARGS if set
	baseCmd := `
# Start agentapi with Claude
CLAUDE_CMD="claude"

# Add --mcp-config if MCP config file exists
if [ -f /mcp-config/merged.json ]; then
    CLAUDE_CMD="$CLAUDE_CMD --mcp-config /mcp-config/merged.json"
    echo "[STARTUP] Using MCP config: /mcp-config/merged.json"
fi

# Add CLAUDE_ARGS if set
if [ -n "$CLAUDE_ARGS" ]; then
    CLAUDE_CMD="$CLAUDE_CMD $CLAUDE_ARGS"
fi

echo "[STARTUP] Starting agentapi with: $CLAUDE_CMD"
exec agentapi server --allowed-hosts '*' --allowed-origins '*' --port $AGENTAPI_PORT -- $CLAUDE_CMD
`
	return baseCmd
}

// restoreSessionFromService restores a session from Kubernetes Service
// This is used to recover sessions after agentapi-proxy restart
func (m *KubernetesSessionManager) restoreSessionFromService(svc *corev1.Service) *kubernetesSession {
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

	session := &kubernetesSession{
		id:             sessionID,
		deploymentName: fmt.Sprintf("agentapi-session-%s", sessionID),
		serviceName:    svc.Name,
		pvcName:        fmt.Sprintf("agentapi-session-%s-pvc", sessionID),
		servicePort:    servicePort,
		namespace:      m.namespace,
		startedAt:      createdAt,
		status:         m.getSessionStatusFromDeployment(sessionID),
		cancelFunc:     cancel,
		request: &RunServerRequest{
			UserID: userID,
			Tags:   tags,
		},
	}

	// Add to memory map
	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Start watching deployment status
	go m.watchDeploymentStatus(ctx, session)

	log.Printf("[K8S_SESSION] Restored session %s from Service", sessionID)

	return session
}
