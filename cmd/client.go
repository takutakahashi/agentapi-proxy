package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

var ClientCmd = &cobra.Command{
	Use:   "client",
	Short: "AgentAPI Client CLI",
	Long:  "Command line client for interacting with AgentAPI endpoints",
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

	ClientCmd.AddCommand(sendCmd)
	ClientCmd.AddCommand(historyCmd)
	ClientCmd.AddCommand(statusCmd)
	ClientCmd.AddCommand(eventsCmd)
	ClientCmd.AddCommand(deleteSessionCmd)
	ClientCmd.AddCommand(taskCmd)
}

func runSend(cmd *cobra.Command, args []string) {
	if endpoint == "" || sessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint and --session-id flags are required\n")
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

	// Create client and send message
	c := client.NewClient(endpoint)
	ctx := context.Background()

	msg := &client.Message{
		Content: message,
		Type:    "user",
	}

	msgResp, err := c.SendMessage(ctx, sessionID, msg)
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
	if endpoint == "" || sessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint and --session-id flags are required\n")
		os.Exit(1)
	}

	c := client.NewClient(endpoint)
	ctx := context.Background()

	messagesResp, err := c.GetMessages(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting history: %v\n", err)
		return
	}

	fmt.Printf("Conversation History (%d messages):\n", len(messagesResp.Messages))
	for _, msg := range messagesResp.Messages {
		fmt.Printf("[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	if endpoint == "" || sessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint and --session-id flags are required\n")
		os.Exit(1)
	}

	c := client.NewClient(endpoint)
	ctx := context.Background()

	statusResp, err := c.GetStatus(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
		return
	}

	fmt.Printf("Agent Status: %s\n", statusResp.Status)
}

func runEvents(cmd *cobra.Command, args []string) {
	if endpoint == "" || sessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint and --session-id flags are required\n")
		os.Exit(1)
	}

	c := client.NewClient(endpoint)
	ctx := context.Background()

	eventChan, errorChan := c.StreamEvents(ctx, sessionID)

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
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint flag is required\n")
		os.Exit(1)
	}
	if sessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: --session-id flag is required\n")
		os.Exit(1)
	}
	if taskTitle == "" {
		fmt.Fprintf(os.Stderr, "Error: --title flag is required\n")
		os.Exit(1)
	}

	c := client.NewClient(endpoint)
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

	taskResp, err := c.CreateTask(ctx, sessionID, req)
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
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint flag is required\n")
		os.Exit(1)
	}

	c := client.NewClient(endpoint)
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
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint flag is required\n")
		os.Exit(1)
	}

	taskID := args[0]
	c := client.NewClient(endpoint)
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
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint flag is required\n")
		os.Exit(1)
	}

	taskID := args[0]
	c := client.NewClient(endpoint)
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
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Error: --endpoint flag is required\n")
		os.Exit(1)
	}

	taskID := args[0]
	c := client.NewClient(endpoint)
	ctx := context.Background()

	if err := c.DeleteTask(ctx, taskID); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s deleted successfully\n", taskID)
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
