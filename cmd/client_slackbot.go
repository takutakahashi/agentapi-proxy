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
	Long:  "Create, list, get, update, and delete SlackBots",
}

var slackbotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SlackBots",
	Long: `List SlackBots and output as JSON.

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

The output can be redirected to a file, edited, and applied back with 'apply'.
Note: bot_token and app_token are write-only and will not appear in the output.

Examples:
  agentapi-proxy client slackbot get abc123
  agentapi-proxy client slackbot get abc123 > slackbot.json`,
	Args: cobra.ExactArgs(1),
	Run:  runSlackBotGet,
}

var slackbotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a SlackBot from JSON",
	Long: `Create a new SlackBot from a JSON body.

Reads JSON from a file (--file) or from stdin if --file is omitted or set to "-".
bot_token (xoxb-...) and app_token (xapp-...) are write-only fields stored securely.

Examples:
  # From a file
  agentapi-proxy client slackbot create --file slackbot.json

  # From stdin
  cat slackbot.json | agentapi-proxy client slackbot create

  # Inline JSON
  echo '{"name":"my-bot","bot_token":"xoxb-...","app_token":"xapp-..."}' | agentapi-proxy client slackbot create`,
	Run: runSlackBotCreate,
}

var slackbotApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a SlackBot from JSON",
	Long: `Partially update a SlackBot by sending a JSON patch.

Only the fields present in the JSON body are updated. Reads JSON from a file
(--file) or from stdin if --file is omitted or set to "-".

Typical workflow:
  1. agentapi-proxy client slackbot get <id> > slackbot.json
  2. Edit slackbot.json (e.g. change status to "paused")
  3. agentapi-proxy client slackbot apply <id> --file slackbot.json

Examples:
  # Pause a SlackBot
  echo '{"status":"paused"}' | agentapi-proxy client slackbot apply abc123

  # Update allowed channels
  echo '{"allowed_channel_names":["general","dev-"]}' | agentapi-proxy client slackbot apply abc123

  # Apply from file
  agentapi-proxy client slackbot apply abc123 --file patch.json`,
	Args: cobra.ExactArgs(1),
	Run:  runSlackBotApply,
}

var slackbotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a SlackBot by ID",
	Long: `Delete a SlackBot by ID.

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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetSlackBot(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting SlackBot: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(slackbotFile)
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

	result, err := c.CreateSlackBot(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating SlackBot: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(slackbotFile)
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

	result, err := c.ApplySlackBot(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying SlackBot: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runSlackBotDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteSlackBot(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting SlackBot: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("SlackBot %s deleted successfully\n", args[0])
}
