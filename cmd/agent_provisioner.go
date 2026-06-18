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
// It starts a local HTTP server (default :9001) for probes/status and pulls
// provision requests from the proxy internal API. The provisioner then:
//
//  1. Runs the full setup sequence (write-pem, clone-repo, compile, sync-extra)
//  2. Starts agentapi (or an ACP bridge) as a subprocess
//  3. Waits for agentapi to become ready
//  4. Sends the initial message (if any)
//
// On Pod restart, if --settings-file already exists (mounted from the K8s
// Secret), provisioning is triggered automatically as Pod restart recovery.
var AgentProvisionerCmd = &cobra.Command{
	Use:   "agent-provisioner",
	Short: "Pull-based provisioner for session Pods",
	Long: `Starts the session Pod provisioner.

Endpoints:
  GET  /healthz   – liveness/readiness probe (always 200)
  GET  /status    – current provisioning state as JSON

Provision requests are pulled from the proxy internal provisioner API.`,
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
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	pullErrCh := make(chan error, 1)
	go func() {
		pullErrCh <- provisioner.RunPullClient(ctx, srv, provisioner.PullClientConfig{
			ProxyURL:  os.Getenv("PROVISIONER_PROXY_URL"),
			Token:     os.Getenv("PROVISIONER_TOKEN"),
			SessionID: os.Getenv("AGENTAPI_SESSION_ID"),
			PodName:   os.Getenv("POD_NAME"),
			Namespace: os.Getenv("POD_NAMESPACE"),
		})
	}()

	select {
	case err := <-errCh:
		return err
	case err := <-pullErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
