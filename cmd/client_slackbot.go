package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

// slackbot subcommand flags
var (
	slackbotFilterStatus string
	slackbotFilterScope  string
	slackbotFilterTeamID string
	slackbotFile         string
)

var slackbotCmd = &cobra.Command{
	Use:   "slackbot",
	Short: "Manage SlackBots",
	Long: `Create, list, get, update, and delete SlackBots.

Connection:
  Endpoint is resolved from --endpoint flag or environment variables:
    AGENTAPI_PROXY_SERVICE_HOST=<host>
    AGENTAPI_PROXY_SERVICE_PORT_HTTP=<port>
  Authentication (optional): AGENTAPI_KEY=<api-key>

Note: bot_token and app_token are write-only — they are stored securely on
the server and will not appear in get/list responses.

Typical workflow:
  # 1. Create a bot with tokens
  echo '{"name":"my-bot","bot_token":"xoxb-...","app_token":"xapp-..."}' \
    | agentapi-proxy client slackbot create

  # 2. List existing bots
  agentapi-proxy client slackbot list

  # 3. Inspect a specific bot
  agentapi-proxy client slackbot get <id> > slackbot.json

  # 4. Edit slackbot.json, then apply only the changed fields
  echo '{"status":"paused"}' | agentapi-proxy client slackbot apply <id>

  # 5. Delete when no longer needed
  agentapi-proxy client slackbot delete <id>`,
}

var slackbotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SlackBots",
	Long: `List SlackBots and output as JSON array.

Filters (all optional):
  --status  "active" or "paused"
  --scope   "user" or "team"
  --team-id team identifier (required when --scope=team)

Examples:
  agentapi-proxy client slackbot list
  agentapi-proxy client slackbot list --status active
  agentapi-proxy client slackbot list --scope team --team-id myorg/myteam`,
	Run: runSlackBotList,
}

var slackbotGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a SlackBot by ID",
	Long: `Get a SlackBot by ID and output as JSON.

Note: bot_token and app_token are write-only and will not appear in the output.
The output can be redirected to a file, edited, and applied back with 'apply'.

Examples:
  agentapi-proxy client slackbot get abc123
  agentapi-proxy client slackbot get abc123 > slackbot.json
  # Then edit slackbot.json and run:
  agentapi-proxy client slackbot apply abc123 --file slackbot.json`,
	Args: cobra.ExactArgs(1),
	Run:  runSlackBotGet,
}

var slackbotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a SlackBot from JSON",
	Long: `Create a new SlackBot from a JSON body.

Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".
The server assigns a unique ID and returns the created resource as JSON.

Required tokens:
  bot_token  Slack Bot token starting with "xoxb-"
  app_token  Slack App-level token starting with "xapp-"

Both tokens are stored securely and will not be returned in future responses.

Example JSON:
  {
    "name": "my-bot",
    "bot_token": "xoxb-...",
    "app_token": "xapp-...",
    "allowed_channel_names": ["general", "dev-alerts"],
    "target_session_tags": {"env": "prod"}
  }

Examples:
  # From a file
  agentapi-proxy client slackbot create --file slackbot.json

  # From stdin
  cat slackbot.json | agentapi-proxy client slackbot create

  # Inline JSON
  echo '{"name":"my-bot","bot_token":"xoxb-...","app_token":"xapp-..."}' \
    | agentapi-proxy client slackbot create`,
	Run: runSlackBotCreate,
}

var slackbotApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a SlackBot from JSON",
	Long: `Partially update a SlackBot by sending a JSON patch.

Only the fields present in the JSON body are updated (merge-patch semantics).
Omitted fields are left unchanged on the server.
Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".

You can rotate tokens by including bot_token and/or app_token in the patch.

Typical workflow:
  1. agentapi-proxy client slackbot get <id> > slackbot.json
  2. Edit slackbot.json (e.g. change status to "paused")
  3. agentapi-proxy client slackbot apply <id> --file slackbot.json

Examples:
  # Pause a SlackBot (only status is changed)
  echo '{"status":"paused"}' | agentapi-proxy client slackbot apply abc123

  # Update allowed channels only
  echo '{"allowed_channel_names":["general","dev-alerts"]}' \
    | agentapi-proxy client slackbot apply abc123

  # Rotate the bot token
  echo '{"bot_token":"xoxb-new-token"}' | agentapi-proxy client slackbot apply abc123

  # Apply a full JSON file (unchanged fields are preserved on the server)
  agentapi-proxy client slackbot apply abc123 --file slackbot.json`,
	Args: cobra.ExactArgs(1),
	Run:  runSlackBotApply,
}

var slackbotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a SlackBot by ID",
	Long: `Delete a SlackBot by ID. This action is irreversible.

To find the ID, run:
  agentapi-proxy client slackbot list

Examples:
  agentapi-proxy client slackbot delete abc123`,
	Args: cobra.ExactArgs(1),
	Run:  runSlackBotDelete,
}

func init() {
	// list flags
	slackbotListCmd.Flags().StringVar(&slackbotFilterStatus, "status", "", `Filter by status: "active" or "paused"`)
	slackbotListCmd.Flags().StringVar(&slackbotFilterScope, "scope", "", `Filter by scope: "user" or "team"`)
	slackbotListCmd.Flags().StringVar(&slackbotFilterTeamID, "team-id", "", "Filter by team ID")

	// create / apply flags
	slackbotCreateCmd.Flags().StringVarP(&slackbotFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)
	slackbotApplyCmd.Flags().StringVarP(&slackbotFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)

	slackbotCmd.AddCommand(slackbotListCmd)
	slackbotCmd.AddCommand(slackbotGetCmd)
	slackbotCmd.AddCommand(slackbotCreateCmd)
	slackbotCmd.AddCommand(slackbotApplyCmd)
	slackbotCmd.AddCommand(slackbotDeleteCmd)

	ClientCmd.AddCommand(slackbotCmd)
}

func runSlackBotList(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()
	opts := &client.ListSlackBotsOptions{
		Status: slackbotFilterStatus,
		Scope:  slackbotFilterScope,
		TeamID: slackbotFilterTeamID,
	}

	result, err := c.ListSlackBots(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing SlackBots: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: verify the endpoint is reachable and credentials are correct.\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetSlackBot(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting SlackBot %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID is correct with: agentapi-proxy client slackbot list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(slackbotFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide JSON via stdin or use --file path/to/slackbot.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"name\":\"my-bot\",\"bot_token\":\"xoxb-...\",\"app_token\":\"xapp-...\"}' | agentapi-proxy client slackbot create\n")
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.CreateSlackBot(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating SlackBot: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: check the JSON body includes required fields: name, bot_token (xoxb-...), app_token (xapp-...).\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(slackbotFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide the patch as JSON via stdin or use --file path/to/patch.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"status\":\"paused\"}' | agentapi-proxy client slackbot apply %s\n", args[0])
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.ApplySlackBot(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying SlackBot %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client slackbot list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteSlackBot(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting SlackBot %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client slackbot list\n")
		os.Exit(1)
	}

	fmt.Printf("SlackBot %s deleted successfully\n", args[0])
}
