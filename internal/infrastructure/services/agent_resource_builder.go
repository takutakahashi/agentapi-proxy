package services

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type AgentResourceConfig struct {
	AgentID       string
	SessionID     string
	Image         string
	MemoryRequest string
	CPURequest    string
	MemoryLimit   string
	CPULimit      string
	StorageSize   string
	Namespace     string
}

type AgentResourceBuilder struct {
	config AgentResourceConfig
}

func NewAgentResourceBuilder(config AgentResourceConfig) *AgentResourceBuilder {
	// Set defaults
	if config.Image == "" {
		config.Image = "agentapi-proxy:latest"
	}
	if config.MemoryRequest == "" {
		config.MemoryRequest = "256Mi"
	}
	if config.CPURequest == "" {
		config.CPURequest = "100m"
	}
	if config.MemoryLimit == "" {
		config.MemoryLimit = "512Mi"
	}
	if config.CPULimit == "" {
		config.CPULimit = "500m"
	}
	if config.StorageSize == "" {
		config.StorageSize = "1Gi"
	}
	if config.Namespace == "" {
		config.Namespace = "agentapi-proxy"
	}

	return &AgentResourceBuilder{config: config}
}

func (b *AgentResourceBuilder) BuildHeadlessService() *corev1.Service {
	serviceName := "agent-" + b.config.AgentID + "-headless"

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: b.config.Namespace,
			Labels: map[string]string{
				"app":        "agentapi-proxy",
				"component":  "agent",
				"agent-id":   b.config.AgentID,
				"session-id": b.config.SessionID,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector: map[string]string{
				"app":       "agentapi-proxy",
				"component": "agent",
				"agent-id":  b.config.AgentID,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Name:       "http",
				},
			},
		},
	}
}

func (b *AgentResourceBuilder) BuildStatefulSet() *appsv1.StatefulSet {
	statefulsetName := "agent-" + b.config.AgentID
	serviceName := "agent-" + b.config.AgentID + "-headless"
	replicas := int32(1)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulsetName,
			Namespace: b.config.Namespace,
			Labels: map[string]string{
				"app":        "agentapi-proxy",
				"component":  "agent",
				"agent-id":   b.config.AgentID,
				"session-id": b.config.SessionID,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: serviceName,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":       "agentapi-proxy",
					"component": "agent",
					"agent-id":  b.config.AgentID,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":        "agentapi-proxy",
						"component":  "agent",
						"agent-id":   b.config.AgentID,
						"session-id": b.config.SessionID,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: b.config.Image,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Name:          "http",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "AGENT_ID",
									Value: b.config.AgentID,
								},
								{
									Name:  "SESSION_ID",
									Value: b.config.SessionID,
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
								{
									Name:  "SESSION_PROVIDER",
									Value: "kubernetes",
								},
								{
									Name:  "K8S_NAMESPACE",
									Value: b.config.Namespace,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(b.config.CPURequest),
									corev1.ResourceMemory: resource.MustParse(b.config.MemoryRequest),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(b.config.CPULimit),
									corev1.ResourceMemory: resource.MustParse(b.config.MemoryLimit),
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
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(b.config.StorageSize),
							},
						},
					},
				},
			},
		},
	}
}

func (b *AgentResourceBuilder) GetResourceNames() (serviceName, statefulsetName string) {
	serviceName = "agent-" + b.config.AgentID + "-headless"
	statefulsetName = "agent-" + b.config.AgentID
	return
}
