package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/provisioner"
)

// AgentProvisionerCmd is the "agent-provisioner" sub-command.
// It starts an HTTP server (default :9001) that provisions session Pods.
//
// The proxy server calls POST /provision with the session settings JSON
// after the Pod becomes ready.  The provisioner then:
//
//  1. Runs the full setup sequence (write-pem, clone-repo, compile, sync-extra)
//  2. Starts agentapi (or claude-agentapi / codex-agentapi) as a subprocess
//  3. Waits for agentapi to become ready
//  4. Sends the initial message (if any)
//
// On Pod restart, if --settings-file already exists (mounted from the K8s
// Secret), provisioning is triggered automatically without waiting for a
// /provision call.
var AgentProvisionerCmd = &cobra.Command{
	Use:   "agent-provisioner",
	Short: "HTTP provisioner server for session Pods",
	Long: `Starts an HTTP server that provisions a session Pod on demand.

Endpoints:
  GET  /healthz   – liveness/readiness probe (always 200)
  GET  /status    – current provisioning state as JSON
  POST /provision – accepts SessionSettings JSON; triggers the session
                    startup sequence asynchronously (returns 202)

On Pod restart the server automatically provisions from --settings-file
(the K8s Secret volume mount) without waiting for a /provision call.`,
	RunE: runAgentProvisioner,
}

func init() {
	AgentProvisionerCmd.Flags().Int("port", 9001,
		"TCP port for the provisioner HTTP server")
	AgentProvisionerCmd.Flags().String("settings-file",
		"/session-settings/settings.yaml",
		"Path to the session settings YAML file used for auto-provisioning on Pod restart")
}

func runAgentProvisioner(cmd *cobra.Command, args []string) error {
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	settingsFile, err := cmd.Flags().GetString("settings-file")
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := provisioner.New(port, settingsFile)
	return srv.Start(ctx)
}
