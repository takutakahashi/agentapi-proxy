package importexportmodule

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/k8sutil"
	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/modulehost"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterHandlers registers import/export REST API handlers.
func RegisterHandlers(configData *config.Config, proxyServer modulehost.ImportExportHost) {
	log.Printf("[IMPORT_EXPORT_HANDLERS] Registering import/export handlers...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Kubernetes config not available, skipping import/export handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Failed to create Kubernetes client, skipping import/export handlers: %v", err)
		return
	}

	namespace := k8sutil.ResolveNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)
	scheduleManager := schedule.NewKubernetesManager(client, namespace)
	webhookRepo := repositories.NewKubernetesWebhookRepository(client, namespace)

	if configData.Webhook.GitHubEnterpriseHost != "" {
		webhookRepo.SetDefaultGitHubEnterpriseHost(configData.Webhook.GitHubEnterpriseHost)
	}

	settingsRepo := proxyServer.GetSettingsRepository()
	encryptionFactory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	encryptionService, err := encryptionFactory.Create()
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Failed to create encryption service, using noop: %v", err)
		encryptionService = services.NewNoopEncryptionService()
	}
	log.Printf("[IMPORT_EXPORT_HANDLERS] Using encryption algorithm: %s", encryptionService.Algorithm())

	importExportHandlers := importexport.NewHandlers(scheduleManager, webhookRepo, settingsRepo, encryptionService)
	proxyServer.AddCustomHandler(importExportHandlers)

	log.Printf("[IMPORT_EXPORT_HANDLERS] Import/export handlers registered successfully")
}
