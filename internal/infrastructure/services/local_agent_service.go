package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// LocalAgentService implements AgentService for local process management
type LocalAgentService struct {
	processes map[int]*LocalProcess
	mu        sync.RWMutex // Mutex to protect against concurrent port allocation
}

// LocalProcess represents a local process
type LocalProcess struct {
	PID       int
	Cmd       *exec.Cmd
	StartedAt time.Time
}

// NewLocalAgentService creates a new LocalAgentService
func NewLocalAgentService() *LocalAgentService {
	return &LocalAgentService{
		processes: make(map[int]*LocalProcess),
	}
}

// StartAgent starts a new agent process with the given configuration
func (s *LocalAgentService) StartAgent(ctx context.Context, config *services.AgentConfig) (*entities.ProcessInfo, error) {
	// Validate configuration
	if err := s.validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Check if port is available
	available, err := s.IsPortAvailable(ctx, config.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to check port availability: %w", err)
	}
	if !available {
		return nil, fmt.Errorf("port %d is not available", config.Port)
	}

	// Build command
	cmd := s.buildCommand(config)

	// Set up environment
	cmd.Env = s.buildEnvironment(config)

	// Set working directory
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	// Create process info
	startedAt := time.Now()
	processInfo := entities.NewProcessInfo(cmd.Process.Pid, startedAt)

	// Store process for management
	s.processes[cmd.Process.Pid] = &LocalProcess{
		PID:       cmd.Process.Pid,
		Cmd:       cmd,
		StartedAt: startedAt,
	}

	return processInfo, nil
}

// StopAgent stops an existing agent process
func (s *LocalAgentService) StopAgent(ctx context.Context, processInfo *entities.ProcessInfo) error {
	pid := processInfo.PID()

	// Find the process
	localProcess, exists := s.processes[pid]
	if !exists {
		// Process might have been started externally, try to stop by PID
		return s.stopProcessByPID(pid)
	}

	// Send SIGTERM for graceful shutdown
	if err := localProcess.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- localProcess.Cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited
		delete(s.processes, pid)
		if err != nil {
			return fmt.Errorf("process exited with error: %w", err)
		}
		return nil
	case <-time.After(10 * time.Second):
		// Timeout, force kill
		return s.KillProcess(ctx, processInfo)
	case <-ctx.Done():
		// Context cancelled
		return ctx.Err()
	}
}

// GetAgentStatus checks the status of an agent process
func (s *LocalAgentService) GetAgentStatus(ctx context.Context, processInfo *entities.ProcessInfo) (services.ProcessStatus, error) {
	pid := processInfo.PID()

	// Check if process exists in our tracking
	if localProcess, exists := s.processes[pid]; exists {
		// Check if process is still running
		if localProcess.Cmd.ProcessState != nil && localProcess.Cmd.ProcessState.Exited() {
			delete(s.processes, pid)
			return services.ProcessStatusStopped, nil
		}
		return services.ProcessStatusRunning, nil
	}

	// Process not in our tracking, check system
	return s.checkSystemProcessStatus(pid)
}

// IsPortAvailable checks if a port is available for use
func (s *LocalAgentService) IsPortAvailable(ctx context.Context, port entities.Port) (bool, error) {
	// Try to listen on the port
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(port)))
	if err != nil {
		// Port is not available
		return false, nil
	}

	// Port is available, close the listener
	_ = ln.Close()
	return true, nil
}

// GetAvailablePort finds an available port within a range
func (s *LocalAgentService) GetAvailablePort(ctx context.Context, startPort, endPort entities.Port) (entities.Port, error) {
	// Use a mutex to prevent race conditions in concurrent port allocation
	s.mu.Lock()
	defer s.mu.Unlock()

	for port := startPort; port <= endPort; port++ {
		available, err := s.IsPortAvailable(ctx, port)
		if err != nil {
			continue
		}
		if available {
			return port, nil
		}
	}

	return 0, errors.New("no available ports in range")
}

// KillProcess forcefully terminates a process
func (s *LocalAgentService) KillProcess(ctx context.Context, processInfo *entities.ProcessInfo) error {
	pid := processInfo.PID()

	// Find the process
	localProcess, exists := s.processes[pid]
	if exists {
		// Kill the process
		if err := localProcess.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}

		// Wait for process to exit
		_ = localProcess.Cmd.Wait()
		delete(s.processes, pid)
		return nil
	}

	// Process not in our tracking, try to kill by PID
	return s.killProcessByPID(pid)
}

// validateConfig validates the agent configuration
func (s *LocalAgentService) validateConfig(config *services.AgentConfig) error {
	if config == nil {
		return errors.New("config cannot be nil")
	}

	if config.SessionID == "" {
		return errors.New("session ID is required")
	}

	if config.UserID == "" {
		return errors.New("user ID is required")
	}

	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}

	return nil
}

// buildCommand builds the command to execute
func (s *LocalAgentService) buildCommand(config *services.AgentConfig) *exec.Cmd {
	// Default to a simple HTTP server for demonstration
	// In a real implementation, this would start the actual agent
	if config.Script != "" {
		args := []string{config.Script}
		args = append(args, config.Args...)
		return exec.Command("sh", args...)
	}

	// Default command: start a simple Python HTTP server
	return exec.Command("python3", "-m", "http.server", strconv.Itoa(int(config.Port)))
}

// buildEnvironment builds the environment variables for the process
func (s *LocalAgentService) buildEnvironment(config *services.AgentConfig) []string {
	env := os.Environ()

	// Add session-specific environment variables
	env = append(env, fmt.Sprintf("SESSION_ID=%s", config.SessionID))
	env = append(env, fmt.Sprintf("USER_ID=%s", config.UserID))
	env = append(env, fmt.Sprintf("PORT=%d", config.Port))

	// Add custom environment variables
	for key, value := range config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// stopProcessByPID stops a process by PID
func (s *LocalAgentService) stopProcessByPID(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	return nil
}

// killProcessByPID kills a process by PID
func (s *LocalAgentService) killProcessByPID(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGKILL
	if err := process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	return nil
}

// checkSystemProcessStatus checks if a process is running at the system level
func (s *LocalAgentService) checkSystemProcessStatus(pid int) (services.ProcessStatus, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return services.ProcessStatusNotFound, nil
	}

	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return services.ProcessStatusNotFound, nil
	}

	// Process exists and is running
	return services.ProcessStatusRunning, nil
}
