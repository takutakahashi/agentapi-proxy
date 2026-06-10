package modulehost

import (
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type Registrar interface {
	AddCustomHandler(handler app.CustomHandler)
}

type SessionManagerProvider interface {
	GetSessionManager() portrepos.SessionManager
}

type ShareRepositoryProvider interface {
	GetShareRepository() portrepos.ShareRepository
}

type SettingsRepositoryProvider interface {
	GetSettingsRepository() portrepos.SettingsRepository
}

type MemoryRepositoryProvider interface {
	GetMemoryRepository() portrepos.MemoryRepository
}

type TaskRepositoryProvider interface {
	GetTaskRepository() portrepos.TaskRepository
}

type TaskGroupRepositoryProvider interface {
	GetTaskGroupRepository() portrepos.TaskGroupRepository
}

type SessionProfileRepositoryProvider interface {
	GetSessionProfileRepository() portrepos.SessionProfileRepository
}

type ScheduleHost interface {
	Registrar
	SessionManagerProvider
	MemoryRepositoryProvider
	SessionProfileRepositoryProvider
}

type WebhookHost interface {
	Registrar
	SessionManagerProvider
	MemoryRepositoryProvider
	SessionProfileRepositoryProvider
}

type ImportExportHost interface {
	Registrar
	SettingsRepositoryProvider
}

type GitHubSyncHost interface {
	Registrar
	SettingsRepositoryProvider
	MemoryRepositoryProvider
	TaskRepositoryProvider
	TaskGroupRepositoryProvider
	SessionProfileRepositoryProvider
}

type SlackBotHandlerHost interface {
	Registrar
}

type SlackSocketHost interface {
	SessionManagerProvider
	MemoryRepositoryProvider
	SessionProfileRepositoryProvider
}

type SessionAllocatorHost interface {
	SessionManagerProvider
}

type SessionManagerHost interface {
	Registrar
	SessionManagerProvider
}

type MCPHost interface {
	Registrar
	SessionManagerProvider
	ShareRepositoryProvider
	TaskRepositoryProvider
	TaskGroupRepositoryProvider
	MemoryRepositoryProvider
}
