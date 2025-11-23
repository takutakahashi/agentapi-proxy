//go:build wireinject
// +build wireinject

package di

import (
	"github.com/google/wire"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/config"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/agent"
	port_repositories "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	port_services "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func InitializeAgentController() (*controllers.AgentController, error) {
	wire.Build(
		config.NewKubernetesConfig,
		provideKubernetesClient,
		provideKubernetesClientset,
		provideAgentRepository,
		provideSessionRepository,
		provideKubernetesService,
		agent.NewAgentManager,
		controllers.NewAgentController,
	)
	return nil, nil
}

func provideKubernetesClient(cfg *config.KubernetesConfig) client.Client {
	return cfg.Client
}

func provideKubernetesClientset(cfg *config.KubernetesConfig) kubernetes.Interface {
	return cfg.ClientSet
}

func provideAgentRepository(client client.Client) port_repositories.AgentRepository {
	return repositories.NewKubernetesAgentRepositoryV2(client)
}

func provideSessionRepository() port_repositories.SessionRepository {
	return repositories.NewMemorySessionRepository()
}

func provideKubernetesService(client client.Client, clientset kubernetes.Interface) port_services.KubernetesService {
	return services.NewKubernetesServiceV2(client, clientset)
}
