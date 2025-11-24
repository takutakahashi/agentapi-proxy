package services

import (
	"context"
)

type KubernetesService interface {
	CreateAgentStatefulSet(ctx context.Context, agentID, sessionID string) error
	CreateAgentStatefulSetWithConfig(ctx context.Context, config AgentResourceConfig) error
	DeleteStatefulSet(ctx context.Context, agentID string) error
	CreateAgentPod(ctx context.Context, sessionID string) (string, error)
	DeletePod(ctx context.Context, podName string) error
	GetPodStatus(ctx context.Context, podName string) (string, error)
	ScalePods(ctx context.Context, sessionID string, replicas int) error
	ListPodsBySession(ctx context.Context, sessionID string) ([]PodInfo, error)
	GetPodLogs(ctx context.Context, podName string, lines int) ([]string, error)
	UpdatePodLabels(ctx context.Context, podName string, labels map[string]string) error
	GetPodMetrics(ctx context.Context, podName string) (*PodMetrics, error)
	CreateConfigMap(ctx context.Context, name string, data map[string]string) error
	UpdateConfigMap(ctx context.Context, name string, data map[string]string) error
	DeleteConfigMap(ctx context.Context, name string) error
	CreateSecret(ctx context.Context, name string, data map[string][]byte) error
	UpdateSecret(ctx context.Context, name string, data map[string][]byte) error
	DeleteSecret(ctx context.Context, name string) error
	CreateUserConfigMap(ctx context.Context, userID string, notificationTargets []string) error
	CreateUserSecret(ctx context.Context, userID string, envVars map[string]string) error
	DeleteUserResources(ctx context.Context, userID string) error
}

type AgentResourceConfig struct {
	AgentID       string
	SessionID     string
	UserID        string
	Image         string
	MemoryRequest string
	CPURequest    string
	MemoryLimit   string
	CPULimit      string
	StorageSize   string
	Namespace     string
}

type PodInfo struct {
	Name      string
	Status    string
	IP        string
	NodeName  string
	StartTime string
	Labels    map[string]string
}

type PodMetrics struct {
	CPUUsage    string
	MemoryUsage string
	NetworkIn   int64
	NetworkOut  int64
}
