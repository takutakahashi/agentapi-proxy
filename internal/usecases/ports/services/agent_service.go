package services

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// AgentService defines the interface for managing agent processes
type AgentService interface {
	// StartAgent starts a new agent process with the given configuration
	StartAgent(ctx context.Context, config *AgentConfig) (*entities.ProcessInfo, error)

	// StopAgent stops an existing agent process
	StopAgent(ctx context.Context, processInfo *entities.ProcessInfo) error

	// GetAgentStatus checks the status of an agent process
	GetAgentStatus(ctx context.Context, processInfo *entities.ProcessInfo) (ProcessStatus, error)

	// IsPortAvailable checks if a port is available for use
	IsPortAvailable(ctx context.Context, port entities.Port) (bool, error)

	// GetAvailablePort finds an available port within a range
	GetAvailablePort(ctx context.Context, startPort, endPort entities.Port) (entities.Port, error)

	// KillProcess forcefully terminates a process
	KillProcess(ctx context.Context, processInfo *entities.ProcessInfo) error
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
	Message     string // Initial message for the agent
}

// ProcessStatus represents the status of a process
type ProcessStatus string

const (
	ProcessStatusRunning  ProcessStatus = "running"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusNotFound ProcessStatus = "not_found"
	ProcessStatusZombie   ProcessStatus = "zombie"
)
