package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func TestNewMockAgentService(t *testing.T) {
	// Test with nil config
	mockService := NewMockAgentService(nil)
	if mockService == nil {
		t.Fatal("NewMockAgentService should not return nil")
	}

	if mockService.config.Behavior != MockBehaviorNormal {
		t.Errorf("Expected default behavior to be %s, got %s", MockBehaviorNormal, mockService.config.Behavior)
	}

	// Test with custom config
	config := &MockConfig{
		Behavior:      MockBehaviorSlow,
		DefaultStatus: services.ProcessStatusRunning,
		Latency:       100 * time.Millisecond,
		FailureRate:   0.1,
	}

	mockService = NewMockAgentService(config)
	if mockService.config.Behavior != MockBehaviorSlow {
		t.Errorf("Expected behavior to be %s, got %s", MockBehaviorSlow, mockService.config.Behavior)
	}

	if mockService.config.Latency != 100*time.Millisecond {
		t.Errorf("Expected latency to be %v, got %v", 100*time.Millisecond, mockService.config.Latency)
	}
}

func TestMockAgentService_StartAgent(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	processInfo, err := mockService.StartAgent(ctx, 8100, map[string]string{"TEST": "value"}, nil)
	if err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	if processInfo == nil {
		t.Fatal("ProcessInfo should not be nil")
	}

	if processInfo.PID() < 1000 {
		t.Errorf("Expected PID to be >= 1000, got %d", processInfo.PID())
	}

	// Check if process is stored
	processes := mockService.GetMockProcesses()
	if len(processes) != 1 {
		t.Errorf("Expected 1 process, got %d", len(processes))
	}

	if _, exists := processes[processInfo.PID()]; !exists {
		t.Error("Process should be stored in mock service")
	}
}

func TestMockAgentService_StartAgentWithConfig(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	config := &services.AgentConfig{
		SessionID:   "test-session",
		UserID:      "test-user",
		Port:        8100,
		Environment: map[string]string{"TEST": "value"},
		WorkingDir:  "/tmp",
		Script:      "test-script",
		Args:        []string{"arg1", "arg2"},
	}

	processInfo, err := mockService.StartAgentWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("StartAgentWithConfig failed: %v", err)
	}

	if processInfo == nil {
		t.Fatal("ProcessInfo should not be nil")
	}

	// Check if process is stored with config
	processes := mockService.GetMockProcesses()
	process, exists := processes[processInfo.PID()]
	if !exists {
		t.Fatal("Process should be stored in mock service")
	}

	if process.Config.SessionID != "test-session" {
		t.Errorf("Expected session ID to be 'test-session', got '%s'", process.Config.SessionID)
	}
}

func TestMockAgentService_StopAgent(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	// Start an agent first
	processInfo, err := mockService.StartAgent(ctx, 8100, nil, nil)
	if err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	// Stop the agent
	err = mockService.StopAgent(ctx, processInfo.PID())
	if err != nil {
		t.Fatalf("StopAgent failed: %v", err)
	}

	// Check if process status is updated
	processes := mockService.GetMockProcesses()
	process, exists := processes[processInfo.PID()]
	if !exists {
		t.Fatal("Process should still be stored in mock service")
	}

	if process.Status != services.ProcessStatusStopped {
		t.Errorf("Expected process status to be %s, got %s", services.ProcessStatusStopped, process.Status)
	}

	// Test stopping non-existent process
	err = mockService.StopAgent(ctx, 99999)
	if err == nil {
		t.Error("StopAgent should return error for non-existent process")
	}
}

func TestMockAgentService_GetAgentStatus(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	// Start an agent first
	processInfo, err := mockService.StartAgent(ctx, 8100, nil, nil)
	if err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	// Check status
	status, err := mockService.GetAgentStatus(ctx, processInfo)
	if err != nil {
		t.Fatalf("GetAgentStatus failed: %v", err)
	}

	if status != services.ProcessStatusRunning {
		t.Errorf("Expected status to be %s, got %s", services.ProcessStatusRunning, status)
	}

	// Test non-existent process
	nonExistentProcess := entities.NewProcessInfo(99999, time.Now())
	status, err = mockService.GetAgentStatus(ctx, nonExistentProcess)
	if err != nil {
		t.Fatalf("GetAgentStatus failed: %v", err)
	}

	if status != services.ProcessStatusNotFound {
		t.Errorf("Expected status to be %s, got %s", services.ProcessStatusNotFound, status)
	}
}

func TestMockAgentService_AlwaysFailBehavior(t *testing.T) {
	ctx := context.Background()
	config := &MockConfig{
		Behavior: MockBehaviorAlwaysFail,
	}
	mockService := NewMockAgentService(config)

	// All operations should fail
	_, err := mockService.StartAgent(ctx, 8100, nil, nil)
	if err == nil {
		t.Error("StartAgent should fail with AlwaysFail behavior")
	}

	_, err = mockService.IsPortAvailable(ctx, 8100)
	if err == nil {
		t.Error("IsPortAvailable should fail with AlwaysFail behavior")
	}

	_, err = mockService.AllocatePort(ctx)
	if err == nil {
		t.Error("AllocatePort should fail with AlwaysFail behavior")
	}
}

func TestMockAgentService_SlowBehavior(t *testing.T) {
	ctx := context.Background()
	config := &MockConfig{
		Behavior: MockBehaviorSlow,
		Latency:  50 * time.Millisecond,
	}
	mockService := NewMockAgentService(config)

	start := time.Now()
	_, err := mockService.StartAgent(ctx, 8100, nil, nil)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	if duration < 50*time.Millisecond {
		t.Errorf("Expected operation to take at least 50ms, took %v", duration)
	}
}

func TestMockAgentService_IsPortAvailable(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	available, err := mockService.IsPortAvailable(ctx, 8100)
	if err != nil {
		t.Fatalf("IsPortAvailable failed: %v", err)
	}

	if !available {
		t.Error("Port should be available in mock implementation")
	}
}

func TestMockAgentService_GetAvailablePort(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	port, err := mockService.GetAvailablePort(ctx, 8100, 8200)
	if err != nil {
		t.Fatalf("GetAvailablePort failed: %v", err)
	}

	if port != 8100 {
		t.Errorf("Expected port to be 8100, got %d", port)
	}
}

func TestMockAgentService_AllocatePort(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	port, err := mockService.AllocatePort(ctx)
	if err != nil {
		t.Fatalf("AllocatePort failed: %v", err)
	}

	if port != 8100 {
		t.Errorf("Expected allocated port to be 8100, got %d", port)
	}
}

func TestMockAgentService_KillProcess(t *testing.T) {
	ctx := context.Background()
	mockService := NewMockAgentService(nil)

	// Start an agent first
	processInfo, err := mockService.StartAgent(ctx, 8100, nil, nil)
	if err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	// Kill the process
	err = mockService.KillProcess(ctx, processInfo)
	if err != nil {
		t.Fatalf("KillProcess failed: %v", err)
	}

	// Check if process is removed
	processes := mockService.GetMockProcesses()
	if _, exists := processes[processInfo.PID()]; exists {
		t.Error("Process should be removed after kill")
	}

	// Test killing non-existent process
	nonExistentProcess := entities.NewProcessInfo(99999, time.Now())
	err = mockService.KillProcess(ctx, nonExistentProcess)
	if err == nil {
		t.Error("KillProcess should return error for non-existent process")
	}
}

func TestMockAgentService_SetMockBehavior(t *testing.T) {
	mockService := NewMockAgentService(nil)

	// Initially normal behavior
	if mockService.config.Behavior != MockBehaviorNormal {
		t.Errorf("Expected initial behavior to be %s, got %s", MockBehaviorNormal, mockService.config.Behavior)
	}

	// Change behavior
	mockService.SetMockBehavior(MockBehaviorAlwaysFail)

	if mockService.config.Behavior != MockBehaviorAlwaysFail {
		t.Errorf("Expected behavior to be %s after SetMockBehavior, got %s", MockBehaviorAlwaysFail, mockService.config.Behavior)
	}
}
