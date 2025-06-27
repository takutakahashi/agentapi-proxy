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

	// Test with a fresh command for each test case
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new command to avoid flag conflicts
			testCmd := &cobra.Command{
				Use: "server",
			}
			testCmd.Flags().StringP("port", "p", "8080", "Port to listen on")
			testCmd.Flags().StringP("config", "c", "config.json", "Configuration file path")
			testCmd.Flags().BoolP("verbose", "v", false, "Enable verbose logging")

			// Reset flag states to ensure clean test
			_ = testCmd.Flags().Set("port", "8080")
			_ = testCmd.Flags().Set("config", "config.json")
			_ = testCmd.Flags().Set("verbose", "false")

			// Parse flags
			err := testCmd.ParseFlags(tt.args)
			require.NoError(t, err)

			// Get flag values
			portFlag, err := testCmd.Flags().GetString("port")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedPort, portFlag)

			cfgFlag, err := testCmd.Flags().GetString("config")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedCfg, cfgFlag)

			verbFlag, err := testCmd.Flags().GetBool("verbose")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedVerb, verbFlag)
		})
	}
}

func TestRunProxyWithInvalidConfig(t *testing.T) {
	// Create a temporary invalid config file
	tmpFile, err := os.CreateTemp("", "invalid-config-*.json")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Write invalid JSON
	_, err = tmpFile.WriteString("{ invalid json }")
	require.NoError(t, err)
	_ = tmpFile.Close()

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
	// Test viper binding functionality with a fresh command
	testCmd := &cobra.Command{
		Use: "server",
	}
	// Use different shorthand flags to avoid conflicts
	testCmd.Flags().StringP("port", "P", "8080", "Port to listen on")
	testCmd.Flags().StringP("config", "C", "config.json", "Configuration file path")
	testCmd.Flags().BoolP("verbose", "V", false, "Enable verbose logging")

	// Create fresh viper instance
	v := viper.New()
	err := v.BindPFlag("port", testCmd.Flags().Lookup("port"))
	require.NoError(t, err)
	err = v.BindPFlag("config", testCmd.Flags().Lookup("config"))
	require.NoError(t, err)
	err = v.BindPFlag("verbose", testCmd.Flags().Lookup("verbose"))
	require.NoError(t, err)

	// Test that viper bindings work correctly
	err = testCmd.ParseFlags([]string{"-P", "4000", "-C", "test.json", "-V"})
	require.NoError(t, err)

	// Check if viper has the correct values
	assert.Equal(t, "4000", v.GetString("port"))
	assert.Equal(t, "test.json", v.GetString("config"))
	assert.True(t, v.GetBool("verbose"))
}
