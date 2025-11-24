package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	portServices "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	agentNamespace = "agentapi-proxy"
	agentImage     = "agentapi-proxy:latest"
)

type KubernetesServiceImpl struct {
	client    client.Client
	clientset kubernetes.Interface
}

func NewKubernetesService(client client.Client, clientset kubernetes.Interface) portServices.KubernetesService {
	return &KubernetesServiceImpl{
		client:    client,
		clientset: clientset,
	}
}

func (s *KubernetesServiceImpl) CreateAgentStatefulSet(ctx context.Context, agentID, sessionID string) error {
	return s.CreateAgentStatefulSetWithConfig(ctx, portServices.AgentResourceConfig{
		AgentID:   agentID,
		SessionID: sessionID,
		Namespace: agentNamespace,
	})
}

func (s *KubernetesServiceImpl) CreateAgentStatefulSetWithConfig(ctx context.Context, config portServices.AgentResourceConfig) error {
	builder := NewAgentResourceBuilder(config)

	// Create headless service first
	service := builder.BuildHeadlessService()
	if err := s.client.Create(ctx, service); err != nil {
		return fmt.Errorf("failed to create headless service: %w", err)
	}

	// Create StatefulSet
	statefulset := builder.BuildStatefulSet()
	if err := s.client.Create(ctx, statefulset); err != nil {
		// Clean up service if StatefulSet creation fails
		_ = s.client.Delete(ctx, service)
		return fmt.Errorf("failed to create statefulset: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) CreateAgentPod(ctx context.Context, sessionID string) (string, error) {
	// This method is deprecated in favor of CreateAgentStatefulSet
	// Keep for backward compatibility but redirect to StatefulSet creation
	agentID := fmt.Sprintf("agent-%d", metav1.Now().Unix())

	if err := s.CreateAgentStatefulSet(ctx, agentID, sessionID); err != nil {
		return "", err
	}

	return fmt.Sprintf("agent-%s-0", agentID), nil
}

func (s *KubernetesServiceImpl) DeleteStatefulSet(ctx context.Context, agentID string) error {
	builder := NewAgentResourceBuilder(portServices.AgentResourceConfig{
		AgentID:   agentID,
		Namespace: agentNamespace,
	})

	serviceName, statefulsetName := builder.GetResourceNames()

	// Delete StatefulSet
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulsetName,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, statefulset); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete statefulset: %w", err)
	}

	// Delete headless service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, service); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete headless service: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) DeletePod(ctx context.Context, podName string) error {
	// Extract agent ID from pod name (format: agent-{agentID}-0)
	if strings.HasPrefix(podName, "agent-") && strings.HasSuffix(podName, "-0") {
		agentID := strings.TrimSuffix(strings.TrimPrefix(podName, "agent-"), "-0")
		return s.DeleteStatefulSet(ctx, agentID)
	}

	// Fallback to direct pod deletion for backward compatibility
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, pod); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}
	return nil
}

func (s *KubernetesServiceImpl) GetPodStatus(ctx context.Context, podName string) (string, error) {
	pod := &corev1.Pod{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      podName,
	}

	if err := s.client.Get(ctx, key, pod); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return "", fmt.Errorf("failed to get pod: %w", err)
		}
		return "NotFound", nil
	}

	return string(pod.Status.Phase), nil
}

func (s *KubernetesServiceImpl) ScalePods(ctx context.Context, sessionID string, replicas int) error {
	podList := &corev1.PodList{}
	if err := s.client.List(ctx, podList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{"session-id": sessionID},
	); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	currentCount := len(podList.Items)

	if currentCount == replicas {
		return nil
	}

	if currentCount > replicas {
		toDelete := currentCount - replicas
		for i := 0; i < toDelete && i < len(podList.Items); i++ {
			if err := s.DeletePod(ctx, podList.Items[i].Name); err != nil {
				return fmt.Errorf("failed to delete pod during scale down: %w", err)
			}
		}
	} else {
		toCreate := replicas - currentCount
		for i := 0; i < toCreate; i++ {
			if _, err := s.CreateAgentPod(ctx, sessionID); err != nil {
				return fmt.Errorf("failed to create pod during scale up: %w", err)
			}
		}
	}

	return nil
}

func (s *KubernetesServiceImpl) ListPodsBySession(ctx context.Context, sessionID string) ([]portServices.PodInfo, error) {
	podList := &corev1.PodList{}
	if err := s.client.List(ctx, podList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{"session-id": sessionID},
	); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	podInfos := make([]portServices.PodInfo, 0, len(podList.Items))
	for _, pod := range podList.Items {
		podInfo := portServices.PodInfo{
			Name:     pod.Name,
			Status:   string(pod.Status.Phase),
			IP:       pod.Status.PodIP,
			NodeName: pod.Spec.NodeName,
			Labels:   pod.Labels,
		}
		if pod.Status.StartTime != nil {
			podInfo.StartTime = pod.Status.StartTime.Format("2006-01-02 15:04:05")
		}
		podInfos = append(podInfos, podInfo)
	}

	return podInfos, nil
}

func (s *KubernetesServiceImpl) GetPodLogs(ctx context.Context, podName string, lines int) ([]string, error) {
	tailLines := int64(lines)
	logOptions := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}

	req := s.clientset.CoreV1().Pods(agentNamespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer func() { _ = logs.Close() }()

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, logs); err != nil {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	logLines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	return logLines, nil
}

func (s *KubernetesServiceImpl) UpdatePodLabels(ctx context.Context, podName string, labels map[string]string) error {
	pod := &corev1.Pod{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      podName,
	}

	if err := s.client.Get(ctx, key, pod); err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}

	for k, v := range labels {
		pod.Labels[k] = v
	}

	if err := s.client.Update(ctx, pod); err != nil {
		return fmt.Errorf("failed to update pod labels: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) GetPodMetrics(ctx context.Context, podName string) (*portServices.PodMetrics, error) {
	return &portServices.PodMetrics{
		CPUUsage:    "100m",
		MemoryUsage: "128Mi",
		NetworkIn:   0,
		NetworkOut:  0,
	}, nil
}

func (s *KubernetesServiceImpl) CreateConfigMap(ctx context.Context, name string, data map[string]string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agentNamespace,
		},
		Data: data,
	}

	if err := s.client.Create(ctx, configMap); err != nil {
		return fmt.Errorf("failed to create configmap: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) UpdateConfigMap(ctx context.Context, name string, data map[string]string) error {
	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      name,
	}

	if err := s.client.Get(ctx, key, configMap); err != nil {
		return fmt.Errorf("failed to get configmap: %w", err)
	}

	configMap.Data = data
	if err := s.client.Update(ctx, configMap); err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) DeleteConfigMap(ctx context.Context, name string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, configMap); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete configmap: %w", err)
	}
	return nil
}

func (s *KubernetesServiceImpl) CreateSecret(ctx context.Context, name string, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agentNamespace,
		},
		Data: data,
	}

	if err := s.client.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to create secret: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) UpdateSecret(ctx context.Context, name string, data map[string][]byte) error {
	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      name,
	}

	if err := s.client.Get(ctx, key, secret); err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	secret.Data = data
	if err := s.client.Update(ctx, secret); err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) DeleteSecret(ctx context.Context, name string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, secret); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}
	return nil
}

func (s *KubernetesServiceImpl) CreateUserConfigMap(ctx context.Context, userID string, notificationTargets []string) error {
	configMapName := fmt.Sprintf("user-%s-notifications", userID)

	// Create notification targets as JSON
	targetData := strings.Join(notificationTargets, ",")
	data := map[string]string{
		"notification_targets.txt": targetData,
	}

	return s.CreateConfigMap(ctx, configMapName, data)
}

func (s *KubernetesServiceImpl) CreateUserSecret(ctx context.Context, userID string, envVars map[string]string) error {
	secretName := fmt.Sprintf("user-%s-env", userID)

	// Convert string map to byte map
	data := make(map[string][]byte)
	for key, value := range envVars {
		data[key] = []byte(value)
	}

	return s.CreateSecret(ctx, secretName, data)
}

func (s *KubernetesServiceImpl) DeleteUserResources(ctx context.Context, userID string) error {
	configMapName := fmt.Sprintf("user-%s-notifications", userID)
	secretName := fmt.Sprintf("user-%s-env", userID)

	// Delete ConfigMap
	if err := s.DeleteConfigMap(ctx, configMapName); err != nil {
		return fmt.Errorf("failed to delete user configmap: %w", err)
	}

	// Delete Secret
	if err := s.DeleteSecret(ctx, secretName); err != nil {
		return fmt.Errorf("failed to delete user secret: %w", err)
	}

	return nil
}
