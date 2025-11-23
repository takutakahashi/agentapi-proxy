package services

import (
	"context"
)

type KubernetesService interface {
	CreateAgentStatefulSet(ctx context.Context, agentID, sessionID string) error
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
