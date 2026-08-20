package services

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
)

type staticRuntimeConfigProvider struct{ config *config.Config }

func (p staticRuntimeConfigProvider) Current() *config.Config { return p.config }
func (staticRuntimeConfigProvider) AgentDefaults() settingspatch.SettingsPatch {
	return settingspatch.SettingsPatch{}
}

func TestApplyRuntimeProfileInheritsSciaNFAAndSessionRBAC(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "esm-sessions"
	client := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	profile := &sessionsettings.RuntimeProfile{
		Version: 1,
		Kubernetes: sessionsettings.KubernetesRuntimeProfile{
			ServiceAccount:                 "parent-session",
			NetworkFilterImage:             "nfa:parent",
			NetworkFilterCPURequest:        "111m",
			NetworkFilterCPULimit:          "222m",
			NetworkFilterMemoryRequest:     "111Mi",
			NetworkFilterMemoryLimit:       "222Mi",
			NetworkFilterInitCPURequest:    "11m",
			NetworkFilterInitCPULimit:      "22m",
			NetworkFilterInitMemoryRequest: "11Mi",
			NetworkFilterInitMemoryLimit:   "22Mi",
		},
		Scia: sessionsettings.SciaRuntimeProfile{
			Enabled:                   true,
			SessionSidecarEnabled:     true,
			SessionSidecarImage:       "scia:parent",
			SessionSidecarConfigImage: "busybox:parent",
			SessionSidecarPort:        18082,
			NoProxy:                   ".svc",
			GoogleHosts:               []string{"google.example"},
			TodoistHosts:              []string{"todoist.example"},
		},
	}

	if err := manager.ApplyRuntimeProfile(context.Background(), profile); err != nil {
		t.Fatalf("ApplyRuntimeProfile() error = %v", err)
	}
	if manager.k8sConfig.ServiceAccount != "parent-session" || manager.k8sConfig.NetworkFilterImage != "nfa:parent" {
		t.Fatalf("kubernetes profile was not inherited: %#v", manager.k8sConfig)
	}
	if !manager.config.Scia.Enabled || !manager.config.Scia.SessionSidecarEnabled || manager.config.Scia.SessionSidecarImage != "scia:parent" {
		t.Fatalf("scia profile was not inherited: %#v", manager.config.Scia)
	}
	localConfig := config.DefaultConfig()
	localConfig.Scia.Enabled = false
	manager.SetConfigProvider(staticRuntimeConfigProvider{config: localConfig})
	manager.refreshConfig()
	if !manager.config.Scia.Enabled || manager.config.Scia.SessionSidecarImage != "scia:parent" || manager.k8sConfig.NetworkFilterImage != "nfa:parent" {
		t.Fatalf("runtime refresh discarded inherited profile: scia=%#v kubernetes=%#v", manager.config.Scia, manager.k8sConfig)
	}
	if _, err := client.CoreV1().ServiceAccounts("esm-sessions").Get(context.Background(), "parent-session", metav1.GetOptions{}); err != nil {
		t.Fatalf("inherited service account not created: %v", err)
	}
	role, err := client.RbacV1().Roles("esm-sessions").Get(context.Background(), "parent-session", metav1.GetOptions{})
	if err != nil || len(role.Rules) == 0 {
		t.Fatalf("inherited role not created: role=%#v err=%v", role, err)
	}
	if _, err := client.RbacV1().RoleBindings("esm-sessions").Get(context.Background(), "parent-session", metav1.GetOptions{}); err != nil {
		t.Fatalf("inherited role binding not created: %v", err)
	}
}

func TestApplyRuntimeProfileRejectsUnknownVersion(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "esm-sessions"
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	if err := manager.ApplyRuntimeProfile(context.Background(), &sessionsettings.RuntimeProfile{Version: 2}); err == nil {
		t.Fatal("ApplyRuntimeProfile() error = nil, want unsupported version error")
	}
}
