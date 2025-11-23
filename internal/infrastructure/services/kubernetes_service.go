package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	agentNamespace = "agentapi-proxy"
	agentImage     = "agentapi-proxy:latest"
)

type KubernetesServiceImpl struct {
	client kubernetes.Interface
}

func NewKubernetesService(client kubernetes.Interface) services.KubernetesService {
	return &KubernetesServiceImpl{
		client: client,
	}
}

func (s *KubernetesServiceImpl) CreateAgentPod(ctx context.Context, sessionID string) (string, error) {
	podName := fmt.Sprintf("agent-%s-%d", sessionID, metav1.Now().Unix())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentNamespace,
			Labels: map[string]string{
				"app":        "agentapi-proxy",
				"component":  "agent",
				"session-id": sessionID,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "agent",
					Image: agentImage,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8080,
							Name:          "http",
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "SESSION_ID",
							Value: sessionID,
						},
						{
							Name: "POD_NAME",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						{
							Name: "POD_NAMESPACE",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							},
						},
						{
							Name: "POD_IP",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt(8080),
							},
						},
						InitialDelaySeconds: 30,
						PeriodSeconds:       10,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/ready",
								Port: intstr.FromInt(8080),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       5,
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	created, err := s.client.CoreV1().Pods(agentNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create pod: %w", err)
	}

	return created.Name, nil
}

func (s *KubernetesServiceImpl) DeletePod(ctx context.Context, podName string) error {
	err := s.client.CoreV1().Pods(agentNamespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pod: %w", err)
	}
	return nil
}

func (s *KubernetesServiceImpl) GetPodStatus(ctx context.Context, podName string) (string, error) {
	pod, err := s.client.CoreV1().Pods(agentNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return "NotFound", nil
		}
		return "", fmt.Errorf("failed to get pod: %w", err)
	}

	return string(pod.Status.Phase), nil
}

func (s *KubernetesServiceImpl) ScalePods(ctx context.Context, sessionID string, replicas int) error {
	labelSelector := fmt.Sprintf("session-id=%s", sessionID)
	pods, err := s.client.CoreV1().Pods(agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	currentCount := len(pods.Items)

	if currentCount == replicas {
		return nil
	}

	if currentCount > replicas {
		toDelete := currentCount - replicas
		for i := 0; i < toDelete && i < len(pods.Items); i++ {
			if err := s.DeletePod(ctx, pods.Items[i].Name); err != nil {
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

func (s *KubernetesServiceImpl) ListPodsBySession(ctx context.Context, sessionID string) ([]services.PodInfo, error) {
	labelSelector := fmt.Sprintf("session-id=%s", sessionID)
	pods, err := s.client.CoreV1().Pods(agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	podInfos := make([]services.PodInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		podInfo := services.PodInfo{
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

	req := s.client.CoreV1().Pods(agentNamespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer logs.Close()

	buf := make([]byte, 2048)
	var logLines []string
	var currentLine strings.Builder

	for {
		n, err := logs.Read(buf)
		if n > 0 {
			data := string(buf[:n])
			lines := strings.Split(data, "\n")

			for i, line := range lines {
				if i == 0 {
					currentLine.WriteString(line)
				} else {
					if currentLine.Len() > 0 {
						logLines = append(logLines, currentLine.String())
						currentLine.Reset()
					}
					if i < len(lines)-1 || strings.HasSuffix(data, "\n") {
						logLines = append(logLines, line)
					} else {
						currentLine.WriteString(line)
					}
				}
			}
		}
		if err != nil {
			if err.Error() != "EOF" && !strings.Contains(err.Error(), "EOF") {
				return nil, fmt.Errorf("error reading logs: %w", err)
			}
			break
		}
	}

	if currentLine.Len() > 0 {
		logLines = append(logLines, currentLine.String())
	}

	return logLines, nil
}

func (s *KubernetesServiceImpl) UpdatePodLabels(ctx context.Context, podName string, labels map[string]string) error {
	pod, err := s.client.CoreV1().Pods(agentNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}

	for k, v := range labels {
		pod.Labels[k] = v
	}

	_, err = s.client.CoreV1().Pods(agentNamespace).Update(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update pod labels: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) GetPodMetrics(ctx context.Context, podName string) (*services.PodMetrics, error) {
	return &services.PodMetrics{
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

	_, err := s.client.CoreV1().ConfigMaps(agentNamespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create configmap: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) UpdateConfigMap(ctx context.Context, name string, data map[string]string) error {
	cm, err := s.client.CoreV1().ConfigMaps(agentNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configmap: %w", err)
	}

	cm.Data = data
	_, err = s.client.CoreV1().ConfigMaps(agentNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}

func (s *KubernetesServiceImpl) DeleteConfigMap(ctx context.Context, name string) error {
	err := s.client.CoreV1().ConfigMaps(agentNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete configmap: %w", err)
	}
	return nil
}
