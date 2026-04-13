package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp/bridge"
)

var (
	acpPort      string
	acpCwd       string
	acpSessionID string
	acpVerbose   bool
)

// AcpServerCmd starts an ACP agent over stdio and exposes it as an
// agentapi-compatible HTTP server (compatible with takutakahashi/claude-agentapi).
var AcpServerCmd = &cobra.Command{
	Use:   "acp-server [flags] -- <agent-command> [agent-args...]",
	Short: "Run an ACP agent over stdio and expose it as an agentapi-compatible HTTP server",
	Long: `acp-server launches an ACP-compatible agent as a subprocess via stdio (stdin/stdout)
and exposes its interface as an agentapi-compatible HTTP API.

The HTTP interface is compatible with takutakahashi/claude-agentapi and coder/agentapi.

Endpoints exposed:
  GET  /health    - health check
  GET  /status    - agent status (running|stable)
  GET  /messages  - conversation history
  POST /message   - send a user message
  GET  /events    - SSE event stream (message_update, status_change, agent_error)
  GET  /action    - list pending actions (permission requests, plan approvals)
  POST /action    - submit a response to a pending action

Example:
  agentapi-proxy acp-server --port 3284 -- my-acp-agent --model gpt-4`,
	Args: cobra.ArbitraryArgs,
	RunE: runAcpServer,
}

func init() {
	AcpServerCmd.Flags().StringVarP(&acpPort, "port", "p", "3284", "HTTP port to listen on")
	AcpServerCmd.Flags().StringVar(&acpCwd, "cwd", "", "Working directory for the ACP session (defaults to current directory)")
	AcpServerCmd.Flags().StringVar(&acpSessionID, "session-id", "", "Session ID to use (defaults to auto-generated)")
	AcpServerCmd.Flags().BoolVarP(&acpVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runAcpServer(cmd *cobra.Command, args []string) error {
	if acpVerbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	// Parse agent command from args (everything after "--" separator or all args).
	agentArgs, err := parseACPAgentArgs(cmd, args)
	if err != nil {
		return err
	}
	if len(agentArgs) == 0 {
		return fmt.Errorf("no agent command specified; usage: acp-server [flags] -- <agent-command> [args...]")
	}

	// Resolve working directory.
	cwd := acpCwd
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Set up root context that cancels on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[acp-server] starting agent: %v", agentArgs)

	// Launch the ACP agent subprocess.
	proc := exec.CommandContext(ctx, agentArgs[0], agentArgs[1:]...)
	proc.Stderr = os.Stderr

	agentStdin, err := proc.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	agentStdout, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := proc.Start(); err != nil {
		return fmt.Errorf("failed to start agent %q: %w", agentArgs[0], err)
	}
	log.Printf("[acp-server] agent started (pid=%d)", proc.Process.Pid)

	// Create ACP client and wire it to the subprocess pipes.
	acpClient := acp.NewClient(agentStdout, agentStdin, acpVerbose)

	// Start the JSON-RPC read loop.
	listenErrCh := make(chan error, 1)
	go func() {
		listenErrCh <- acpClient.Listen(ctx)
	}()

	// Perform the ACP handshake.
	if err := acpClient.Initialize(ctx); err != nil {
		return fmt.Errorf("ACP initialization failed: %w", err)
	}

	// Create the ACP session.
	if err := acpClient.NewSession(ctx, cwd, nil); err != nil {
		return fmt.Errorf("ACP session creation failed: %w", err)
	}
	log.Printf("[acp-server] ACP session ready (session=%s)", acpClient.SessionID())

	// Create the bridge and start its event loop.
	b := bridge.New(acpClient, acpVerbose)
	go b.Run(ctx)

	// Start the HTTP server.
	srv := bridge.NewServer(b, acpVerbose)
	addr := ":" + acpPort
	log.Printf("[acp-server] HTTP server listening on %s", addr)

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- srv.Start(ctx, addr)
	}()

	// Watch for the process exiting unexpectedly.
	procDoneCh := make(chan error, 1)
	go func() {
		procDoneCh <- proc.Wait()
	}()

	select {
	case <-ctx.Done():
		log.Printf("[acp-server] shutting down")
		return nil

	case err := <-httpErrCh:
		if err != nil {
			return fmt.Errorf("HTTP server error: %w", err)
		}
		return nil

	case err := <-listenErrCh:
		if err != nil && ctx.Err() == nil {
			log.Printf("[acp-server] JSON-RPC listener closed: %v", err)
		}
		return nil

	case err := <-procDoneCh:
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("agent process exited unexpectedly: %w", err)
		}
		return nil
	}
}

// parseACPAgentArgs extracts the agent command from cobra args.
// Cobra collects everything after "--" in ArgsLenAtDash() and args slice.
func parseACPAgentArgs(cmd *cobra.Command, args []string) ([]string, error) {
	dashIdx := cmd.ArgsLenAtDash()
	if dashIdx >= 0 {
		// User used "--" separator; everything after is the agent command.
		return args[dashIdx:], nil
	}
	// No "--": all positional args are the agent command.
	return args, nil
}
