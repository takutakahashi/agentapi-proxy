package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	EnvTestNamespace = "agentapi-proxy"
	TestTimeout      = 10 * time.Second
)

type EnvTestSuite struct {
	testEnv   *envtest.Environment
	cfg       *rest.Config
	client    client.Client
	clientset kubernetes.Interface
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewEnvTestSuite() *EnvTestSuite {
	return &EnvTestSuite{}
}

func (s *EnvTestSuite) Setup() error {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: false,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "testbin", "k8s", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", "linux", "amd64")),
	}

	cfg, err := s.testEnv.Start()
	if err != nil {
		return fmt.Errorf("failed to start test environment: %w", err)
	}
	s.cfg = cfg

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	s.client = k8sClient

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}
	s.clientset = clientset

	if err := s.setupNamespace(); err != nil {
		return fmt.Errorf("failed to setup namespace: %w", err)
	}

	return nil
}

func (s *EnvTestSuite) setupNamespace() error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: EnvTestNamespace,
		},
	}

	if err := s.client.Create(s.ctx, namespace); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create namespace: %w", err)
		}
	}
	return nil
}

func (s *EnvTestSuite) Teardown() error {
	if s.cancel != nil {
		s.cancel()
	}

	if s.testEnv != nil {
		return s.testEnv.Stop()
	}
	return nil
}

func (s *EnvTestSuite) GetClient() client.Client {
	return s.client
}

func (s *EnvTestSuite) GetClientset() kubernetes.Interface {
	return s.clientset
}

func (s *EnvTestSuite) GetContext() context.Context {
	return s.ctx
}

func (s *EnvTestSuite) GetConfig() *rest.Config {
	return s.cfg
}

func (s *EnvTestSuite) CreateTestAgent(id, sessionID string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("agent-%s", id),
			Namespace: EnvTestNamespace,
			Labels: map[string]string{
				"type":       "agent",
				"session-id": sessionID,
				"agent-id":   id,
			},
		},
		Data: map[string]string{
			"agent.json": fmt.Sprintf(`{"id":"%s","sessionId":"%s","status":"active"}`, id, sessionID),
		},
	}

	if err := s.client.Create(s.ctx, configMap); err != nil {
		return nil, fmt.Errorf("failed to create test agent configmap: %w", err)
	}

	return configMap, nil
}

func (s *EnvTestSuite) CleanupTestResources() error {
	configMapList := &corev1.ConfigMapList{}
	if err := s.client.List(s.ctx, configMapList,
		client.InNamespace(EnvTestNamespace),
		client.MatchingLabels{"type": "agent"},
	); err != nil {
		return fmt.Errorf("failed to list configmaps: %w", err)
	}

	for _, cm := range configMapList.Items {
		if err := s.client.Delete(s.ctx, &cm); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete configmap %s: %w", cm.Name, err)
			}
		}
	}

	return nil
}
