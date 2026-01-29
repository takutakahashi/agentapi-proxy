package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var (
	endpoint  string
	sessionID string
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

func init() {
	ClientCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", "", "AgentAPI endpoint URL (required)")
	ClientCmd.PersistentFlags().StringVarP(&sessionID, "session-id", "s", "", "Session ID for the agent (required)")

	if err := ClientCmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		panic(err)
	}
	if err := ClientCmd.MarkPersistentFlagRequired("session-id"); err != nil {
		panic(err)
	}

	ClientCmd.AddCommand(sendCmd)
	ClientCmd.AddCommand(historyCmd)
	ClientCmd.AddCommand(statusCmd)
	ClientCmd.AddCommand(eventsCmd)
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

	fmt.Printf("Message sent successfully\nID: %d\nTimestamp: %s\n", msgResp.ID, msgResp.Timestamp.Format(time.RFC3339))
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
