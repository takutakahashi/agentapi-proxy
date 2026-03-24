package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

// schedule subcommand flags
var (
	scheduleFilterStatus string
	scheduleFilterScope  string
	scheduleFilterTeamID string
	scheduleFile         string
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage schedules",
	Long:  "Create, list, get, update, and delete schedules",
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schedules",
	Long: `List schedules and output as JSON.

Examples:
  agentapi-proxy client schedule list
  agentapi-proxy client schedule list --status active
  agentapi-proxy client schedule list --scope team --team-id myorg/myteam`,
	Run: runScheduleList,
}

var scheduleGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a schedule by ID",
	Long: `Get a schedule by ID and output as JSON.

The output can be redirected to a file, edited, and applied back with 'apply'.

Examples:
  agentapi-proxy client schedule get abc123
  agentapi-proxy client schedule get abc123 > schedule.json`,
	Args: cobra.ExactArgs(1),
	Run:  runScheduleGet,
}

var scheduleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a schedule from JSON",
	Long: `Create a new schedule from a JSON body.

Reads JSON from a file (--file) or from stdin if --file is omitted or set to "-".

Examples:
  # One-time schedule
  echo '{"name":"daily-report","scheduled_at":"2026-03-25T09:00:00Z"}' | agentapi-proxy client schedule create

  # Recurring schedule (cron)
  cat schedule.json | agentapi-proxy client schedule create

  # From file
  agentapi-proxy client schedule create --file schedule.json`,
	Run: runScheduleCreate,
}

var scheduleApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a schedule from JSON",
	Long: `Partially update a schedule by sending a JSON patch.

Only the fields present in the JSON body are updated. Reads JSON from a file
(--file) or from stdin if --file is omitted or set to "-".

Typical workflow:
  1. agentapi-proxy client schedule get <id> > schedule.json
  2. Edit schedule.json (e.g. change status to "paused")
  3. agentapi-proxy client schedule apply <id> --file schedule.json

Examples:
  # Pause a schedule
  echo '{"status":"paused"}' | agentapi-proxy client schedule apply abc123

  # Update cron expression
  echo '{"cron_expr":"0 10 * * 1-5"}' | agentapi-proxy client schedule apply abc123

  # Apply from file
  agentapi-proxy client schedule apply abc123 --file patch.json`,
	Args: cobra.ExactArgs(1),
	Run:  runScheduleApply,
}

var scheduleDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a schedule by ID",
	Long: `Delete a schedule by ID.

Examples:
  agentapi-proxy client schedule delete abc123`,
	Args: cobra.ExactArgs(1),
	Run:  runScheduleDelete,
}

func init() {
	// list flags
	scheduleListCmd.Flags().StringVar(&scheduleFilterStatus, "status", "", `Filter by status: "active", "paused", or "completed"`)
	scheduleListCmd.Flags().StringVar(&scheduleFilterScope, "scope", "", `Filter by scope: "user" or "team"`)
	scheduleListCmd.Flags().StringVar(&scheduleFilterTeamID, "team-id", "", "Filter by team ID")

	// create / apply flags
	scheduleCreateCmd.Flags().StringVarP(&scheduleFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)
	scheduleApplyCmd.Flags().StringVarP(&scheduleFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)

	scheduleCmd.AddCommand(scheduleListCmd)
	scheduleCmd.AddCommand(scheduleGetCmd)
	scheduleCmd.AddCommand(scheduleCreateCmd)
	scheduleCmd.AddCommand(scheduleApplyCmd)
	scheduleCmd.AddCommand(scheduleDeleteCmd)

	ClientCmd.AddCommand(scheduleCmd)
}

func runScheduleList(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	opts := &client.ListSchedulesOptions{
		Status: scheduleFilterStatus,
		Scope:  scheduleFilterScope,
		TeamID: scheduleFilterTeamID,
	}

	result, err := c.ListSchedules(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing schedules: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetSchedule(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting schedule: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(scheduleFile)
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

	result, err := c.CreateSchedule(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating schedule: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(scheduleFile)
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

	result, err := c.ApplySchedule(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying schedule: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteSchedule(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting schedule: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Schedule %s deleted successfully\n", args[0])
}
