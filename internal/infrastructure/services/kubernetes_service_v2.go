package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubernetesServiceV2 struct {
	client    client.Client
	clientset kubernetes.Interface
}

func NewKubernetesServiceV2(client client.Client, clientset kubernetes.Interface) services.KubernetesService {
	return &KubernetesServiceV2{
		client:    client,
		clientset: clientset,
	}
}

func (s *KubernetesServiceV2) CreateAgentPod(ctx context.Context, sessionID string) (string, error) {
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

	if err := s.client.Create(ctx, pod); err != nil {
		return "", fmt.Errorf("failed to create pod: %w", err)
	}

	return pod.Name, nil
}

func (s *KubernetesServiceV2) DeletePod(ctx context.Context, podName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentNamespace,
		},
	}

	if err := s.client.Delete(ctx, pod); err != nil && !client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}
	return nil
}

func (s *KubernetesServiceV2) GetPodStatus(ctx context.Context, podName string) (string, error) {
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

func (s *KubernetesServiceV2) ScalePods(ctx context.Context, sessionID string, replicas int) error {
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

func (s *KubernetesServiceV2) ListPodsBySession(ctx context.Context, sessionID string) ([]services.PodInfo, error) {
	podList := &corev1.PodList{}
	if err := s.client.List(ctx, podList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{"session-id": sessionID},
	); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	podInfos := make([]services.PodInfo, 0, len(podList.Items))
	for _, pod := range podList.Items {
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

func (s *KubernetesServiceV2) GetPodLogs(ctx context.Context, podName string, lines int) ([]string, error) {
	tailLines := int64(lines)
	logOptions := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}

	req := s.clientset.CoreV1().Pods(agentNamespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer logs.Close()

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, logs); err != nil {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	logLines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	return logLines, nil
}

func (s *KubernetesServiceV2) UpdatePodLabels(ctx context.Context, podName string, labels map[string]string) error {
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

func (s *KubernetesServiceV2) GetPodMetrics(ctx context.Context, podName string) (*services.PodMetrics, error) {
	return &services.PodMetrics{
		CPUUsage:    "100m",
		MemoryUsage: "128Mi",
		NetworkIn:   0,
		NetworkOut:  0,
	}, nil
}

func (s *KubernetesServiceV2) CreateConfigMap(ctx context.Context, name string, data map[string]string) error {
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

func (s *KubernetesServiceV2) UpdateConfigMap(ctx context.Context, name string, data map[string]string) error {
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

func (s *KubernetesServiceV2) DeleteConfigMap(ctx context.Context, name string) error {
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
