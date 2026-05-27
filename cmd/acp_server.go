package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp/bridge"
)

var (
	acpPort        string
	acpCwd         string
	acpSessionID   string
	acpSessionFile string
	acpOutputFile  string
	acpVerbose     bool
	acpAutoApprove bool
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
	AcpServerCmd.Flags().StringVar(&acpSessionFile, "session-file", "", "File to persist ACP session ID for reuse across restarts (defaults to {cwd}/.acp-session-id)")
	AcpServerCmd.Flags().StringVar(&acpOutputFile, "output-file", "", "File to append conversation history in acp-posts JSONL format (for Slack integration)")
	AcpServerCmd.Flags().BoolVarP(&acpVerbose, "verbose", "v", false, "Enable verbose logging")
	AcpServerCmd.Flags().BoolVar(&acpAutoApprove, "auto-approve", false, "Automatically approve all permission requests without showing a UI modal")
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

	// Resolve session file path (used to persist the session ID across restarts).
	sessionFile := acpSessionFile
	if sessionFile == "" {
		sessionFile = filepath.Join(cwd, ".acp-session-id")
	}

	// Try to restore a previous session if the agent supports session/load.
	restored := false
	if acpClient.AgentCaps().SessionLoad {
		if data, err := os.ReadFile(sessionFile); err == nil {
			savedID := strings.TrimSpace(string(data))
			if savedID != "" {
				if err := acpClient.LoadSession(ctx, savedID); err != nil {
					log.Printf("[acp-server] session/load failed (%v), creating new session", err)
				} else {
					log.Printf("[acp-server] restored previous session (session=%s)", acpClient.SessionID())
					restored = true
				}
			}
		}
	}

	if !restored {
		// Create a fresh ACP session.
		if err := acpClient.NewSession(ctx, cwd, nil); err != nil {
			return fmt.Errorf("ACP session creation failed: %w", err)
		}
		// Persist the new session ID so future restarts can attempt to restore it.
		if err := os.WriteFile(sessionFile, []byte(acpClient.SessionID()), 0600); err != nil {
			log.Printf("[acp-server] warning: failed to save session ID to %s: %v", sessionFile, err)
		} else {
			log.Printf("[acp-server] session ID saved to %s", sessionFile)
		}
	}

	log.Printf("[acp-server] ACP session ready (session=%s)", acpClient.SessionID())

	// Create the bridge and start its event loop.
	b := bridge.New(acpClient, acpClient.SessionID(), acpVerbose, acpOutputFile, acpAutoApprove)
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
