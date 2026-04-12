package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

// ACPCmd is the "acp" sub-command.
// It starts a WebSocket server that speaks the Agent Client Protocol (ACP)
// and proxies incoming requests to a locally running claude-agentapi HTTP
// server (e.g. started by the agent-provisioner).
//
// Usage:
//
//	agentapi-proxy acp [--port 9002] [--agentapi-url http://localhost:8080]
//
// The WebSocket endpoint is ws://<host>:<port>/acp.
// A liveness probe is available at GET /healthz.
var ACPCmd = &cobra.Command{
	Use:   "acp",
	Short: "ACP-to-agentapi bridge server",
	Long: `Starts a WebSocket server implementing the Agent Client Protocol (ACP).

ACP clients (e.g. code editors) connect to the WebSocket endpoint and can
interact with the claude-agentapi session via the standardised ACP protocol.

The server translates each ACP method call into the corresponding
claude-agentapi HTTP API call:

  initialize         → capability negotiation (no backend call)
  session/new        → generate a new session UUID (agentapi is single-session)
  session/prompt     → POST /message + stream GET /events as session/update
  session/cancel     → POST /action {type:"stop_agent"}
  session/list       → returns the active session ID

Pending actions (approve_plan / answer_question) surfaced by GET /action are
forwarded to the ACP client as session/request_permission requests; the
client's response is forwarded to POST /action.

Endpoints:
  WS  /acp      – ACP WebSocket endpoint
  GET /healthz  – liveness probe

Reference:
  https://github.com/agentclientprotocol/agent-client-protocol
  https://github.com/agentclientprotocol/claude-agent-acp`,
	RunE: runACP,
}

func init() {
	ACPCmd.Flags().Int("port", 9002,
		"TCP port for the ACP WebSocket server")
	ACPCmd.Flags().String("agentapi-url", "",
		"URL of the claude-agentapi HTTP server "+
			"(default: http://localhost:<AGENTAPI_PORT>, AGENTAPI_PORT defaults to 8080)")
}

func runACP(cmd *cobra.Command, args []string) error {
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}

	agentapiURL, err := cmd.Flags().GetString("agentapi-url")
	if err != nil {
		return err
	}
	if agentapiURL == "" {
		agentapiURL = defaultAgentapiURL()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := acp.NewServer(port, agentapiURL)
	return srv.Start(ctx)
}

// defaultAgentapiURL constructs the default agentapi URL from the AGENTAPI_PORT
// environment variable (falls back to 8080).
func defaultAgentapiURL() string {
	port := os.Getenv("AGENTAPI_PORT")
	if port == "" {
		port = "8080"
	}
	return fmt.Sprintf("http://localhost:%s", port)
}
