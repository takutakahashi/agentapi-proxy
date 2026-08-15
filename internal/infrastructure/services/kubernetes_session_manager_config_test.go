package services

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
)

type staticConfigProvider struct {
	config *config.Config
}

func (p staticConfigProvider) Current() *config.Config { return p.config }

func (staticConfigProvider) AgentDefaults() settingspatch.SettingsPatch {
	return settingspatch.SettingsPatch{}
}

func TestRefreshConfigPreservesProvisionerToken(t *testing.T) {
	manager := &KubernetesSessionManager{
		k8sConfig: &config.KubernetesSessionConfig{ProvisionerToken: "startup-secret"},
	}
	manager.SetConfigProvider(staticConfigProvider{config: &config.Config{}})

	if got := manager.k8sConfig.ProvisionerToken; got != "startup-secret" {
		t.Fatalf("ProvisionerToken = %q, want startup secret", got)
	}
}
