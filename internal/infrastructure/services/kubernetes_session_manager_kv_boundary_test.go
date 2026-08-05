package services

import (
	"context"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesSessionManagerNeverInitializesKVBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test"
	cfg.KVStore.Backend = "libsql"
	cfg.KVStore.DatabaseURL = "http://must-not-be-contacted.invalid"
	client := fake.NewSimpleClientset()

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("session manager must not initialize application KV backend: %v", err)
	}
	if manager.GetClient() != client {
		t.Fatal("session manager must retain the raw Kubernetes client")
	}
	if _, err := client.CoreV1().Secrets("test").Get(context.Background(), provisionerTokenSecretName, metav1.GetOptions{}); err != nil {
		t.Fatalf("session-manager Secret was not created in Kubernetes: %v", err)
	}
}
