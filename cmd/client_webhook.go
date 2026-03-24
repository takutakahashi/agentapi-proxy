package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

// webhook subcommand flags
var (
	webhookFilterType   string
	webhookFilterStatus string
	webhookFilterScope  string
	webhookFilterTeamID string
	webhookFile         string
)

var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Manage webhooks",
	Long:  "Create, list, get, update, and delete webhooks",
}

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhooks",
	Long: `List webhooks and output as JSON.

Examples:
  agentapi-proxy client webhook list
  agentapi-proxy client webhook list --status active
  agentapi-proxy client webhook list --type github --scope team --team-id myorg/myteam`,
	Run: runWebhookList,
}

var webhookGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a webhook by ID",
	Long: `Get a webhook by ID and output as JSON.

The output can be redirected to a file, edited, and applied back with 'apply'.

Examples:
  agentapi-proxy client webhook get abc123
  agentapi-proxy client webhook get abc123 > webhook.json`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookGet,
}

var webhookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a webhook from JSON",
	Long: `Create a new webhook from a JSON body.

Reads JSON from a file (--file) or from stdin if --file is omitted or set to "-".

Examples:
  # From a file
  agentapi-proxy client webhook create --file webhook.json

  # From stdin
  cat webhook.json | agentapi-proxy client webhook create

  # Inline JSON
  echo '{"name":"my-webhook","type":"github","triggers":[]}' | agentapi-proxy client webhook create`,
	Run: runWebhookCreate,
}

var webhookApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a webhook from JSON",
	Long: `Partially update a webhook by sending a JSON patch.

Only the fields present in the JSON body are updated. Reads JSON from a file
(--file) or from stdin if --file is omitted or set to "-".

Typical workflow:
  1. agentapi-proxy client webhook get <id> > webhook.json
  2. Edit webhook.json (e.g. change status to "paused")
  3. agentapi-proxy client webhook apply <id> --file webhook.json

Examples:
  # Pause a webhook
  echo '{"status":"paused"}' | agentapi-proxy client webhook apply abc123

  # Apply from file
  agentapi-proxy client webhook apply abc123 --file patch.json`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookApply,
}

var webhookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a webhook by ID",
	Long: `Delete a webhook by ID.

Examples:
  agentapi-proxy client webhook delete abc123`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookDelete,
}

var webhookRegenerateSecretCmd = &cobra.Command{
	Use:   "regenerate-secret <id>",
	Short: "Regenerate the secret for a webhook",
	Long: `Regenerate the HMAC secret for a webhook. The new secret is shown once.

Examples:
  agentapi-proxy client webhook regenerate-secret abc123`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookRegenerateSecret,
}

func init() {
	// list flags
	webhookListCmd.Flags().StringVar(&webhookFilterType, "type", "", `Filter by webhook type: "github" or "custom"`)
	webhookListCmd.Flags().StringVar(&webhookFilterStatus, "status", "", `Filter by status: "active" or "paused"`)
	webhookListCmd.Flags().StringVar(&webhookFilterScope, "scope", "", `Filter by scope: "user" or "team"`)
	webhookListCmd.Flags().StringVar(&webhookFilterTeamID, "team-id", "", "Filter by team ID")

	// create / apply flags
	webhookCreateCmd.Flags().StringVarP(&webhookFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)
	webhookApplyCmd.Flags().StringVarP(&webhookFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)

	webhookCmd.AddCommand(webhookListCmd)
	webhookCmd.AddCommand(webhookGetCmd)
	webhookCmd.AddCommand(webhookCreateCmd)
	webhookCmd.AddCommand(webhookApplyCmd)
	webhookCmd.AddCommand(webhookDeleteCmd)
	webhookCmd.AddCommand(webhookRegenerateSecretCmd)

	ClientCmd.AddCommand(webhookCmd)
}

func runWebhookList(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	opts := &client.ListWebhooksOptions{
		Type:   webhookFilterType,
		Status: webhookFilterStatus,
		Scope:  webhookFilterScope,
		TeamID: webhookFilterTeamID,
	}

	result, err := c.ListWebhooks(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing webhooks: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetWebhook(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting webhook: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(webhookFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.CreateWebhook(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating webhook: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(webhookFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.ApplyWebhook(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying webhook: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteWebhook(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting webhook: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Webhook %s deleted successfully\n", args[0])
}

func runWebhookRegenerateSecret(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.RegenerateWebhookSecret(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error regenerating secret: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}
