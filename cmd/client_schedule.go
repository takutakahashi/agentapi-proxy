package cmd

import (
	"context"
	"encoding/json"
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
	scheduleEnvVars      []string
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage schedules",
	Long: `Create, list, get, update, and delete schedules.

Connection:
  Endpoint is resolved from --endpoint flag or environment variables:
    AGENTAPI_PROXY_SERVICE_HOST=<host>
    AGENTAPI_PROXY_SERVICE_PORT_HTTP=<port>
  Authentication (optional): AGENTAPI_KEY=<api-key>

Typical workflow:
  # 1. List existing schedules
  agentapi-proxy client schedule list

  # 2. Inspect a specific schedule
  agentapi-proxy client schedule get <id> > schedule.json

  # 3. Edit schedule.json, then apply only the changed fields
  echo '{"status":"paused"}' | agentapi-proxy client schedule apply <id>

  # 4. Delete when no longer needed
  agentapi-proxy client schedule delete <id>`,
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schedules",
	Long: `List schedules and output as JSON array.

Filters (all optional):
  --status  "active", "paused", or "completed"
  --scope   "user" or "team"
  --team-id team identifier (required when --scope=team)

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
  agentapi-proxy client schedule get abc123 > schedule.json
  # Then edit schedule.json and run:
  agentapi-proxy client schedule apply abc123 --file schedule.json`,
	Args: cobra.ExactArgs(1),
	Run:  runScheduleGet,
}

var scheduleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a schedule from JSON",
	Long: `Create a new schedule from a JSON body.

Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".
The server assigns a unique ID and returns the created resource as JSON.

Use --env KEY=VALUE (repeatable) to inject environment variables into the session.
These are merged into session_config.environment, overriding any same-key values
already present in the JSON body.

Example JSON for a one-time schedule:
  {
    "name": "daily-report",
    "scheduled_at": "2026-03-25T09:00:00Z",
    "session_config": {
      "params": {"message": "Generate the daily report"}
    }
  }

Example JSON for a recurring schedule (cron):
  {
    "name": "weekday-standup",
    "cron_expr": "0 9 * * 1-5",
    "session_config": {
      "params": {"message": "Post standup summary"},
      "environment": {"MY_VAR": "value"}
    }
  }

Examples:
  # From a file
  agentapi-proxy client schedule create --file schedule.json

  # From stdin
  cat schedule.json | agentapi-proxy client schedule create

  # Inline one-time schedule
  echo '{"name":"daily-report","scheduled_at":"2026-03-25T09:00:00Z","session_config":{"params":{"message":"hello"}}}' \
    | agentapi-proxy client schedule create

  # With environment variables via --env flags
  echo '{"name":"daily-report","cron_expr":"0 9 * * 1-5","session_config":{"params":{"message":"hello"}}}' \
    | agentapi-proxy client schedule create --env MY_VAR=value --env DEBUG=true`,
	Run: runScheduleCreate,
}

var scheduleApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Apply (patch) a schedule from JSON",
	Long: `Partially update a schedule by sending a JSON patch.

Only the fields present in the JSON body are updated (merge-patch semantics).
Omitted fields are left unchanged on the server.
Reads JSON from a file (--file) or from stdin when --file is omitted or set to "-".

Use --env KEY=VALUE (repeatable) to set or update environment variables in
session_config.environment. These are merged into the patch body, overriding any
same-key values already present in the JSON body.

Typical workflow:
  1. agentapi-proxy client schedule get <id> > schedule.json
  2. Edit schedule.json (e.g. change status to "paused")
  3. agentapi-proxy client schedule apply <id> --file schedule.json

Examples:
  # Pause a schedule (only status is changed)
  echo '{"status":"paused"}' | agentapi-proxy client schedule apply abc123

  # Update cron expression only
  echo '{"cron_expr":"0 10 * * 1-5"}' | agentapi-proxy client schedule apply abc123

  # Apply a full JSON file (unchanged fields are preserved on the server)
  agentapi-proxy client schedule apply abc123 --file schedule.json

  # Update environment variables only
  echo '{}' | agentapi-proxy client schedule apply abc123 --env MY_VAR=new_value --env DEBUG=false

  # Combine JSON patch with --env flags (flags take precedence for overlapping keys)
  echo '{"status":"active"}' | agentapi-proxy client schedule apply abc123 --env MY_VAR=value`,
	Args: cobra.ExactArgs(1),
	Run:  runScheduleApply,
}

var scheduleDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a schedule by ID",
	Long: `Delete a schedule by ID. This action is irreversible.

To find the ID, run:
  agentapi-proxy client schedule list

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
	scheduleCreateCmd.Flags().StringArrayVar(&scheduleEnvVars, "env", nil, `Environment variable in KEY=VALUE format (can be specified multiple times)`)
	scheduleApplyCmd.Flags().StringVarP(&scheduleFile, "file", "f", "", `Path to JSON file, or "-" for stdin (default: stdin)`)
	scheduleApplyCmd.Flags().StringArrayVar(&scheduleEnvVars, "env", nil, `Environment variable in KEY=VALUE format (can be specified multiple times)`)

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
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
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
		fmt.Fprintf(os.Stderr, "Hint: verify the endpoint is reachable and credentials are correct.\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleGet(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.GetSchedule(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting schedule %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID is correct with: agentapi-proxy client schedule list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

// mergeEnvIntoScheduleJSON merges the given environment variable map into the
// session_config.environment field of the provided JSON object.  If envMap is
// empty the original data is returned unchanged.  CLI --env flags take
// precedence over values already present in the JSON body.
func mergeEnvIntoScheduleJSON(data []byte, envMap map[string]string) ([]byte, error) {
	if len(envMap) == 0 {
		return data, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Ensure session_config exists
	sessionConfig, _ := obj["session_config"].(map[string]interface{})
	if sessionConfig == nil {
		sessionConfig = make(map[string]interface{})
		obj["session_config"] = sessionConfig
	}

	// Ensure environment exists inside session_config
	env, _ := sessionConfig["environment"].(map[string]interface{})
	if env == nil {
		env = make(map[string]interface{})
		sessionConfig["environment"] = env
	}

	// Merge: CLI flags override JSON body values
	for k, v := range envMap {
		env[k] = v
	}

	merged, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return merged, nil
}

func runScheduleCreate(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(scheduleFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide JSON via stdin or use --file path/to/schedule.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"name\":\"my-schedule\",\"cron_expr\":\"0 9 * * 1-5\"}' | agentapi-proxy client schedule create\n")
		os.Exit(1)
	}

	if len(scheduleEnvVars) > 0 {
		envMap, err := parseKeyValueFlags(scheduleEnvVars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing --env flags: %v\n", err)
			fmt.Fprintf(os.Stderr, "Hint: use KEY=VALUE format, e.g. --env MY_VAR=value\n")
			os.Exit(1)
		}
		data, err = mergeEnvIntoScheduleJSON(data, envMap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error merging environment variables: %v\n", err)
			os.Exit(1)
		}
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.CreateSchedule(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating schedule: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: check the JSON body for required fields (e.g. name, cron_expr or scheduled_at).\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleApply(cmd *cobra.Command, args []string) {
	data, err := readJSONInput(scheduleFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: provide the patch as JSON via stdin or use --file path/to/patch.json\n")
		fmt.Fprintf(os.Stderr, "Example: echo '{\"status\":\"paused\"}' | agentapi-proxy client schedule apply %s\n", args[0])
		os.Exit(1)
	}

	if len(scheduleEnvVars) > 0 {
		envMap, err := parseKeyValueFlags(scheduleEnvVars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing --env flags: %v\n", err)
			fmt.Fprintf(os.Stderr, "Hint: use KEY=VALUE format, e.g. --env MY_VAR=value\n")
			os.Exit(1)
		}
		data, err = mergeEnvIntoScheduleJSON(data, envMap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error merging environment variables: %v\n", err)
			os.Exit(1)
		}
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	result, err := c.ApplySchedule(ctx, args[0], data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying schedule %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client schedule list\n")
		os.Exit(1)
	}

	fmt.Println(prettyJSONOutput(result))
}

func runScheduleDelete(cmd *cobra.Command, args []string) {
	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n%s\n", err, endpointHint)
		os.Exit(1)
	}

	ctx := context.Background()

	if err := c.DeleteSchedule(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting schedule %q: %v\n", args[0], err)
		fmt.Fprintf(os.Stderr, "Hint: confirm the ID exists with: agentapi-proxy client schedule list\n")
		os.Exit(1)
	}

	fmt.Printf("Schedule %s deleted successfully\n", args[0])
}
