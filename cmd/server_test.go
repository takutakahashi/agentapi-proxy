package cmd

import (
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerCmd(t *testing.T) {
	// Test that the command is properly configured
	assert.Equal(t, "server", ServerCmd.Use)
	assert.Equal(t, "Start the AgentAPI Proxy Server", ServerCmd.Short)
	assert.NotNil(t, ServerCmd.Run)
}

func TestServerCmdFlags(t *testing.T) {
	// Test flags are properly configured

	tests := []struct {
		name         string
		args         []string
		expectedPort string
		expectedCfg  string
		expectedVerb bool
	}{
		{
			name:         "default values",
			args:         []string{},
			expectedPort: "8080",
			expectedCfg:  "config.json",
			expectedVerb: false,
		},
		{
			name:         "custom port",
			args:         []string{"-p", "9090"},
			expectedPort: "9090",
			expectedCfg:  "config.json",
			expectedVerb: false,
		},
		{
			name:         "custom config",
			args:         []string{"-c", "custom.json"},
			expectedPort: "8080",
			expectedCfg:  "custom.json",
			expectedVerb: false,
		},
		{
			name:         "verbose mode",
			args:         []string{"-v"},
			expectedPort: "8080",
			expectedCfg:  "config.json",
			expectedVerb: true,
		},
		{
			name:         "all flags",
			args:         []string{"-p", "3000", "-c", "test.json", "-v"},
			expectedPort: "3000",
			expectedCfg:  "test.json",
			expectedVerb: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper for each test
			viper.Reset()

			// Parse flags
			err := ServerCmd.ParseFlags(tt.args)
			require.NoError(t, err)

			// Get flag values
			portFlag, err := ServerCmd.Flags().GetString("port")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedPort, portFlag)

			cfgFlag, err := ServerCmd.Flags().GetString("config")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedCfg, cfgFlag)

			verbFlag, err := ServerCmd.Flags().GetBool("verbose")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedVerb, verbFlag)
		})
	}
}

func TestRunProxyWithInvalidConfig(t *testing.T) {
	// Create a temporary invalid config file
	tmpFile, err := os.CreateTemp("", "invalid-config-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Write invalid JSON
	_, err = tmpFile.WriteString("{ invalid json }")
	require.NoError(t, err)
	tmpFile.Close()

	// Set the config flag to the invalid file
	cfg = tmpFile.Name()
	port = "0" // Use port 0 to let the system assign a port
	verbose = true

	// Create a test command
	testCmd := &cobra.Command{}

	// Start the proxy in a goroutine
	done := make(chan bool)
	go func() {
		// This should use default config when the file is invalid
		runProxy(testCmd, []string{})
		done <- true
	}()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	// Send interrupt signal to trigger shutdown
	process, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	err = process.Signal(os.Interrupt)
	require.NoError(t, err)

	// Wait for shutdown with timeout
	select {
	case <-done:
		// Server shut down successfully
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down in time")
	}
}

func TestRunProxyGracefulShutdown(t *testing.T) {
	// Use a valid config or create a temporary one
	cfg = "config.json"
	port = "0" // Use port 0 to let the system assign a port
	verbose = false

	// Create a test command
	testCmd := &cobra.Command{}

	// Start the proxy in a goroutine
	serverStarted := make(chan bool)
	done := make(chan bool)

	go func() {
		// Override the server startup to signal when ready
		go func() {
			time.Sleep(50 * time.Millisecond)
			serverStarted <- true
		}()

		runProxy(testCmd, []string{})
		done <- true
	}()

	// Wait for server to start
	select {
	case <-serverStarted:
		// Server started
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not start in time")
	}

	// Give the server a bit more time to fully initialize
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM to trigger graceful shutdown
	process, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	err = process.Signal(os.Interrupt)
	require.NoError(t, err)

	// Wait for shutdown
	select {
	case <-done:
		// Server shut down successfully
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down gracefully")
	}
}

func TestViperBindings(t *testing.T) {
	// Reset everything before testing
	viper.Reset()
	ServerCmd.ResetFlags()

	// Flags are already initialized

	// Test that viper bindings work correctly
	err := ServerCmd.ParseFlags([]string{"-p", "4000", "-c", "test.json", "-v"})
	require.NoError(t, err)

	// Check if viper has the correct values
	assert.Equal(t, "4000", viper.GetString("port"))
	assert.Equal(t, "test.json", viper.GetString("config"))
	assert.True(t, viper.GetBool("verbose"))
}
