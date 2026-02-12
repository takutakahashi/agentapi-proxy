package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var (
	endpoint       string
	sessionID      string
	confirmDelete  bool
	useEnvEndpoint bool
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

The endpoint can be overridden with the --endpoint flag.

Examples:
  # Delete current session (with confirmation)
  agentapi-proxy client delete-session

  # Delete current session without confirmation
  agentapi-proxy client delete-session --confirm

  # Delete with custom endpoint
  agentapi-proxy client delete-session --endpoint http://localhost:8080`,
	Run: runDeleteSession,
}

func init() {
	ClientCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", "", "AgentAPI endpoint URL (required)")
	ClientCmd.PersistentFlags().StringVarP(&sessionID, "session-id", "s", "", "Session ID for the agent (required)")

	if err := ClientCmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		panic(err)
	}
	if err := ClientCmd.MarkPersistentFlagRequired("session-id"); err != nil {
		panic(err)
	}

	// delete-session command flags
	deleteSessionCmd.Flags().BoolVar(&confirmDelete, "confirm", false, "Skip confirmation prompt")
	deleteSessionCmd.Flags().BoolVar(&useEnvEndpoint, "use-env-endpoint", false, "Use endpoint from environment variables")

	ClientCmd.AddCommand(sendCmd)
	ClientCmd.AddCommand(historyCmd)
	ClientCmd.AddCommand(statusCmd)
	ClientCmd.AddCommand(eventsCmd)
	ClientCmd.AddCommand(deleteSessionCmd)
}

func runSend(cmd *cobra.Command, args []string) {
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

func runDeleteSession(cmd *cobra.Command, args []string) {
	// Get session ID from environment variable
	envSessionID := os.Getenv("AGENTAPI_SESSION_ID")
	if envSessionID == "" {
		fmt.Fprintf(os.Stderr, "Error: AGENTAPI_SESSION_ID environment variable is not set\n")
		os.Exit(1)
	}

	// Get API key from environment variable
	apiKey := os.Getenv("AGENTAPI_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: AGENTAPI_KEY environment variable is not set\n")
		os.Exit(1)
	}

	// Determine endpoint
	var endpointURL string
	if useEnvEndpoint || endpoint == "" {
		// Build endpoint from environment variables
		proxyHost := os.Getenv("AGENTAPI_PROXY_SERVICE_HOST")
		proxyPort := os.Getenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP")

		if proxyHost == "" || proxyPort == "" {
			fmt.Fprintf(os.Stderr, "Error: AGENTAPI_PROXY_SERVICE_HOST or AGENTAPI_PROXY_SERVICE_PORT_HTTP environment variable is not set\n")
			os.Exit(1)
		}

		endpointURL = fmt.Sprintf("http://%s:%s", proxyHost, proxyPort)
	} else {
		endpointURL = endpoint
	}

	// Show confirmation unless --confirm flag is set
	if !confirmDelete {
		fmt.Printf("Are you sure you want to delete session %s? [y/N]: ", envSessionID)
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

	// Create client with authentication
	c := client.NewClientWithAuth(endpointURL, apiKey)
	ctx := context.Background()

	// Delete session
	resp, err := c.DeleteSession(ctx, envSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session deleted successfully: %s\n", resp.Message)
	fmt.Printf("Session ID: %s\n", resp.SessionID)
}
