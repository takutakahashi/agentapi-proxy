package di

import (
	"os"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	services_ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func TestNewContainer(t *testing.T) {
	container := NewContainer()

	// Test repositories are initialized
	if container.SessionRepo == nil {
		t.Error("SessionRepo should not be nil")
	}

	if container.UserRepo == nil {
		t.Error("UserRepo should not be nil")
	}

	if container.NotificationRepo == nil {
		t.Error("NotificationRepo should not be nil")
	}

	// Test services are initialized
	if container.AgentService == nil {
		t.Error("AgentService should not be nil")
	}

	if container.AuthService == nil {
		t.Error("AuthService should not be nil")
	}

	if container.NotificationService == nil {
		t.Error("NotificationService should not be nil")
	}

	// Test use cases are initialized
	if container.CreateSessionUC == nil {
		t.Error("CreateSessionUC should not be nil")
	}

	if container.DeleteSessionUC == nil {
		t.Error("DeleteSessionUC should not be nil")
	}

	if container.AuthenticateUserUC == nil {
		t.Error("AuthenticateUserUC should not be nil")
	}

	// Test presenters are initialized
	if container.SessionPresenter == nil {
		t.Error("SessionPresenter should not be nil")
	}

	if container.AuthPresenter == nil {
		t.Error("AuthPresenter should not be nil")
	}

	// Test controllers are initialized
	if container.SessionController == nil {
		t.Error("SessionController should not be nil")
	}

	if container.AuthController == nil {
		t.Error("AuthController should not be nil")
	}

	if container.AuthMiddleware == nil {
		t.Error("AuthMiddleware should not be nil")
	}
}

func TestMockModeDetection(t *testing.T) {
	// Test normal mode
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "false")
	if isMockMode() {
		t.Error("isMockMode should return false when AGENTAPI_MOCK_MODE is false")
	}

	// Test mock mode
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "true")
	if !isMockMode() {
		t.Error("isMockMode should return true when AGENTAPI_MOCK_MODE is true")
	}

	// Clean up
	_ = os.Unsetenv("AGENTAPI_MOCK_MODE")
}

func TestAgentServiceSelection(t *testing.T) {
	// Test normal mode - should use LocalAgentService
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "false")
	container := NewContainer()

	// Check if it's LocalAgentService (not MockAgentService)
	if _, ok := container.AgentService.(*services.MockAgentService); ok {
		t.Error("Should not use MockAgentService in normal mode")
	}

	// Test mock mode - should use MockAgentService
	_ = os.Setenv("AGENTAPI_MOCK_MODE", "true")
	container = NewContainer()

	if _, ok := container.AgentService.(*services.MockAgentService); !ok {
		t.Error("Should use MockAgentService in mock mode")
	}

	// Clean up
	_ = os.Unsetenv("AGENTAPI_MOCK_MODE")
}

func TestLoadMockConfig(t *testing.T) {
	// Test default config
	config := loadMockConfig()
	if config.Behavior != services.MockBehaviorNormal {
		t.Errorf("Expected default behavior to be %s, got %s", services.MockBehaviorNormal, config.Behavior)
	}
	if config.Latency != 0 {
		t.Errorf("Expected default latency to be 0, got %v", config.Latency)
	}

	// Test with environment variables
	_ = os.Setenv("AGENTAPI_MOCK_BEHAVIOR", "always_fail")
	_ = os.Setenv("AGENTAPI_MOCK_LATENCY", "100ms")
	_ = os.Setenv("AGENTAPI_MOCK_FAILURE_RATE", "0.5")

	config = loadMockConfig()
	if config.Behavior != services.MockBehaviorAlwaysFail {
		t.Errorf("Expected behavior to be %s, got %s", services.MockBehaviorAlwaysFail, config.Behavior)
	}
	if config.Latency != 100*time.Millisecond {
		t.Errorf("Expected latency to be 100ms, got %v", config.Latency)
	}
	if config.FailureRate != 0.5 {
		t.Errorf("Expected failure rate to be 0.5, got %f", config.FailureRate)
	}

	// Clean up
	_ = os.Unsetenv("AGENTAPI_MOCK_BEHAVIOR")
	_ = os.Unsetenv("AGENTAPI_MOCK_LATENCY")
	_ = os.Unsetenv("AGENTAPI_MOCK_FAILURE_RATE")
}

func TestLoadMockConfigWithInvalidValues(t *testing.T) {
	// Test with invalid latency
	_ = os.Setenv("AGENTAPI_MOCK_LATENCY", "invalid")
	config := loadMockConfig()
	if config.Latency != 0 {
		t.Errorf("Expected latency to be 0 with invalid value, got %v", config.Latency)
	}

	// Test with invalid failure rate
	_ = os.Setenv("AGENTAPI_MOCK_FAILURE_RATE", "invalid")
	config = loadMockConfig()
	if config.FailureRate != 0 {
		t.Errorf("Expected failure rate to be 0 with invalid value, got %f", config.FailureRate)
	}

	// Clean up
	_ = os.Unsetenv("AGENTAPI_MOCK_LATENCY")
	_ = os.Unsetenv("AGENTAPI_MOCK_FAILURE_RATE")
}

func TestMockConfigEnvironmentVariables(t *testing.T) {
	testCases := []struct {
		name           string
		envVars        map[string]string
		expectedConfig *services.MockConfig
	}{
		{
			name:    "Default config",
			envVars: map[string]string{},
			expectedConfig: &services.MockConfig{
				Behavior:      services.MockBehaviorNormal,
				DefaultStatus: services_ports.ProcessStatusRunning,
				Latency:       0,
				FailureRate:   0,
			},
		},
		{
			name: "Custom behavior",
			envVars: map[string]string{
				"AGENTAPI_MOCK_BEHAVIOR": "slow",
			},
			expectedConfig: &services.MockConfig{
				Behavior:      services.MockBehaviorSlow,
				DefaultStatus: services_ports.ProcessStatusRunning,
				Latency:       0,
				FailureRate:   0,
			},
		},
		{
			name: "Full config",
			envVars: map[string]string{
				"AGENTAPI_MOCK_BEHAVIOR":     "always_fail",
				"AGENTAPI_MOCK_LATENCY":      "200ms",
				"AGENTAPI_MOCK_FAILURE_RATE": "0.8",
			},
			expectedConfig: &services.MockConfig{
				Behavior:      services.MockBehaviorAlwaysFail,
				DefaultStatus: services_ports.ProcessStatusRunning,
				Latency:       200 * time.Millisecond,
				FailureRate:   0.8,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tc.envVars {
				_ = os.Setenv(k, v)
			}

			// Load config
			config := loadMockConfig()

			// Verify config
			if config.Behavior != tc.expectedConfig.Behavior {
				t.Errorf("Expected behavior %s, got %s", tc.expectedConfig.Behavior, config.Behavior)
			}
			if config.Latency != tc.expectedConfig.Latency {
				t.Errorf("Expected latency %v, got %v", tc.expectedConfig.Latency, config.Latency)
			}
			if config.FailureRate != tc.expectedConfig.FailureRate {
				t.Errorf("Expected failure rate %f, got %f", tc.expectedConfig.FailureRate, config.FailureRate)
			}

			// Clean up environment variables
			for k := range tc.envVars {
				_ = os.Unsetenv(k)
			}
		})
	}
}
