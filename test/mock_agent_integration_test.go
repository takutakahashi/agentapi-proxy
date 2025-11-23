package test

import (
	"context"
	"os"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	services_ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// TestMockAgentIntegration tests mock agent integration with DI container
func TestMockAgentIntegration(t *testing.T) {
	// Test normal mode
	t.Run("Normal Mode", func(t *testing.T) {
		_ = os.Setenv("AGENTAPI_MOCK_MODE", "false")
		defer func() { _ = os.Unsetenv("AGENTAPI_MOCK_MODE") }()

		container := di.NewContainer()
		if _, ok := container.AgentService.(*services.MockAgentService); ok {
			t.Error("Should not use MockAgentService in normal mode")
		}
	})

	// Test mock mode
	t.Run("Mock Mode", func(t *testing.T) {
		_ = os.Setenv("AGENTAPI_MOCK_MODE", "true")
		defer func() { _ = os.Unsetenv("AGENTAPI_MOCK_MODE") }()

		container := di.NewContainer()
		mockService, ok := container.AgentService.(*services.MockAgentService)
		if !ok {
			t.Fatal("Should use MockAgentService in mock mode")
		}

		// Test basic mock functionality
		ctx := context.Background()
		processInfo, err := mockService.StartAgent(ctx, 8100, map[string]string{"TEST": "value"}, nil)
		if err != nil {
			t.Fatalf("Mock StartAgent failed: %v", err)
		}

		// Verify process was created
		if processInfo.PID() < 1000 {
			t.Errorf("Expected mock PID >= 1000, got %d", processInfo.PID())
		}

		// Test status
		status, err := mockService.GetAgentStatus(ctx, processInfo)
		if err != nil {
			t.Fatalf("Mock GetAgentStatus failed: %v", err)
		}

		if status != services_ports.ProcessStatusRunning {
			t.Errorf("Expected status %s, got %s", services_ports.ProcessStatusRunning, status)
		}

		// Test stop
		err = mockService.StopAgent(ctx, processInfo.PID())
		if err != nil {
			t.Fatalf("Mock StopAgent failed: %v", err)
		}
	})
}

// TestMockAgentEnvironmentConfiguration tests environment variable configuration
func TestMockAgentEnvironmentConfiguration(t *testing.T) {
	testCases := []struct {
		name     string
		envVars  map[string]string
		testFunc func(t *testing.T, container *di.Container)
	}{
		{
			name: "Default Mock Configuration",
			envVars: map[string]string{
				"AGENTAPI_MOCK_MODE": "true",
			},
			testFunc: func(t *testing.T, container *di.Container) {
				mockService := container.AgentService.(*services.MockAgentService)
				// Note: we can't access internal config, so just test basic functionality
				ctx := context.Background()
				available, err := mockService.IsPortAvailable(ctx, 8100)
				if err != nil {
					t.Fatalf("IsPortAvailable failed: %v", err)
				}
				if !available {
					t.Error("Port should be available in mock")
				}
			},
		},
		{
			name: "Always Fail Mock Configuration",
			envVars: map[string]string{
				"AGENTAPI_MOCK_MODE":     "true",
				"AGENTAPI_MOCK_BEHAVIOR": "always_fail",
			},
			testFunc: func(t *testing.T, container *di.Container) {
				mockService := container.AgentService.(*services.MockAgentService)

				// Test that operations fail
				ctx := context.Background()
				_, err := mockService.StartAgent(ctx, 8100, nil, nil)
				if err == nil {
					t.Error("Expected StartAgent to fail with always_fail behavior")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tc.envVars {
				_ = os.Setenv(k, v)
			}
			defer func() {
				for k := range tc.envVars {
					_ = os.Unsetenv(k)
				}
			}()

			// Create container and test
			container := di.NewContainer()
			tc.testFunc(t, container)
		})
	}
}

// TestMockAgentSessionLifecycle tests complete session lifecycle with mock
func TestMockAgentSessionLifecycle(t *testing.T) {
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "true")
	defer func() { _ = os.Unsetenv("AGENTAPI_MOCK_MODE") }()

	container := di.NewContainer()
	mockService := container.AgentService.(*services.MockAgentService)

	ctx := context.Background()

	// Create session
	sessionID := entities.SessionID("test-session-123")
	userID := entities.UserID("test-user")
	port := entities.Port(8100)

	config := &services_ports.AgentConfig{
		SessionID:   sessionID,
		UserID:      userID,
		Port:        port,
		Environment: entities.Environment{"TEST_ENV": "mock_test"},
		WorkingDir:  "/tmp",
		Script:      "test-script.sh",
		Args:        []string{"arg1", "arg2"},
	}

	// Start agent
	processInfo, err := mockService.StartAgentWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to start mock agent: %v", err)
	}

	t.Logf("Started mock agent with PID: %d", processInfo.PID())

	// Check status
	status, err := mockService.GetAgentStatus(ctx, processInfo)
	if err != nil {
		t.Fatalf("Failed to get mock agent status: %v", err)
	}

	t.Logf("Mock agent status: %s", status)

	// Check process is tracked
	processes := mockService.GetMockProcesses()
	if len(processes) < 1 {
		t.Errorf("Expected at least 1 tracked process, got %d", len(processes))
	}

	// Test port operations
	available, err := mockService.IsPortAvailable(ctx, port)
	if err != nil {
		t.Fatalf("Failed to check port availability: %v", err)
	}
	if !available {
		t.Error("Port should be available in mock")
	}

	allocatedPort, err := mockService.AllocatePort(ctx)
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	t.Logf("Allocated mock port: %d", allocatedPort)

	// Stop agent
	err = mockService.StopAgent(ctx, processInfo.PID())
	if err != nil {
		t.Fatalf("Failed to stop mock agent: %v", err)
	}

	// Check status after stop
	status, err = mockService.GetAgentStatus(ctx, processInfo)
	if err != nil {
		t.Fatalf("Failed to get mock agent status after stop: %v", err)
	}

	if status != services_ports.ProcessStatusStopped {
		t.Errorf("Expected status %s after stop, got %s", services_ports.ProcessStatusStopped, status)
	}

	t.Logf("Mock agent session lifecycle completed successfully")
}

// TestMockAgentPortManagement tests port-related operations
func TestMockAgentPortManagement(t *testing.T) {
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "true")
	defer func() { _ = os.Unsetenv("AGENTAPI_MOCK_MODE") }()

	container := di.NewContainer()
	mockService := container.AgentService.(*services.MockAgentService)

	ctx := context.Background()

	// Test IsPortAvailable
	available, err := mockService.IsPortAvailable(ctx, 8100)
	if err != nil {
		t.Fatalf("IsPortAvailable failed: %v", err)
	}
	if !available {
		t.Error("Port should always be available in mock")
	}

	// Test GetAvailablePort
	port, err := mockService.GetAvailablePort(ctx, 8100, 8200)
	if err != nil {
		t.Fatalf("GetAvailablePort failed: %v", err)
	}
	if port != 8100 {
		t.Errorf("Expected port 8100, got %d", port)
	}

	// Test AllocatePort
	allocatedPort, err := mockService.AllocatePort(ctx)
	if err != nil {
		t.Fatalf("AllocatePort failed: %v", err)
	}
	if allocatedPort != 8100 {
		t.Errorf("Expected allocated port 8100, got %d", allocatedPort)
	}

	t.Logf("Mock port management tests completed successfully")
}
