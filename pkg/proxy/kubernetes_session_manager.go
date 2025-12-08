package proxy

import (
	"context"
	"fmt"
	"log"
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

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// KubernetesSessionManager manages sessions using Kubernetes Deployments
type KubernetesSessionManager struct {
	config    *config.Config
	k8sConfig *config.KubernetesSessionConfig
	client    kubernetes.Interface
	verbose   bool
	logger    *logger.Logger
	sessions  map[string]*kubernetesSession
	mutex     sync.RWMutex
	namespace string
}

// NewKubernetesSessionManager creates a new KubernetesSessionManager
func NewKubernetesSessionManager(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
) (*KubernetesSessionManager, error) {
	k8sConfig := &cfg.KubernetesSession

	// Get config using controller-runtime (supports in-cluster and kubeconfig)
	restConfig := ctrl.GetConfigOrDie()

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

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

	// Create PVC
	if err := m.createPVC(ctx, session); err != nil {
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}
	log.Printf("[K8S_SESSION] Created PVC %s for session %s", pvcName, id)

	// Create Deployment
	if err := m.createDeployment(ctx, session, req); err != nil {
		if delErr := m.deletePVC(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup PVC after deployment creation failure: %v", delErr)
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
					Containers: []corev1.Container{
						{
							Name:            "agentapi",
							Image:           m.k8sConfig.Image,
							ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
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
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(m.k8sConfig.BasePort),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(m.k8sConfig.BasePort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "workdir",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: session.pvcName,
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := m.client.AppsV1().Deployments(m.namespace).Create(ctx, deployment, metav1.CreateOptions{})
	return err
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

	// Add repository info if available
	if req.RepoInfo != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "AGENTAPI_REPO_FULLNAME", Value: req.RepoInfo.FullName},
			corev1.EnvVar{Name: "AGENTAPI_CLONE_DIR", Value: req.RepoInfo.CloneDir},
		)
	}

	// Add environment variables from request
	for k, v := range req.Environment {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
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
