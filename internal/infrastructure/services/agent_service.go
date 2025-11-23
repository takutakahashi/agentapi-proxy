package services

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// AgentServiceImpl implements the AgentService interface
type AgentServiceImpl struct {
	basePort    int
	maxPort     int
	currentPort int
}

// NewAgentService creates a new AgentService implementation
func NewAgentService(basePort, maxPort int) services.AgentService {
	return &AgentServiceImpl{
		basePort:    basePort,
		maxPort:     maxPort,
		currentPort: basePort,
	}
}

// StartAgent starts an agent with simplified parameters
func (s *AgentServiceImpl) StartAgent(ctx context.Context, port int, environment map[string]string, repository *entities.Repository) (*entities.ProcessInfo, error) {
	// Create a simple command - in a real implementation, this would be more sophisticated
	cmd := exec.CommandContext(ctx, "sleep", "3600") // Placeholder command

	// Set environment variables
	env := os.Environ()
	for k, v := range environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	// Set working directory if repository is provided
	if repository != nil {
		// In a real implementation, this would clone the repository first
		// For now, just use a placeholder directory
		cmd.Dir = "/tmp/" + repository.Name()
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start agent process: %w", err)
	}

	// Create ProcessInfo
	processInfo := entities.NewProcessInfo(cmd.Process.Pid, time.Now())
	processInfo.SetCommand(cmd.Path, cmd.Args)

	return processInfo, nil
}

// StartAgentWithConfig starts a new agent process with the given configuration
func (s *AgentServiceImpl) StartAgentWithConfig(ctx context.Context, config *services.AgentConfig) (*entities.ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, config.Script, config.Args...)

	// Set environment variables
	env := os.Environ()
	for k, v := range config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	// Set working directory
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start agent process: %w", err)
	}

	// Create ProcessInfo
	processInfo := entities.NewProcessInfo(cmd.Process.Pid, time.Now())
	processInfo.SetCommand(cmd.Path, cmd.Args)

	return processInfo, nil
}

// StopAgent stops an existing agent process by PID
func (s *AgentServiceImpl) StopAgent(ctx context.Context, pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGTERM first
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	// Wait a bit for graceful shutdown
	time.Sleep(5 * time.Second)

	// Check if process is still running
	if err := process.Signal(syscall.Signal(0)); err == nil {
		// Process is still running, send SIGKILL
		if err := process.Signal(syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
		}
	}

	return nil
}

// GetAgentStatus checks the status of an agent process
func (s *AgentServiceImpl) GetAgentStatus(ctx context.Context, processInfo *entities.ProcessInfo) (services.ProcessStatus, error) {
	process, err := os.FindProcess(processInfo.PID())
	if err != nil {
		return services.ProcessStatusNotFound, nil
	}

	// Check if process is still running by sending signal 0
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return services.ProcessStatusStopped, nil
	}

	return services.ProcessStatusRunning, nil
}

// IsPortAvailable checks if a port is available for use
func (s *AgentServiceImpl) IsPortAvailable(ctx context.Context, port entities.Port) (bool, error) {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return false, nil
	}
	defer ln.Close()
	return true, nil
}

// GetAvailablePort finds an available port within a range
func (s *AgentServiceImpl) GetAvailablePort(ctx context.Context, startPort, endPort entities.Port) (entities.Port, error) {
	for port := startPort; port <= endPort; port++ {
		available, err := s.IsPortAvailable(ctx, port)
		if err != nil {
			continue
		}
		if available {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found in range %d-%d", startPort, endPort)
}

// AllocatePort allocates the next available port
func (s *AgentServiceImpl) AllocatePort(ctx context.Context) (int, error) {
	for i := 0; i < (s.maxPort - s.basePort + 1); i++ {
		port := s.currentPort
		s.currentPort++
		if s.currentPort > s.maxPort {
			s.currentPort = s.basePort
		}

		available, err := s.IsPortAvailable(ctx, entities.Port(port))
		if err != nil {
			continue
		}
		if available {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found")
}

// KillProcess forcefully terminates a process
func (s *AgentServiceImpl) KillProcess(ctx context.Context, processInfo *entities.ProcessInfo) error {
	process, err := os.FindProcess(processInfo.PID())
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", processInfo.PID(), err)
	}

	if err := process.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("failed to kill process %d: %w", processInfo.PID(), err)
	}

	return nil
}
