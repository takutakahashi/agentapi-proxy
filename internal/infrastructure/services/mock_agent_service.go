package services

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// MockAgentService is a mock implementation of AgentService for testing purposes
type MockAgentService struct {
	processes map[int]*MockProcess
	nextPID   int
	config    *MockConfig
}

// MockProcess represents a mock process
type MockProcess struct {
	PID       int
	StartedAt time.Time
	Config    *services.AgentConfig
	Status    services.ProcessStatus
}

// MockConfig contains configuration for mock behavior
type MockConfig struct {
	Behavior      MockBehavior
	DefaultStatus services.ProcessStatus
	Latency       time.Duration
	FailureRate   float64
}

// MockBehavior defines different mock behaviors
type MockBehavior string

const (
	MockBehaviorNormal     MockBehavior = "normal"
	MockBehaviorAlwaysFail MockBehavior = "always_fail"
	MockBehaviorSlow       MockBehavior = "slow"
)

// NewMockAgentService creates a new MockAgentService
func NewMockAgentService(config *MockConfig) *MockAgentService {
	if config == nil {
		config = &MockConfig{
			Behavior:      MockBehaviorNormal,
			DefaultStatus: services.ProcessStatusRunning,
			Latency:       0,
			FailureRate:   0,
		}
	}

	return &MockAgentService{
		processes: make(map[int]*MockProcess),
		nextPID:   1000, // Start from 1000 for mock PIDs
		config:    config,
	}
}

// StartAgent starts a mock agent with simplified parameters
func (m *MockAgentService) StartAgent(ctx context.Context, port int, environment map[string]string, repository *entities.Repository) (*entities.ProcessInfo, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return nil, err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return nil, fmt.Errorf("mock agent service: always fail mode")
	}

	// Generate mock PID
	pid := m.nextPID
	m.nextPID++

	// Create mock process
	mockProcess := &MockProcess{
		PID:       pid,
		StartedAt: time.Now(),
		Status:    m.config.DefaultStatus,
	}

	// Store process
	m.processes[pid] = mockProcess

	// Create ProcessInfo
	processInfo := entities.NewProcessInfo(pid, mockProcess.StartedAt)
	processInfo.SetCommand("mock-agent", []string{"mock-agent"})

	return processInfo, nil
}

// StartAgentWithConfig starts a mock agent process with the given configuration
func (m *MockAgentService) StartAgentWithConfig(ctx context.Context, config *services.AgentConfig) (*entities.ProcessInfo, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return nil, err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return nil, fmt.Errorf("mock agent service: always fail mode")
	}

	// Generate mock PID
	pid := m.nextPID
	m.nextPID++

	// Create mock process
	mockProcess := &MockProcess{
		PID:       pid,
		StartedAt: time.Now(),
		Config:    config,
		Status:    m.config.DefaultStatus,
	}

	// Store process
	m.processes[pid] = mockProcess

	// Create ProcessInfo
	processInfo := entities.NewProcessInfo(pid, mockProcess.StartedAt)
	processInfo.SetCommand("mock-agent", []string{"mock-agent"})

	return processInfo, nil
}

// StopAgent stops a mock agent process by PID
func (m *MockAgentService) StopAgent(ctx context.Context, pid int) error {
	if err := m.simulateLatency(ctx); err != nil {
		return err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return fmt.Errorf("mock agent service: always fail mode")
	}

	// Find and mark as stopped
	if process, exists := m.processes[pid]; exists {
		process.Status = services.ProcessStatusStopped
		return nil
	}

	return fmt.Errorf("process %d not found", pid)
}

// GetAgentStatus checks the status of a mock agent process
func (m *MockAgentService) GetAgentStatus(ctx context.Context, processInfo *entities.ProcessInfo) (services.ProcessStatus, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return services.ProcessStatusNotFound, err
	}

	pid := processInfo.PID()

	if process, exists := m.processes[pid]; exists {
		return process.Status, nil
	}

	return services.ProcessStatusNotFound, nil
}

// IsPortAvailable always returns true for mock implementation
func (m *MockAgentService) IsPortAvailable(ctx context.Context, port entities.Port) (bool, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return false, err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return false, fmt.Errorf("mock agent service: always fail mode")
	}

	return true, nil
}

// GetAvailablePort returns the requested start port for mock implementation
func (m *MockAgentService) GetAvailablePort(ctx context.Context, startPort, endPort entities.Port) (entities.Port, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return 0, err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return 0, fmt.Errorf("mock agent service: always fail mode")
	}

	return startPort, nil
}

// AllocatePort allocates a mock port
func (m *MockAgentService) AllocatePort(ctx context.Context) (int, error) {
	if err := m.simulateLatency(ctx); err != nil {
		return 0, err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return 0, fmt.Errorf("mock agent service: always fail mode")
	}

	return 8100, nil // Return a fixed port for mock
}

// KillProcess forcefully terminates a mock process
func (m *MockAgentService) KillProcess(ctx context.Context, processInfo *entities.ProcessInfo) error {
	if err := m.simulateLatency(ctx); err != nil {
		return err
	}

	if m.config.Behavior == MockBehaviorAlwaysFail {
		return fmt.Errorf("mock agent service: always fail mode")
	}

	pid := processInfo.PID()

	// Find and remove process
	if process, exists := m.processes[pid]; exists {
		process.Status = services.ProcessStatusStopped
		delete(m.processes, pid)
		return nil
	}

	return fmt.Errorf("process %d not found", pid)
}

// GetMockProcesses returns all mock processes (for testing)
func (m *MockAgentService) GetMockProcesses() map[int]*MockProcess {
	return m.processes
}

// SetMockBehavior updates the mock behavior
func (m *MockAgentService) SetMockBehavior(behavior MockBehavior) {
	m.config.Behavior = behavior
}

// simulateLatency simulates network/processing latency if configured
func (m *MockAgentService) simulateLatency(ctx context.Context) error {
	if m.config.Latency > 0 {
		select {
		case <-time.After(m.config.Latency):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
