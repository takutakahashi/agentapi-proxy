package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var (
	endpoint      string
	sessionID     string
	confirmDelete bool
)

// task subcommand flags
var (
	taskTitle         string
	taskDescription   string
	taskType          string
	taskScope         string
	taskTeamID        string
	taskGroupID       string
	taskStatus        string
	taskNewSessionID  string
	taskFilterStatus  string
	taskFilterType    string
	taskFilterScope   string
	taskFilterTeamID  string
	taskFilterGroupID string
	taskLinks         []string
)

// memory subcommand flags
var (
	memoryTitle       string
	memoryContent     string
	memoryContentFile string
	memoryScope       string
	memoryTeamID      string
	memoryTags        []string
	memoryExcludeTags []string
	memoryKeys        []string
	memoryFormat      string
	memoryUnion       bool
)

// memory save-session subcommand flags
var (
	memorySaveSessionScope  string
	memorySaveSessionTeamID string
)

// summarize-drafts subcommand flags
var (
	summarizeDraftsSourceSessionID string
	summarizeDraftsScope           string
	summarizeDraftsTeamID          string
	summarizeDraftsKeys            []string
)

// send-notification subcommand flags
var (
	clientNotifyTitle     string
	clientNotifyBody      string
	clientNotifySessionID string
	clientNotifyUserID    string
)

// cycle subcommand flags
var (
	cycleMaxCount int
)

// resolveClient creates a client using flags if provided, otherwise falling back
// to environment variables (AGENTAPI_PROXY_SERVICE_HOST, AGENTAPI_PROXY_SERVICE_PORT_HTTP,
// AGENTAPI_SESSION_ID, AGENTAPI_KEY).
// Returns the client and the resolved session ID.
func resolveClient() (*client.Client, string, error) {
	resolvedEndpoint := endpoint
	resolvedSessionID := sessionID

	if resolvedEndpoint == "" {
		envEndpoint, err := client.EndpointFromEnv()
		if err != nil {
			return nil, "", fmt.Errorf("--endpoint not specified and %w", err)
		}
		resolvedEndpoint = envEndpoint
	}

	if resolvedSessionID == "" {
		resolvedSessionID = os.Getenv("AGENTAPI_SESSION_ID")
		if resolvedSessionID == "" {
			return nil, "", fmt.Errorf("--session-id not specified and AGENTAPI_SESSION_ID is not set")
		}
	}

	apiKey := os.Getenv("AGENTAPI_KEY")
	c := client.NewClient(resolvedEndpoint, client.WithAPIKeyAuth(apiKey))
	return c, resolvedSessionID, nil
}

// resolveMemoryClient creates a client for memory operations using flags or env vars.
// Unlike resolveClient, session-id is not required for memory operations.
func resolveMemoryClient() (*client.Client, error) {
	resolvedEndpoint := endpoint
	if resolvedEndpoint == "" {
		envEndpoint, err := client.EndpointFromEnv()
		if err != nil {
			return nil, fmt.Errorf("--endpoint not specified and %w", err)
		}
		resolvedEndpoint = envEndpoint
	}

	apiKey := os.Getenv("AGENTAPI_KEY")
	return client.NewClient(resolvedEndpoint, client.WithAPIKeyAuth(apiKey)), nil
}

// parseKeyValueFlags parses a slice of "key=value" strings into a map.
func parseKeyValueFlags(flags []string) (map[string]string, error) {
	result := make(map[string]string, len(flags))
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid key=value format: %q", f)
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

var ClientCmd = &cobra.Command{
	Use:   "client",
	Short: "AgentAPI Client CLI",
	Long:  "Command line client for interacting with AgentAPI endpoints",
}

var cycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "Send a cycle message read from CYCLE_ENABLED; no-op if the file is absent",
	Long: `Read /tmp/check/CYCLE_ENABLED. If the file does not exist, exit without doing
anything (cycle is disabled).

The file content is used as the message sent to the session.  This allows the
Stop hook to be registered permanently in Claude's settings and the cycle to be
activated simply by writing the desired message into /tmp/check/CYCLE_ENABLED.

To stop the cycle, the agent (or a user) deletes /tmp/check/CYCLE_ENABLED.  The
auto-appended suffix included in every sent message instructs the agent to do this
when the goal is achieved.

Each invocation increments a counter stored in /tmp/check/CYCLE_COUNT.
If --max-count is set and the counter reaches the limit, the command exits without
sending a message and also removes CYCLE_ENABLED to prevent further cycles.

Examples:
  # Enable cycling by writing a message into the marker file
  mkdir -p /tmp/check
  echo "Please continue the task" > /tmp/check/CYCLE_ENABLED

  # Stop after 10 cycles at most
  agentapi-proxy client cycle --max-count 10

  agentapi-proxy client cycle --session-id my-session`,
	Args: cobra.NoArgs,
	RunE: runCycle,
}

var sendCmd = &cobra.Command{
	Use:   "send [message]",
	Short: "Send a message to the agent",
	Long:  "Send a message to the agent endpoint",
	Args:  cobra.MaximumNArgs(1),
	Run:   runSend,
}

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Get conversation history",
	Long:  "Retrieve the conversation history from the agent",
	Run:   runHistory,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get agent status",
	Long:  "Get the current status of the agent",
	Run:   runStatus,
}

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Monitor agent events",
	Long:  "Monitor real-time events from the agent using Server-Sent Events",
	Run:   runEvents,
}

var deleteSessionCmd = &cobra.Command{
	Use:   "delete-session",
	Short: "Delete the current session",
	Long: `Delete the current session using environment variables.

This command deletes the current agent session by reading configuration from
environment variables:
- AGENTAPI_SESSION_ID: The session ID to delete
- AGENTAPI_KEY: API key for authentication
- AGENTAPI_PROXY_SERVICE_HOST and AGENTAPI_PROXY_SERVICE_PORT_HTTP: For endpoint URL

Examples:
  # Delete current session (with confirmation)
  agentapi-proxy client delete-session

  # Delete current session without confirmation
  agentapi-proxy client delete-session --confirm`,
	Run: runDeleteSession,
}

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage memory entries",
	Long:  "Create, list, get, update, delete, and upsert memory entries",
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memory entries",
	Long: `List memory entries with optional filters.

Formats:
  json     - JSON array (default)
  markdown - Markdown with titles and content (suitable for injection into CLAUDE.md)
  content  - Raw content only

Examples:
  agentapi-proxy client memory list --scope user
  agentapi-proxy client memory list --scope team --team-id myorg/myteam
  agentapi-proxy client memory list --tag project=myapp --format markdown`,
	Run: runMemoryList,
}

var memoryGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a memory entry by ID",
	Long:  "Retrieve details of a specific memory entry. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runMemoryGet,
}

var memoryCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new memory entry",
	Long: `Create a new memory entry.

Examples:
  agentapi-proxy client memory create \
    --title "Project notes" \
    --content "Key decisions..." \
    --scope user

  agentapi-proxy client memory create \
    --title "Team knowledge" \
    --content-file /tmp/notes.md \
    --scope team --team-id myorg/myteam \
    --tag project=myapp`,
	Run: runMemoryCreate,
}

var memoryUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a memory entry",
	Long:  "Update an existing memory entry. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runMemoryUpdate,
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a memory entry",
	Long:  "Delete a memory entry by ID. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runMemoryDelete,
}

var memoryUpsertCmd = &cobra.Command{
	Use:   "upsert",
	Short: "Create or update a memory entry by key tags",
	Long: `Create or update a memory entry identified by key tags.

The --key flags define the lookup criteria (AND logic). If a matching entry
is found, it is updated. If not, a new entry is created with the key tags
as its tags.

Examples:
  agentapi-proxy client memory upsert \
    --title "Session summary" \
    --content-file /tmp/content.md \
    --key project=myapp --key env=prod \
    --scope user`,
	Run: runMemoryUpsert,
}

var memorySaveSessionCmd = &cobra.Command{
	Use:   "save-session",
	Short: "Save current session conversation to memory",
	Long: `Save the current session's conversation history to a memory entry.

Session information is resolved from environment variables:
  AGENTAPI_SESSION_ID             Session ID to save (required)
  AGENTAPI_REPO_FULLNAME          Repository full name (e.g., owner/repo)
  AGENTAPI_TEAM_ID                Team ID (used when --scope team is not specified)

Memory tags are populated automatically:
  session_id=<id>                 Always set (used as unique key)
  repo=<owner/repo>               Set when AGENTAPI_REPO_FULLNAME is available
  schedule_id=<id>                Set when the session was triggered by a schedule
  webhook_id=<id>                 Set when the session was triggered by a webhook
  slackbot_id=<id>                Set when the session was triggered by a slackbot
  date=<YYYY-MM-DD>               Date the memory was saved

This command is intended to be called from a Stop hook to automatically
accumulate session knowledge into a memory-based knowledge base.

Examples:
  # Save current session using env vars (typical Stop hook usage)
  agentapi-proxy client memory save-session

  # Save as team-scoped memory
  agentapi-proxy client memory save-session --scope team --team-id myorg/myteam`,
	Run: runMemorySaveSession,
}

var summarizeDraftsCmd = &cobra.Command{
	Use:   "summarize-drafts",
	Short: "Create a one-shot session to summarize draft memories",
	Long: `Create a one-shot session that summarizes draft memories from a completed session.

The session will check for draft memories tagged with session-id=<source-session-id>
and draft=true, summarize their content, update the corresponding main memory with a
date snapshot, and delete the draft memories.

This command is typically called automatically by the memory-sync sidecar when a
session with memory_key configured stops. It can also be invoked manually.

Examples:
  agentapi-proxy client summarize-drafts \
    --source-session-id abc123 \
    --scope user

  agentapi-proxy client summarize-drafts \
    --source-session-id abc123 \
    --scope team --team-id myorg/myteam \
    --key project=myapp --key env=prod`,
	Run: runSummarizeDrafts,
}

var sendNotificationClientCmd = &cobra.Command{
	Use:   "send-notification",
	Short: "Send a push notification via API",
	Long: `Send a push notification to subscribers via the agentapi-proxy API.

Either --notify-session-id or --notify-user-id must be specified to identify the target.

Examples:
  # Send to all users subscribed to a session
  agentapi-proxy client send-notification \
    --title "作業が完了しました" \
    --body "作業内容を確認してください" \
    --notify-session-id "$AGENTAPI_SESSION_ID"

  # Send to a specific user
  agentapi-proxy client send-notification \
    --title "Notification" \
    --body "Something happened" \
    --notify-user-id "user123"`,
	RunE: runClientSendNotification,
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks",
	Long:  "Create, list, get, update, and delete tasks",
}

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	Long: `Create a new task associated with the current session.

--session-id and --endpoint flags are required.

Examples:
  agentapi-proxy client task create \
    --endpoint http://proxy:8080 \
    --session-id my-session \
    --title "My task" \
    --task-type agent \
    --scope user

  # With links (url only)
  agentapi-proxy client task create \
    --endpoint http://proxy:8080 \
    --session-id my-session \
    --title "Review PR" \
    --task-type user \
    --scope user \
    --link "https://github.com/owner/repo/pull/123"

  # With links (url and title)
  agentapi-proxy client task create \
    --endpoint http://proxy:8080 \
    --session-id my-session \
    --title "Review PR" \
    --task-type user \
    --scope user \
    --link "https://github.com/owner/repo/pull/123|PR #123"`,
	Run: runTaskCreate,
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	Long:  "List tasks with optional filters. --endpoint is required.",
	Run:   runTaskList,
}

var taskGetCmd = &cobra.Command{
	Use:   "get <taskId>",
	Short: "Get a task by ID",
	Long:  "Retrieve details of a specific task. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskGet,
}

var taskUpdateCmd = &cobra.Command{
	Use:   "update <taskId>",
	Short: "Update a task",
	Long:  "Partially update an existing task. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskUpdate,
}

var taskDeleteCmd = &cobra.Command{
	Use:   "delete <taskId>",
	Short: "Delete a task",
	Long:  "Delete a task by ID. --endpoint is required.",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskDelete,
}

func init() {
	ClientCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", "", "AgentAPI endpoint URL (required for most commands)")
	ClientCmd.PersistentFlags().StringVarP(&sessionID, "session-id", "s", "", "Session ID for the agent (required for most commands)")

	// delete-session command flags
	deleteSessionCmd.Flags().BoolVar(&confirmDelete, "confirm", false, "Skip confirmation prompt")

	// task create flags
	taskCreateCmd.Flags().StringVar(&taskTitle, "title", "", "Task title (required)")
	taskCreateCmd.Flags().StringVar(&taskDescription, "description", "", "Task description")
	taskCreateCmd.Flags().StringVar(&taskType, "task-type", "agent", `Task type: "user" or "agent"`)
	taskCreateCmd.Flags().StringVar(&taskScope, "scope", "user", `Task scope: "user" or "team"`)
	taskCreateCmd.Flags().StringVar(&taskTeamID, "team-id", "", "Team ID (required when scope is 'team')")
	taskCreateCmd.Flags().StringVar(&taskGroupID, "group-id", "", "Task group ID (optional)")
	taskCreateCmd.Flags().StringArrayVar(&taskLinks, "link", nil, `Link to associate with the task. Format: "url" or "url|title". Can be specified multiple times.`)

	// task list flags
	taskListCmd.Flags().StringVar(&taskFilterScope, "scope", "", `Filter by scope: "user" or "team"`)
	taskListCmd.Flags().StringVar(&taskFilterTeamID, "team-id", "", "Filter by team ID")
	taskListCmd.Flags().StringVar(&taskFilterGroupID, "group-id", "", "Filter by group ID")
	taskListCmd.Flags().StringVar(&taskFilterStatus, "status", "", `Filter by status: "todo" or "done"`)
	taskListCmd.Flags().StringVar(&taskFilterType, "task-type", "", `Filter by type: "user" or "agent"`)

	// task update flags
	taskUpdateCmd.Flags().StringVar(&taskTitle, "title", "", "New title")
	taskUpdateCmd.Flags().StringVar(&taskDescription, "description", "", "New description")
	taskUpdateCmd.Flags().StringVar(&taskStatus, "status", "", `New status: "todo" or "done"`)
	taskUpdateCmd.Flags().StringVar(&taskGroupID, "group-id", "", "New group ID")
	taskUpdateCmd.Flags().StringVar(&taskNewSessionID, "session-id-new", "", "New session ID to associate with the task")

	// task subcommands
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskUpdateCmd)
	taskCmd.AddCommand(taskDeleteCmd)

	// memory list flags
	memoryListCmd.Flags().StringVar(&memoryScope, "scope", "user", `Memory scope: "user" or "team"`)
	memoryListCmd.Flags().StringVar(&memoryTeamID, "team-id", "", "Team ID (required when scope is 'team')")
	memoryListCmd.Flags().StringArrayVar(&memoryTags, "tag", nil, "Tag filter in key=value format (AND logic by default, can be specified multiple times)")
	memoryListCmd.Flags().StringArrayVar(&memoryExcludeTags, "exclude-tag", nil, "Exclude memories matching these tags in key=value format (AND logic, can be specified multiple times)")
	memoryListCmd.Flags().StringVar(&memoryFormat, "format", "json", `Output format: "json", "markdown", or "content"`)
	memoryListCmd.Flags().BoolVar(&memoryUnion, "union", false, "Union mode: return memories matching ANY specified tag (OR logic, makes a separate API call per tag)")

	// memory create flags
	memoryCreateCmd.Flags().StringVar(&memoryTitle, "title", "", "Memory title (required)")
	memoryCreateCmd.Flags().StringVar(&memoryContent, "content", "", "Memory content")
	memoryCreateCmd.Flags().StringVar(&memoryContentFile, "content-file", "", "Path to file containing memory content")
	memoryCreateCmd.Flags().StringVar(&memoryScope, "scope", "user", `Memory scope: "user" or "team"`)
	memoryCreateCmd.Flags().StringVar(&memoryTeamID, "team-id", "", "Team ID (required when scope is 'team')")
	memoryCreateCmd.Flags().StringArrayVar(&memoryTags, "tag", nil, "Tag in key=value format (can be specified multiple times)")

	// memory update flags
	memoryUpdateCmd.Flags().StringVar(&memoryTitle, "title", "", "New title")
	memoryUpdateCmd.Flags().StringVar(&memoryContent, "content", "", "New content")
	memoryUpdateCmd.Flags().StringVar(&memoryContentFile, "content-file", "", "Path to file containing new content")
	memoryUpdateCmd.Flags().StringArrayVar(&memoryTags, "tag", nil, "New tags in key=value format (replaces all tags)")

	// memory upsert flags
	memoryUpsertCmd.Flags().StringVar(&memoryTitle, "title", "", "Memory title (required)")
	memoryUpsertCmd.Flags().StringVar(&memoryContent, "content", "", "Memory content")
	memoryUpsertCmd.Flags().StringVar(&memoryContentFile, "content-file", "", "Path to file containing memory content")
	memoryUpsertCmd.Flags().StringVar(&memoryScope, "scope", "user", `Memory scope: "user" or "team"`)
	memoryUpsertCmd.Flags().StringVar(&memoryTeamID, "team-id", "", "Team ID (required when scope is 'team')")
	memoryUpsertCmd.Flags().StringArrayVar(&memoryKeys, "key", nil, "Key tag in key=value format for lookup (AND logic, can be specified multiple times)")
	memoryUpsertCmd.Flags().StringArrayVar(&memoryTags, "tag", nil, "Additional tag in key=value format (merged with --key tags)")

	// memory save-session flags
	memorySaveSessionCmd.Flags().StringVar(&memorySaveSessionScope, "scope", "user", `Memory scope: "user" or "team"`)
	memorySaveSessionCmd.Flags().StringVar(&memorySaveSessionTeamID, "team-id", "", "Team ID (required when scope is 'team')")

	// memory subcommands
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryGetCmd)
	memoryCmd.AddCommand(memoryCreateCmd)
	memoryCmd.AddCommand(memoryUpdateCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)
	memoryCmd.AddCommand(memoryUpsertCmd)
	memoryCmd.AddCommand(memorySaveSessionCmd)

	// summarize-drafts flags
	summarizeDraftsCmd.Flags().StringVar(&summarizeDraftsSourceSessionID, "source-session-id", "", "Session ID whose draft memories should be summarized (required)")
	summarizeDraftsCmd.Flags().StringVar(&summarizeDraftsScope, "scope", "user", `Memory scope: "user" or "team"`)
	summarizeDraftsCmd.Flags().StringVar(&summarizeDraftsTeamID, "team-id", "", "Team ID (required when scope is 'team')")
	summarizeDraftsCmd.Flags().StringArrayVar(&summarizeDraftsKeys, "key", nil, "Memory key tag in key=value format (can be specified multiple times)")

	// send-notification flags
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyTitle, "title", "", "Notification title (required)")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyBody, "body", "", "Notification body (required)")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifySessionID, "notify-session-id", "", "Session ID whose subscribers will receive the notification")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyUserID, "notify-user-id", "", "User ID to send the notification to")

	// cycle flags
	cycleCmd.Flags().IntVar(&cycleMaxCount, "max-count", 10, "Maximum number of cycles (0 means unlimited, default: 10). Exits when the count in /tmp/check/CYCLE_COUNT reaches this limit.")

	ClientCmd.AddCommand(cycleCmd)
	ClientCmd.AddCommand(sendCmd)
	ClientCmd.AddCommand(historyCmd)
	ClientCmd.AddCommand(statusCmd)
	ClientCmd.AddCommand(eventsCmd)
	ClientCmd.AddCommand(deleteSessionCmd)
	ClientCmd.AddCommand(summarizeDraftsCmd)
	ClientCmd.AddCommand(sendNotificationClientCmd)
	ClientCmd.AddCommand(taskCmd)
	ClientCmd.AddCommand(memoryCmd)
}

const (
	cycleEnabledPath = "/tmp/check/CYCLE_ENABLED"
	cycleCountPath   = "/tmp/check/CYCLE_COUNT"
)

// cycleConditionCheckSuffix is appended to every message sent by the cycle command.
// It instructs the agent to check whether the conditions described in the message
// are met, and to delete CYCLE_ENABLED if so (which stops further cycles).
const cycleConditionCheckSuffix = `

---
⚠️ [cycle セッション] このメッセージを受け取ったら、まず上記の達成条件を確認してください。
条件をすべて満たしている場合は、追加の作業は行わずに直ちに以下のコマンドを実行してサイクルを終了させてください:

` + "```" + `bash
rm -f /tmp/check/CYCLE_ENABLED
` + "```" + `

条件を満たしていない場合は、引き続き作業を行ってください。`

// readCycleCount reads the current cycle count from cycleCountPath.
// Returns 0 if the file does not exist.
func readCycleCount() (int, error) {
	data, err := os.ReadFile(cycleCountPath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read cycle count: %w", err)
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count); err != nil {
		return 0, fmt.Errorf("invalid cycle count in %s: %w", cycleCountPath, err)
	}
	return count, nil
}

// writeCycleCount writes the given count to cycleCountPath.
func writeCycleCount(count int) error {
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		return fmt.Errorf("failed to create /tmp/check: %w", err)
	}
	if err := os.WriteFile(cycleCountPath, []byte(fmt.Sprintf("%d\n", count)), 0o644); err != nil {
		return fmt.Errorf("failed to write cycle count: %w", err)
	}
	return nil
}

func runCycle(cmd *cobra.Command, args []string) error {
	// Read /tmp/check/CYCLE_ENABLED. Its content is the message to send.
	// If the file does not exist the cycle is disabled — exit without doing anything.
	messageBytes, err := os.ReadFile(cycleEnabledPath)
	if os.IsNotExist(err) {
		fmt.Println("CYCLE_ENABLED not found, skipping cycle")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cycleEnabledPath, err)
	}

	message := strings.TrimSpace(string(messageBytes))
	if message == "" {
		return fmt.Errorf("%s exists but is empty; write the cycle message into it", cycleEnabledPath)
	}

	// Read and check the cycle count when --max-count is set
	if cycleMaxCount > 0 {
		count, err := readCycleCount()
		if err != nil {
			return err
		}
		if count >= cycleMaxCount {
			fmt.Printf("Cycle count limit reached (%d/%d), removing CYCLE_ENABLED\n", count, cycleMaxCount)
			// Remove CYCLE_ENABLED so the hook becomes a no-op from now on.
			_ = os.Remove(cycleEnabledPath)
			return nil
		}
		// Increment and persist the counter before sending
		if err := writeCycleCount(count + 1); err != nil {
			return err
		}
		fmt.Printf("Cycle count: %d/%d\n", count+1, cycleMaxCount)
	}

	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		return fmt.Errorf("failed to resolve client: %w", err)
	}

	// Wait for the session to become stable before sending.
	// The Stop hook fires while Claude is still wrapping up ("running"),
	// and agentapi rejects user messages with 422 until the status is "stable".
	ctx := context.Background()
	if err := waitForStable(ctx, c, resolvedSessionID); err != nil {
		return fmt.Errorf("timed out waiting for stable status: %w", err)
	}

	msg := &client.Message{
		Content: message + cycleConditionCheckSuffix,
		Type:    "user",
	}

	msgResp, err := c.SendMessage(ctx, resolvedSessionID, msg)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}

	if msgResp.OK {
		fmt.Println("Message sent successfully")
	} else {
		return fmt.Errorf("message was not sent successfully")
	}

	return nil
}

// waitForStable polls the session status until it is "stable" or the timeout is reached.
func waitForStable(ctx context.Context, c *client.Client, sessionID string) error {
	const (
		pollInterval = 2 * time.Second
		timeout      = 120 * time.Second
	)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statusResp, err := c.GetStatus(ctx, sessionID)
		if err == nil && statusResp.Status == "stable" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("session did not become stable within %s", timeout)
}

func runSend(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var message string
	if len(args) > 0 {
		message = args[0]
	} else {
		fmt.Print("Enter message: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			message = scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}
	}

	if message == "" {
		fmt.Fprintf(os.Stderr, "Message cannot be empty\n")
		return
	}

	ctx := context.Background()

	msg := &client.Message{
		Content: message,
		Type:    "user",
	}

	msgResp, err := c.SendMessage(ctx, resolvedSessionID, msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		return
	}

	if msgResp.OK {
		fmt.Printf("Message sent successfully\n")
	} else {
		fmt.Fprintf(os.Stderr, "Message was not sent successfully\n")
	}
}

func runHistory(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	messagesResp, err := c.GetMessages(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting history: %v\n", err)
		return
	}

	fmt.Printf("Conversation History (%d messages):\n", len(messagesResp.Messages))
	for _, msg := range messagesResp.Messages {
		ts := ""
		if msg.Timestamp != nil {
			ts = msg.Timestamp.Format("15:04:05")
		}
		fmt.Printf("[%s] %s: %s\n", ts, msg.Role, msg.Content)
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	statusResp, err := c.GetStatus(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
		return
	}

	fmt.Printf("Agent Status: %s\n", statusResp.Status)
}

func runEvents(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	eventChan, errorChan := c.StreamEvents(ctx, resolvedSessionID)

	fmt.Println("Monitoring events... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			if strings.HasPrefix(event, "data: ") {
				data := strings.TrimPrefix(event, "data: ")
				fmt.Printf("[EVENT] %s\n", data)
			}
		case err, ok := <-errorChan:
			if !ok {
				return
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading events: %v\n", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func runTaskCreate(cmd *cobra.Command, args []string) {
	if taskTitle == "" {
		fmt.Fprintf(os.Stderr, "Error: --title flag is required\n")
		os.Exit(1)
	}

	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	links := make([]client.TaskLink, 0, len(taskLinks))
	for _, l := range taskLinks {
		parts := strings.SplitN(l, "|", 2)
		link := client.TaskLink{URL: parts[0]}
		if len(parts) == 2 {
			link.Title = parts[1]
		}
		links = append(links, link)
	}

	req := &client.CreateTaskRequest{
		Title:       taskTitle,
		Description: taskDescription,
		TaskType:    taskType,
		Scope:       taskScope,
		TeamID:      taskTeamID,
		GroupID:     taskGroupID,
		Links:       links,
	}

	taskResp, err := c.CreateTask(ctx, resolvedSessionID, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating task: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(taskResp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runTaskList(cmd *cobra.Command, args []string) {
	c, _, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	opts := &client.ListTasksOptions{
		Scope:    taskFilterScope,
		TeamID:   taskFilterTeamID,
		GroupID:  taskFilterGroupID,
		Status:   taskFilterStatus,
		TaskType: taskFilterType,
	}

	listResp, err := c.ListTasks(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tasks: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Tasks (%d total):\n", listResp.Total)
	for _, t := range listResp.Tasks {
		fmt.Printf("  [%s] %s (%s/%s) session=%s\n", t.Status, t.Title, t.TaskType, t.Scope, t.SessionID)
	}
}

func runTaskGet(cmd *cobra.Command, args []string) {
	c, _, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	taskID := args[0]
	ctx := context.Background()

	taskResp, err := c.GetTask(ctx, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting task: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(taskResp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runTaskUpdate(cmd *cobra.Command, args []string) {
	c, _, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	taskID := args[0]
	ctx := context.Background()

	req := &client.UpdateTaskRequest{}
	if cmd.Flags().Changed("title") {
		req.Title = &taskTitle
	}
	if cmd.Flags().Changed("description") {
		req.Description = &taskDescription
	}
	if cmd.Flags().Changed("status") {
		req.Status = &taskStatus
	}
	if cmd.Flags().Changed("group-id") {
		req.GroupID = &taskGroupID
	}
	if cmd.Flags().Changed("session-id-new") {
		req.SessionID = &taskNewSessionID
	}

	taskResp, err := c.UpdateTask(ctx, taskID, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating task: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(taskResp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runTaskDelete(cmd *cobra.Command, args []string) {
	c, _, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	taskID := args[0]
	ctx := context.Background()

	if err := c.DeleteTask(ctx, taskID); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s deleted successfully\n", taskID)
}

// readJSONInput reads JSON from a file path, or from stdin if path is "-" or empty.
// Returns the raw bytes to be used as an HTTP request body.
func readJSONInput(file string) ([]byte, error) {
	if file == "-" || file == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", file, err)
	}
	return data, nil
}

// prettyJSONOutput pretty-prints raw JSON bytes. Falls back to the original bytes if parsing fails.
func prettyJSONOutput(data []byte) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	if err := enc.Encode(v); err != nil {
		return string(data)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// endpointHint is appended to error messages when the endpoint cannot be resolved,
// to help users understand how to configure the connection.
const endpointHint = `
Hint: configure the endpoint using one of the following methods:
  1. Flag:    --endpoint http://<host>:<port>
  2. Env vars: AGENTAPI_PROXY_SERVICE_HOST=<host> AGENTAPI_PROXY_SERVICE_PORT_HTTP=<port>

Optional authentication:
  AGENTAPI_KEY=<api-key>`

// resolveBaseClient creates a client using flags or environment variables.
// Unlike resolveClient, no session-id is required.
func resolveBaseClient() (*client.Client, error) {
	resolvedEndpoint := endpoint
	if resolvedEndpoint == "" {
		envEndpoint, err := client.EndpointFromEnv()
		if err != nil {
			return nil, fmt.Errorf("--endpoint not specified and %w", err)
		}
		resolvedEndpoint = envEndpoint
	}
	apiKey := os.Getenv("AGENTAPI_KEY")
	return client.NewClient(resolvedEndpoint, client.WithAPIKeyAuth(apiKey)), nil
}

// readContentFlag reads the memory content from --content or --content-file flags.
func readContentFlag(content, contentFile string) (string, error) {
	if contentFile != "" {
		data, err := os.ReadFile(contentFile)
		if err != nil {
			return "", fmt.Errorf("failed to read content file %q: %w", contentFile, err)
		}
		return string(data), nil
	}
	return content, nil
}

// formatMemoriesMarkdown formats memory entries as Markdown suitable for CLAUDE.md injection.
// Each entry is rendered as: title heading, body content, separated by horizontal rules.
func formatMemoriesMarkdown(memories []*client.MemoryEntry) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, m := range memories {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(m.Title)
		sb.WriteString("\n\n")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func runMemoryList(cmd *cobra.Command, args []string) {
	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tags, err := parseKeyValueFlags(memoryTags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --tag flag: %v\n", err)
		os.Exit(1)
	}

	excludeTags, err := parseKeyValueFlags(memoryExcludeTags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --exclude-tag flag: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	var listResp *client.MemoryListResponse
	if memoryUnion && len(tags) > 0 {
		listResp, err = c.ListMemoriesUnion(ctx, memoryScope, memoryTeamID, tags)
	} else {
		listResp, err = c.ListMemories(ctx, memoryScope, memoryTeamID, tags, excludeTags)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing memories: %v\n", err)
		os.Exit(1)
	}

	switch memoryFormat {
	case "markdown":
		fmt.Print(formatMemoriesMarkdown(listResp.Memories))
	case "content":
		for _, m := range listResp.Memories {
			fmt.Println(m.Content)
		}
	default: // "json"
		out, err := json.MarshalIndent(listResp, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	}
}

func runMemoryGet(cmd *cobra.Command, args []string) {
	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	entry, err := c.GetMemory(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting memory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runMemoryCreate(cmd *cobra.Command, args []string) {
	if memoryTitle == "" {
		fmt.Fprintf(os.Stderr, "Error: --title flag is required\n")
		os.Exit(1)
	}

	content, err := readContentFlag(memoryContent, memoryContentFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tags, err := parseKeyValueFlags(memoryTags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --tag flag: %v\n", err)
		os.Exit(1)
	}

	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	req := &client.CreateMemoryRequest{
		Title:   memoryTitle,
		Content: content,
		Scope:   memoryScope,
		TeamID:  memoryTeamID,
		Tags:    tags,
	}

	entry, err := c.CreateMemory(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating memory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runMemoryUpdate(cmd *cobra.Command, args []string) {
	content, err := readContentFlag(memoryContent, memoryContentFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	req := &client.UpdateMemoryRequest{}

	if cmd.Flags().Changed("title") {
		req.Title = memoryTitle
	}
	if cmd.Flags().Changed("content") || cmd.Flags().Changed("content-file") {
		req.Content = content
	}
	if cmd.Flags().Changed("tag") {
		tags, err := parseKeyValueFlags(memoryTags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --tag flag: %v\n", err)
			os.Exit(1)
		}
		req.Tags = tags
	}

	entry, err := c.UpdateMemory(ctx, args[0], req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating memory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runMemoryDelete(cmd *cobra.Command, args []string) {
	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := c.DeleteMemory(ctx, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting memory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Memory %s deleted successfully\n", args[0])
}

func runMemoryUpsert(cmd *cobra.Command, args []string) {
	if memoryTitle == "" {
		fmt.Fprintf(os.Stderr, "Error: --title flag is required\n")
		os.Exit(1)
	}

	content, err := readContentFlag(memoryContent, memoryContentFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	keyTags, err := parseKeyValueFlags(memoryKeys)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --key flag: %v\n", err)
		os.Exit(1)
	}

	// Merge --tag flags into keyTags (key flags take precedence)
	if len(memoryTags) > 0 {
		extraTags, err := parseKeyValueFlags(memoryTags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --tag flag: %v\n", err)
			os.Exit(1)
		}
		for k, v := range extraTags {
			if _, exists := keyTags[k]; !exists {
				keyTags[k] = v
			}
		}
	}

	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	entry, err := c.UpsertMemory(ctx, memoryScope, memoryTeamID, memoryTitle, content, keyTags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error upserting memory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func runMemorySaveSession(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// Resolve client and session ID (required)
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get conversation messages
	messagesResp, err := c.GetMessages(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting messages: %v\n", err)
		os.Exit(1)
	}

	if len(messagesResp.Messages) == 0 {
		fmt.Println("No messages in session, skipping memory save")
		return
	}

	// Try to get session metadata (tags) from the proxy; non-fatal if unavailable.
	var sessionTags map[string]string
	sessionInfo, err := c.GetSessionByID(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not retrieve session info (tags will be omitted): %v\n", err)
		sessionTags = make(map[string]string)
	} else {
		if sessionInfo.Tags != nil {
			sessionTags = sessionInfo.Tags
		} else {
			sessionTags = make(map[string]string)
		}
	}

	// Gather metadata from environment variables
	repo := os.Getenv("AGENTAPI_REPO_FULLNAME")
	teamID := memorySaveSessionTeamID
	if teamID == "" {
		teamID = os.Getenv("AGENTAPI_TEAM_ID")
	}

	scope := memorySaveSessionScope
	if scope == "user" && teamID != "" {
		scope = "team"
	}

	// Build memory tags
	tags := map[string]string{
		"session_id": resolvedSessionID,
		"date":       time.Now().Format("2006-01-02"),
	}
	if repo != "" {
		tags["repo"] = repo
	}
	for _, key := range []string{"schedule_id", "webhook_id", "slackbot_id"} {
		if v := sessionTags[key]; v != "" {
			tags[key] = v
		}
	}

	// Determine trigger source for display
	trigger := "manual"
	switch {
	case tags["schedule_id"] != "":
		trigger = "schedule:" + tags["schedule_id"]
	case tags["webhook_id"] != "":
		trigger = "webhook:" + tags["webhook_id"]
	case tags["slackbot_id"] != "":
		trigger = "slackbot:" + tags["slackbot_id"]
	}

	// Build memory content
	content := buildSessionMemoryContent(resolvedSessionID, repo, trigger, messagesResp.Messages)

	// Build title: use short session ID prefix + date
	shortID := resolvedSessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	title := fmt.Sprintf("Session %s (%s)", shortID, time.Now().Format("2006-01-02"))

	// Use memory client (does not require session ID)
	mc, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	req := &client.CreateMemoryRequest{
		Title:   title,
		Content: content,
		Scope:   scope,
		TeamID:  teamID,
		Tags:    tags,
	}

	entry, err := mc.CreateMemory(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating memory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// buildSessionMemoryContent formats session messages into a structured markdown memory entry.
func buildSessionMemoryContent(sessionID, repo, trigger string, messages []client.Message) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Session: %s\n", sessionID)
	fmt.Fprintf(&sb, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	if repo != "" {
		fmt.Fprintf(&sb, "**Repository**: %s\n", repo)
	}
	fmt.Fprintf(&sb, "**Triggered by**: %s\n", trigger)
	fmt.Fprintf(&sb, "**Message count**: %d\n\n", len(messages))
	sb.WriteString("### Conversation\n\n")

	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = msg.Type
		}
		if ts := msg.GetTimestamp(); ts != nil {
			fmt.Fprintf(&sb, "**[%s] %s**:\n", ts.Format("15:04:05"), role)
		} else {
			fmt.Fprintf(&sb, "**%s**:\n", role)
		}
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// buildSummarizationMessage returns the initial message for the draft summarization session.
func buildSummarizationMessage(sourceSessionID, today string) string {
	return fmt.Sprintf(
		"前のセッション（セッション ID: %s）のドラフトメモリをサマライズし、メモリを更新してください。\n\n"+
			"## 作業手順\n\n"+
			"1. `list_memories` ツールで以下の条件のメモリを取得してください\n"+
			"   - タグ: `session-id=%s` かつ `draft=true`\n"+
			"2. ドラフトの会話ログから重要な情報・決定事項・知見を抽出してください\n"+
			"3. 対応するメインメモリを探してください（同じ memory_key タグを持ち `draft` タグのないもの）\n"+
			"   - 存在しない場合は新規作成してください\n"+
			"4. メインメモリを次の方針で更新してください\n"+
			"   - 本日（%s）の日付スナップショットセクションを追加し、抽出した重要情報を記録\n"+
			"   - 重複・陳腐化した内容は削除\n"+
			"   - 将来的に参照価値の高い決定事項・知見を優先して残す\n"+
			"5. ドラフトメモリを `delete_memory` ツールで削除してください\n\n"+
			"ドラフトメモリが見つからない場合はその旨を確認して終了してください。",
		sourceSessionID, sourceSessionID, today,
	)
}

func runSummarizeDrafts(cmd *cobra.Command, args []string) {
	if summarizeDraftsSourceSessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --source-session-id is required\n")
		os.Exit(1)
	}

	c, err := resolveMemoryClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	today := time.Now().Format("2006-01-02")
	initialMessage := buildSummarizationMessage(summarizeDraftsSourceSessionID, today)

	ctx := context.Background()
	req := &client.StartRequest{
		Scope:  summarizeDraftsScope,
		TeamID: summarizeDraftsTeamID,
		// MemoryKey is intentionally not set for summarization sessions.
		// The session uses MCP tools to access memories directly, and we don't
		// want the summarization session to create its own draft memory which
		// would trigger another summarization session (infinite loop).
		Params: &client.StartParams{
			Message: initialMessage,
			Oneshot: true,
		},
	}

	resp, err := c.Start(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating summarization session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Summarization session created: %s\n", resp.SessionID)
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	// Create client from environment variables
	c, config, err := client.NewClientFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Show confirmation unless --confirm flag is set
	if !confirmDelete {
		fmt.Printf("Are you sure you want to delete session %s? [y/N]: ", config.SessionID)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			response := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if response != "y" && response != "yes" {
				fmt.Println("Deletion cancelled")
				return
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}
	}

	// Delete session
	ctx := context.Background()
	resp, err := c.DeleteSession(ctx, config.SessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session deleted successfully: %s\n", resp.Message)
	fmt.Printf("Session ID: %s\n", resp.SessionID)
}

// notifyRateLimitFile is the file used to track the last notification send time.
const notifyRateLimitFile = "/tmp/notify"

// notifyRateLimitCooldown is the minimum interval between client-side notifications.
const notifyRateLimitCooldown = 3 * time.Minute

// checkNotifyRateLimit returns true if a notification was sent within the cooldown window.
func checkNotifyRateLimit() bool {
	data, err := os.ReadFile(notifyRateLimitFile)
	if err != nil {
		return false // file missing → not rate-limited
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return false // unparseable → ignore
	}
	return time.Since(last) < notifyRateLimitCooldown
}

// recordNotifySent writes the current time to the rate-limit file.
func recordNotifySent() {
	_ = os.WriteFile(notifyRateLimitFile, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func runClientSendNotification(cmd *cobra.Command, args []string) error {
	if clientNotifyTitle == "" {
		return fmt.Errorf("--title is required")
	}
	if clientNotifyBody == "" {
		return fmt.Errorf("--body is required")
	}
	if clientNotifySessionID == "" && clientNotifyUserID == "" {
		return fmt.Errorf("either --notify-session-id or --notify-user-id is required")
	}

	// Client-side rate limiting: skip if a notification was sent recently.
	if checkNotifyRateLimit() {
		fmt.Println("Notification skipped (rate limited)")
		return nil
	}

	c, err := resolveMemoryClient()
	if err != nil {
		return fmt.Errorf("failed to resolve client: %w", err)
	}

	req := &client.SendNotificationRequest{
		Title:     clientNotifyTitle,
		Body:      clientNotifyBody,
		SessionID: clientNotifySessionID,
		UserID:    clientNotifyUserID,
	}

	resp, err := c.SendNotification(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	if resp.Success {
		recordNotifySent()
		fmt.Println("Notification sent successfully")
	} else {
		fmt.Fprintf(os.Stderr, "Notification send failed: %s\n", resp.Message)
		os.Exit(1)
	}
	return nil
}
