package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestK8sModeConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected K8sModeConfig
	}{
		{
			name:    "default config",
			envVars: map[string]string{},
			expected: K8sModeConfig{
				Enabled:   false,
				Namespace: "agentapi-proxy",
				Image:     "agentapi-proxy:latest",
				Resources: K8sResourcesConfig{
					CPURequest:    "100m",
					CPULimit:      "500m",
					MemoryRequest: "256Mi",
					MemoryLimit:   "512Mi",
					StorageSize:   "1Gi",
				},
			},
		},
		{
			name: "k8s mode enabled via env",
			envVars: map[string]string{
				"AGENTAPI_K8S_MODE_ENABLED":   "true",
				"AGENTAPI_K8S_MODE_NAMESPACE": "custom-ns",
				"AGENTAPI_K8S_MODE_IMAGE":     "custom:v1.0",
			},
			expected: K8sModeConfig{
				Enabled:   true,
				Namespace: "custom-ns",
				Image:     "custom:v1.0",
				Resources: K8sResourcesConfig{
					CPURequest:    "100m",
					CPULimit:      "500m",
					MemoryRequest: "256Mi",
					MemoryLimit:   "512Mi",
					StorageSize:   "1Gi",
				},
			},
		},
		{
			name: "custom resource configuration",
			envVars: map[string]string{
				"AGENTAPI_K8S_MODE_ENABLED":                  "true",
				"AGENTAPI_K8S_MODE_RESOURCES_CPU_REQUEST":    "200m",
				"AGENTAPI_K8S_MODE_RESOURCES_CPU_LIMIT":      "1000m",
				"AGENTAPI_K8S_MODE_RESOURCES_MEMORY_REQUEST": "512Mi",
				"AGENTAPI_K8S_MODE_RESOURCES_MEMORY_LIMIT":   "1Gi",
				"AGENTAPI_K8S_MODE_RESOURCES_STORAGE_SIZE":   "2Gi",
			},
			expected: K8sModeConfig{
				Enabled:   true,
				Namespace: "agentapi-proxy",
				Image:     "agentapi-proxy:latest",
				Resources: K8sResourcesConfig{
					CPURequest:    "200m",
					CPULimit:      "1000m",
					MemoryRequest: "512Mi",
					MemoryLimit:   "1Gi",
					StorageSize:   "2Gi",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment first
			clearK8sModeEnv()

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			// Load config
			config, err := LoadConfig("")
			assert.NoError(t, err)

			// Verify k8s mode configuration
			assert.Equal(t, tt.expected.Enabled, config.K8sMode.Enabled)
			assert.Equal(t, tt.expected.Namespace, config.K8sMode.Namespace)
			assert.Equal(t, tt.expected.Image, config.K8sMode.Image)
			assert.Equal(t, tt.expected.Resources, config.K8sMode.Resources)
		})
	}
}

func TestK8sModeConfigJSON(t *testing.T) {
	configJSON := `{
		"k8s_mode": {
			"enabled": true,
			"namespace": "test-namespace",
			"image": "test:v1.0",
			"resources": {
				"cpu_request": "250m",
				"cpu_limit": "750m",
				"memory_request": "384Mi",
				"memory_limit": "768Mi",
				"storage_size": "1.5Gi"
			}
		}
	}`

	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "config*.json")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(configJSON))
	assert.NoError(t, err)
	tmpfile.Close()

	// Load config from file
	config, err := LoadConfig(tmpfile.Name())
	assert.NoError(t, err)

	// Verify k8s mode configuration
	assert.True(t, config.K8sMode.Enabled)
	assert.Equal(t, "test-namespace", config.K8sMode.Namespace)
	assert.Equal(t, "test:v1.0", config.K8sMode.Image)
	assert.Equal(t, "250m", config.K8sMode.Resources.CPURequest)
	assert.Equal(t, "750m", config.K8sMode.Resources.CPULimit)
	assert.Equal(t, "384Mi", config.K8sMode.Resources.MemoryRequest)
	assert.Equal(t, "768Mi", config.K8sMode.Resources.MemoryLimit)
	assert.Equal(t, "1.5Gi", config.K8sMode.Resources.StorageSize)
}

func TestK8sModeConfigYAML(t *testing.T) {
	configYAML := `
k8s_mode:
  enabled: true
  namespace: yaml-namespace
  image: yaml:v2.0
  resources:
    cpu_request: 150m
    cpu_limit: 600m
    memory_request: 320Mi
    memory_limit: 640Mi
    storage_size: 1.2Gi
`

	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(configYAML))
	assert.NoError(t, err)
	tmpfile.Close()

	// Load config from file
	config, err := LoadConfig(tmpfile.Name())
	assert.NoError(t, err)

	// Verify k8s mode configuration
	assert.True(t, config.K8sMode.Enabled)
	assert.Equal(t, "yaml-namespace", config.K8sMode.Namespace)
	assert.Equal(t, "yaml:v2.0", config.K8sMode.Image)
	assert.Equal(t, "150m", config.K8sMode.Resources.CPURequest)
	assert.Equal(t, "600m", config.K8sMode.Resources.CPULimit)
	assert.Equal(t, "320Mi", config.K8sMode.Resources.MemoryRequest)
	assert.Equal(t, "640Mi", config.K8sMode.Resources.MemoryLimit)
	assert.Equal(t, "1.2Gi", config.K8sMode.Resources.StorageSize)
}

func TestK8sModeConfigEnvOverridesFile(t *testing.T) {
	configJSON := `{
		"k8s_mode": {
			"enabled": false,
			"namespace": "file-namespace",
			"image": "file:v1.0"
		}
	}`

	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "config*.json")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(configJSON))
	assert.NoError(t, err)
	tmpfile.Close()

	// Set environment variables that should override file config
	os.Setenv("AGENTAPI_K8S_MODE_ENABLED", "true")
	os.Setenv("AGENTAPI_K8S_MODE_NAMESPACE", "env-namespace")
	defer func() {
		os.Unsetenv("AGENTAPI_K8S_MODE_ENABLED")
		os.Unsetenv("AGENTAPI_K8S_MODE_NAMESPACE")
	}()

	// Load config from file
	config, err := LoadConfig(tmpfile.Name())
	assert.NoError(t, err)

	// Verify environment variables override file config
	assert.True(t, config.K8sMode.Enabled)                     // Overridden by env
	assert.Equal(t, "env-namespace", config.K8sMode.Namespace) // Overridden by env
	assert.Equal(t, "file:v1.0", config.K8sMode.Image)         // From file (not overridden)
}

func clearK8sModeEnv() {
	envVars := []string{
		"AGENTAPI_K8S_MODE_ENABLED",
		"AGENTAPI_K8S_MODE_NAMESPACE",
		"AGENTAPI_K8S_MODE_IMAGE",
		"AGENTAPI_K8S_MODE_RESOURCES_CPU_REQUEST",
		"AGENTAPI_K8S_MODE_RESOURCES_CPU_LIMIT",
		"AGENTAPI_K8S_MODE_RESOURCES_MEMORY_REQUEST",
		"AGENTAPI_K8S_MODE_RESOURCES_MEMORY_LIMIT",
		"AGENTAPI_K8S_MODE_RESOURCES_STORAGE_SIZE",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}
}
