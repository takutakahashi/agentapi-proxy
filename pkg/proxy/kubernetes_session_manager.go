package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
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

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// KubernetesSessionManager manages sessions using Kubernetes Deployments
type KubernetesSessionManager struct {
	config             *config.Config
	k8sConfig          *config.KubernetesSessionConfig
	client             kubernetes.Interface
	verbose            bool
	logger             *logger.Logger
	sessions           map[string]*kubernetesSession
	mutex              sync.RWMutex
	namespace          string
	credentialProvider CredentialProvider
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

	return NewKubernetesSessionManagerWithClient(cfg, verbose, lgr, client, nil)
}

// NewKubernetesSessionManagerWithClient creates a new KubernetesSessionManager with a custom client
// This is useful for testing with a fake client
// If credProvider is nil, the default credential provider chain will be used
func NewKubernetesSessionManagerWithClient(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
	client kubernetes.Interface,
	credProvider CredentialProvider,
) (*KubernetesSessionManager, error) {
	k8sConfig := &cfg.KubernetesSession

	// Determine namespace
	namespace := k8sConfig.Namespace
	if namespace == "" {
		// Use namespace from controller-runtime config or default
		namespace = "default"
	}

	// Use default credential provider if not specified
	if credProvider == nil {
		credProvider = DefaultCredentialProvider()
	}

	log.Printf("[K8S_SESSION] Initialized KubernetesSessionManager in namespace: %s", namespace)

	return &KubernetesSessionManager{
		config:             cfg,
		k8sConfig:          k8sConfig,
		client:             client,
		verbose:            verbose,
		logger:             lgr,
		sessions:           make(map[string]*kubernetesSession),
		namespace:          namespace,
		credentialProvider: credProvider,
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

	// Load credentials (optional) - use userID to locate user-specific credentials
	var secretName string
	creds, err := m.credentialProvider.Load(req.UserID)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to load credentials for user %s: %v", req.UserID, err)
		// Continue without credentials (non-fatal)
	}
	if creds != nil {
		secretName = fmt.Sprintf("agentapi-session-%s-credentials", id)
		log.Printf("[K8S_SESSION] Loaded credentials for user %s", req.UserID)
	}

	// Create kubernetesSession
	session := &kubernetesSession{
		id:             id,
		request:        req,
		deploymentName: deploymentName,
		serviceName:    serviceName,
		pvcName:        pvcName,
		secretName:     secretName,
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

	// Create Credential Secret (if credentials exist)
	if creds != nil {
		if err := m.createCredentialSecret(ctx, session, creds); err != nil {
			m.cleanupSession(id)
			return nil, fmt.Errorf("failed to create credential Secret: %w", err)
		}
		log.Printf("[K8S_SESSION] Created credential Secret %s for session %s", secretName, id)
	}

	// Create PVC
	if err := m.createPVC(ctx, session); err != nil {
		if session.secretName != "" {
			if delErr := m.deleteCredentialSecret(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup Secret after PVC creation failure: %v", delErr)
			}
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}
	log.Printf("[K8S_SESSION] Created PVC %s for session %s", pvcName, id)

	// Create Deployment
	if err := m.createDeployment(ctx, session, req); err != nil {
		if delErr := m.deletePVC(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup PVC after deployment creation failure: %v", delErr)
		}
		if session.secretName != "" {
			if delErr := m.deleteCredentialSecret(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup Secret after deployment creation failure: %v", delErr)
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
		if delErr := m.deletePVC(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup PVC after service creation failure: %v", delErr)
		}
		if session.secretName != "" {
			if delErr := m.deleteCredentialSecret(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup Secret after service creation failure: %v", delErr)
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
func (m *KubernetesSessionManager) GetSession(id string) Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	session, exists := m.sessions[id]
	if !exists {
		return nil
	}
	return session
}

// ListSessions returns all sessions matching the filter
func (m *KubernetesSessionManager) ListSessions(filter SessionFilter) []Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var result []Session
	for _, session := range m.sessions {
		// User ID filter
		if filter.UserID != "" && session.request.UserID != filter.UserID {
			continue
		}

		// Status filter
		if filter.Status != "" && session.Status() != filter.Status {
			continue
		}

		// Tag filters
		if len(filter.Tags) > 0 {
			matchAllTags := true
			for tagKey, tagValue := range filter.Tags {
				if sessionTagValue, exists := session.request.Tags[tagKey]; !exists || sessionTagValue != tagValue {
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

// DeleteSession stops and removes a session
func (m *KubernetesSessionManager) DeleteSession(id string) error {
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	if !exists {
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
func (m *KubernetesSessionManager) Shutdown(timeout time.Duration) error {
	m.mutex.RLock()
	sessions := make([]*kubernetesSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mutex.RUnlock()

	log.Printf("[K8S_SESSION] Shutting down, terminating %d sessions...", len(sessions))

	if len(sessions) == 0 {
		return nil
	}

	// Delete all sessions in parallel
	var wg sync.WaitGroup
	for _, session := range sessions {
		wg.Add(1)
		go func(s *kubernetesSession) {
			defer wg.Done()
			if s.cancelFunc != nil {
				s.cancelFunc()
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := m.deleteSessionResources(ctx, s); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to delete session %s: %v", s.id, err)
			}
		}(session)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[K8S_SESSION] All sessions terminated")
		return nil
	case <-time.After(timeout):
		log.Printf("[K8S_SESSION] Shutdown timeout reached")
		return fmt.Errorf("shutdown timeout")
	}
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

# Copy credentials.json from Secret if exists
if [ -f /claude-credentials/credentials.json ]; then
    cp /claude-credentials/credentials.json /claude-config/.claude/.credentials.json
    chmod 600 /claude-config/.claude/.credentials.json
    echo "Credentials file copied"
fi

# Set permissions (running as user 999)
chmod 644 /claude-config/.claude.json
chmod -R 755 /claude-config/.claude
chmod 644 /claude-config/.claude/settings.json 2>/dev/null || true

echo "Claude configuration setup complete"
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

	// Determine working directory based on whether repository is specified
	workingDir := "/home/agentapi/workdir"
	if req.RepoInfo != nil && req.RepoInfo.FullName != "" {
		workingDir = "/home/agentapi/workdir/repo"
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
		Env: envVars,
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
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workdir",
				MountPath: "/home/agentapi/workdir",
			},
			// Mount claude.json from EmptyDir using SubPath
			{
				Name:      "claude-config",
				MountPath: "/home/agentapi/.claude.json",
				SubPath:   ".claude.json",
			},
			// Mount .claude directory from EmptyDir using SubPath
			{
				Name:      "claude-config",
				MountPath: "/home/agentapi/.claude",
				SubPath:   ".claude",
			},
			// Mount notification subscriptions from Secret
			{
				Name:      "notification-subscriptions",
				MountPath: "/home/agentapi/notifications",
				ReadOnly:  true,
			},
		},
		Command: []string{"sh", "-c"},
		Args: []string{
			fmt.Sprintf("agentapi server --allowed-hosts '*' --allowed-origins '*' --port %d -- claude $CLAUDE_ARGS", m.k8sConfig.BasePort),
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
					ServiceAccountName: m.k8sConfig.ServiceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    int64Ptr(999),
						RunAsUser:  int64Ptr(999),
						RunAsGroup: int64Ptr(999),
					},
					InitContainers: initContainers,
					Containers:     []corev1.Container{container},
					Volumes:        volumes,
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

# Write PEM to file if provided via environment variable
if [ -n "$GITHUB_APP_PEM" ]; then
    mkdir -p /home/agentapi/.github
    echo "$GITHUB_APP_PEM" > /home/agentapi/.github/app.pem
    chmod 600 /home/agentapi/.github/app.pem
    export GITHUB_APP_PEM_PATH=/home/agentapi/.github/app.pem
    echo "GitHub App PEM file created"
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
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  int64Ptr(999),
			RunAsGroup: int64Ptr(999),
		},
	}
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
	}

	// Add credentials volume mount if exists
	if session.secretName != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "claude-credentials",
			MountPath: "/claude-credentials",
			ReadOnly:  true,
		})
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
	volumes := []corev1.Volume{
		// Workdir PVC
		{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: session.pvcName,
				},
			},
		},
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
	}

	// Add credentials Secret volume if exists
	if session.secretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "claude-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: session.secretName,
					Optional:   boolPtr(true),
				},
			},
		})
	}

	// Add notification subscription Secret volume
	// Secret name follows the pattern: notification-subscriptions-{userID}
	notificationSecretName := fmt.Sprintf("notification-subscriptions-%s", sanitizeLabelValue(session.request.UserID))
	volumes = append(volumes, corev1.Volume{
		Name: "notification-subscriptions",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: notificationSecretName,
				Optional:   boolPtr(true), // Optional - user may not have subscriptions
			},
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

	// Delete Credential Secret (if exists)
	if session.secretName != "" {
		err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, session.secretName, deleteOptions)
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Sprintf("secret: %v", err))
		}
	}

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

	// Delete PVC
	err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.pvcName, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("pvc: %v", err))
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

// int64Ptr returns a pointer to an int64
func int64Ptr(i int64) *int64 {
	return &i
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool {
	return &b
}

// createCredentialSecret creates a Kubernetes Secret for Claude credentials
// The secret contains credentials.json file that will be mounted to $HOME/.claude/.credentials.json
func (m *KubernetesSessionManager) createCredentialSecret(ctx context.Context, session *kubernetesSession, creds *ClaudeCredentials) error {
	var credentialsBytes []byte

	// Use RawJSON if available (preserves original file format)
	if len(creds.RawJSON) > 0 {
		credentialsBytes = creds.RawJSON
	} else {
		// Fall back to constructing JSON from fields (for EnvCredentialProvider)
		expiresAt, err := strconv.ParseInt(creds.ExpiresAt, 10, 64)
		if err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to parse expiresAt '%s': %v", creds.ExpiresAt, err)
			expiresAt = 0
		}

		credentialsJSON := map[string]interface{}{
			"claudeAiOauth": map[string]interface{}{
				"accessToken":  creds.AccessToken,
				"refreshToken": creds.RefreshToken,
				"expiresAt":    expiresAt,
			},
		}

		var err2 error
		credentialsBytes, err2 = json.Marshal(credentialsJSON)
		if err2 != nil {
			return fmt.Errorf("failed to marshal credentials: %w", err2)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.secretName,
			Namespace: m.namespace,
			Labels:    m.buildLabels(session),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"credentials.json": credentialsBytes,
		},
	}

	_, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

// deleteCredentialSecret deletes the credential Secret for a session
func (m *KubernetesSessionManager) deleteCredentialSecret(ctx context.Context, session *kubernetesSession) error {
	return m.client.CoreV1().Secrets(m.namespace).Delete(ctx, session.secretName, metav1.DeleteOptions{})
}

// GetClient returns the Kubernetes client (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetClient() kubernetes.Interface {
	return m.client
}

// GetNamespace returns the Kubernetes namespace (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetNamespace() string {
	return m.namespace
}
