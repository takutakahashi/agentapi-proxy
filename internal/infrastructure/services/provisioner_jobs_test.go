package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestEnsureProvisionerTokenCreatesSecret(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	lgr := logger.NewLogger()

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	if manager.k8sConfig.ProvisionerToken == "" {
		t.Fatal("ProvisionerToken should be generated")
	}

	sec, err := manager.client.CoreV1().Secrets("test-ns").Get(context.Background(), provisionerTokenSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected token Secret to be created: %v", err)
	}
	if got := string(sec.Data[provisionerTokenSecretKey]); got != manager.k8sConfig.ProvisionerToken {
		t.Fatalf("Secret token = %q, manager token = %q", got, manager.k8sConfig.ProvisionerToken)
	}
}

func TestEnsureProvisionerTokenReusesExistingSecret(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	lgr := logger.NewLogger()
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provisionerTokenSecretName,
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			provisionerTokenSecretKey: []byte("existing-token"),
		},
	}

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, fake.NewSimpleClientset(existing))
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	if manager.k8sConfig.ProvisionerToken != "existing-token" {
		t.Fatalf("ProvisionerToken = %q, want existing-token", manager.k8sConfig.ProvisionerToken)
	}
}
