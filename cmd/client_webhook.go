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
	Long: `Create, list, get, update, and delete webhooks.

Connection:
  Endpoint is resolved from --endpoint flag or environment variables:
    AGENTAPI_PROXY_SERVICE_HOST=<host>
    AGENTAPI_PROXY_SERVICE_PORT_HTTP=<port>
  Authentication (optional): AGENTAPI_KEY=<api-key>

Typical workflow:
  # 1. List existing webhooks
  agentapi-proxy client webhook list

  # 2. Inspect a specific webhook
  agentapi-proxy client webhook get <id> > webhook.json

  # 3. Edit webhook.json, then apply only the changed fields
  echo '{"status":"paused"}' | agentapi-proxy client webhook apply <id>

  # 4. Delete when no longer needed
  agentapi-proxy client webhook delete <id>`,
}

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhooks",
	Long: `List webhooks and output as JSON array.

Filters (all optional):
  --type    "github" or "custom"
  --status  "active" or "paused"
  --scope   "user" or "team"
  --team-id team identifier (required when --scope=team)

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
  agentapi-proxy client webhook get abc123 > webhook.json
  # Then edit webhook.json and run:
  agentapi-proxy client webhook apply abc123 --file webhook.json`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookGet,
}

var webhookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a webhook from JSON",
	Long: `Create a new webhook from a JSON body.

Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".
The server assigns a unique ID and returns the created resource as JSON.

Required JSON fields vary by webhook type. Example for a GitHub webhook:
  {
    "name": "my-webhook",
    "type": "github",
    "triggers": ["push", "pull_request"],
    "target_session_tags": {"env": "prod"}
  }

Examples:
  # From a file
  agentapi-proxy client webhook create --file webhook.json

  # From stdin
  cat webhook.json | agentapi-proxy client webhook create

  # Inline JSON
  echo '{"name":"my-webhook","type":"github","triggers":["push"]}' \
    | agentapi-proxy client webhook create`,
	Run: runWebhookCreate,
}

var webhookApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a webhook from JSON",
	Long: `Partially update a webhook by sending a JSON patch.

Only the fields present in the JSON body are updated (merge-patch semantics).
Omitted fields are left unchanged on the server.
Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".

Typical workflow:
  1. agentapi-proxy client webhook get <id> > webhook.json
  2. Edit webhook.json (e.g. change status to "paused")
  3. agentapi-proxy client webhook apply <id> --file webhook.json

Examples:
  # Pause a webhook (only status is changed)
  echo '{"status":"paused"}' | agentapi-proxy client webhook apply abc123

  # Apply a full JSON file (unchanged fields are preserved on the server)
  agentapi-proxy client webhook apply abc123 --file webhook.json`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookApply,
}

var webhookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a webhook by ID",
	Long: `Delete a webhook by ID. This action is irreversible.

To find the ID, run:
  agentapi-proxy client webhook list

Examples:
  agentapi-proxy client webhook delete abc123`,
	Args: cobra.ExactArgs(1),
	Run:  runWebhookDelete,
}

var webhookRegenerateSecretCmd = &cobra.Command{
	Use:   "regenerate-secret <id>",
	Short: "Regenerate the secret for a webhook",
	Long: `Regenerate the HMAC secret for a webhook.

The new secret is returned once in the response. Store it immediately —
it cannot be retrieved again after this call.
Update any external service (e.g. GitHub) that uses the current secret.

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
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
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
		fmt.Fprintf(os.Stderr, "Hint: verify the endpoint is reachable and credentials are correct.\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetWebhook(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting webhook %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID is correct with: agentapi-proxy client webhook list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(webhookFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide JSON via stdin or use --file path/to/webhook.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"name\":\"my-webhook\",\"type\":\"github\"}' | agentapi-proxy client webhook create\n")
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.CreateWebhook(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating webhook: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: check the JSON body for required fields and valid values.\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(webhookFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide the patch as JSON via stdin or use --file path/to/patch.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"status\":\"paused\"}' | agentapi-proxy client webhook apply %s\n", args[0])
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.ApplyWebhook(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying webhook %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client webhook list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runWebhookDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteWebhook(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting webhook %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client webhook list\n")
		os.Exit(1)
	}

	fmt.Printf("Webhook %s deleted successfully\n", args[0])
}

func runWebhookRegenerateSecret(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.RegenerateWebhookSecret(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error regenerating secret for webhook %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client webhook list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}
