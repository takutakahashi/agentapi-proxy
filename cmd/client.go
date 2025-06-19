package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	endpoint  string
	sessionID string
)

type Message struct {
	Content string `json:"content"`
	Type    string `json:"type"`
}

type MessageResponse struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

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

	msg := Message{
		Content: message,
		Type:    "user",
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling message: %v\n", err)
		return
	}

	url := fmt.Sprintf("%s/%s/message", endpoint, sessionID)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP error %d: %s\n", resp.StatusCode, string(body))
		return
	}

	var msgResp MessageResponse
	if err := json.Unmarshal(body, &msgResp); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return
	}

	fmt.Printf("Message sent successfully\nID: %s\nTimestamp: %s\n", msgResp.ID, msgResp.Timestamp.Format(time.RFC3339))
}

func runHistory(cmd *cobra.Command, args []string) {
	url := fmt.Sprintf("%s/%s/messages", endpoint, sessionID)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting history: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP error %d: %s\n", resp.StatusCode, string(body))
		return
	}

	var messages []MessageResponse
	if err := json.Unmarshal(body, &messages); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return
	}

	fmt.Printf("Conversation History (%d messages):\n", len(messages))
	for _, msg := range messages {
		fmt.Printf("[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	url := fmt.Sprintf("%s/%s/status", endpoint, sessionID)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP error %d: %s\n", resp.StatusCode, string(body))
		return
	}

	var status StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return
	}

	fmt.Printf("Agent Status: %s\n", status.Status)
}

func runEvents(cmd *cobra.Command, args []string) {
	url := fmt.Sprintf("%s/%s/events", endpoint, sessionID)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to events: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "HTTP error %d: %s\n", resp.StatusCode, string(body))
		return
	}

	fmt.Println("Monitoring events... (Press Ctrl+C to stop)")
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			fmt.Printf("[EVENT] %s\n", data)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading events: %v\n", err)
	}
}