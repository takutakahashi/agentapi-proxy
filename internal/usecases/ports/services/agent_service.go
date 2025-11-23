package services

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// AgentService defines the interface for managing agent processes
type AgentService interface {
	// StartAgent starts an agent with simplified parameters
	StartAgent(ctx context.Context, port int, environment map[string]string, repository *entities.Repository) (*entities.ProcessInfo, error)

	// StartAgentWithConfig starts a new agent process with the given configuration
	StartAgentWithConfig(ctx context.Context, config *AgentConfig) (*entities.ProcessInfo, error)

	// StopAgent stops an existing agent process by PID
	StopAgent(ctx context.Context, pid int) error

	// GetAgentStatus checks the status of an agent process
	GetAgentStatus(ctx context.Context, processInfo *entities.ProcessInfo) (ProcessStatus, error)

	// IsPortAvailable checks if a port is available for use
	IsPortAvailable(ctx context.Context, port entities.Port) (bool, error)

	// GetAvailablePort finds an available port within a range
	GetAvailablePort(ctx context.Context, startPort, endPort entities.Port) (entities.Port, error)

	// AllocatePort allocates the next available port
	AllocatePort(ctx context.Context) (int, error)

	// KillProcess forcefully terminates a process
	KillProcess(ctx context.Context, processInfo *entities.ProcessInfo) error
}

// StartAgentParams contains simplified parameters for starting an agent
type StartAgentParams struct {
	Port        int
	Environment map[string]string
	Repository  *entities.Repository
}

// AgentConfig contains configuration for starting an agent
type AgentConfig struct {
	SessionID   entities.SessionID
	UserID      entities.UserID
	Port        entities.Port
	Environment entities.Environment
	WorkingDir  string
	Repository  *entities.Repository
	Script      string
	Args        []string
}

// ProcessStatus represents the status of a process
type ProcessStatus string

const (
	ProcessStatusRunning  ProcessStatus = "running"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusNotFound ProcessStatus = "not_found"
	ProcessStatusZombie   ProcessStatus = "zombie"
)
