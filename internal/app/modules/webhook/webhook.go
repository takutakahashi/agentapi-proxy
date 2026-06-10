package webhookmodule

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/modulehost"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/webhook"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterHandlers registers webhook REST API handlers.
func RegisterHandlers(configData *config.Config, proxyServer modulehost.WebhookHost) {
	log.Printf("[WEBHOOK_HANDLERS] Registering webhook handlers...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[WEBHOOK_HANDLERS] Kubernetes config not available, skipping webhook handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[WEBHOOK_HANDLERS] Failed to create Kubernetes client, skipping webhook handlers: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)
	webhookRepo := repositories.NewKubernetesWebhookRepository(client, namespace)

	if configData.Webhook.GitHubEnterpriseHost != "" {
		webhookRepo.SetDefaultGitHubEnterpriseHost(configData.Webhook.GitHubEnterpriseHost)
		log.Printf("[WEBHOOK_HANDLERS] Default GitHub Enterprise host configured: %s", configData.Webhook.GitHubEnterpriseHost)
	}

	webhookHandlers := webhook.NewHandlers(webhookRepo, proxyServer.GetSessionManager(), configData.Webhook.BaseURL, proxyServer.GetMemoryRepository(), proxyServer.GetSessionProfileRepository())
	proxyServer.AddCustomHandler(webhookHandlers)

	if configData.Webhook.BaseURL != "" {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL configured: %s", configData.Webhook.BaseURL)
	} else {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL not configured, will auto-detect from request headers")
	}
	log.Printf("[WEBHOOK_HANDLERS] Webhook handlers registered successfully")
}
