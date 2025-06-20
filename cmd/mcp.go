package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var (
	mcpPort    int
	proxyURL   string
	mcpVerbose bool
)

type AgentAPIServer struct {
	client *client.Client
}

var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol Server",
	Long:  "Start an MCP server that exposes agentapi-proxy functionality",
	Run:   runMCPServer,
}

func init() {
	MCPCmd.Flags().IntVarP(&mcpPort, "port", "p", 3000, "Port to listen on")
	MCPCmd.Flags().StringVar(&proxyURL, "proxy-url", "http://localhost:8080", "AgentAPI proxy URL")
	MCPCmd.Flags().BoolVarP(&mcpVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runMCPServer(cmd *cobra.Command, args []string) {
	if mcpVerbose {
		log.Printf("Starting MCP server on port %d", mcpPort)
		log.Printf("Proxy URL: %s", proxyURL)
	}

	apiServer := &AgentAPIServer{
		client: client.NewClient(proxyURL),
	}

	mcpServer := server.NewMCPServer(
		"agentapi-proxy-mcp",
		"1.0.0",
	)

	mcpServer.AddTool(&mcp.Tool{
		Name:        "start_session",
		Description: "Start a new agentapi session",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID for the session",
				},
				"environment": map[string]interface{}{
					"type":        "object",
					"description": "Environment variables for the session",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
			},
			Required: []string{"user_id"},
		},
	}, apiServer.handleStartSession)

	mcpServer.AddTool(&mcp.Tool{
		Name:        "search_sessions",
		Description: "Search for active sessions",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by user ID",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by session status",
				},
			},
		},
	}, apiServer.handleSearchSessions)

	mcpServer.AddTool(&mcp.Tool{
		Name:        "send_message",
		Description: "Send a message to an agent session",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to send message to",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message content to send",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Message type (user or raw)",
					"enum":        []string{"user", "raw"},
					"default":     "user",
				},
			},
			Required: []string{"session_id", "message"},
		},
	}, apiServer.handleSendMessage)

	mcpServer.AddTool(&mcp.Tool{
		Name:        "get_messages",
		Description: "Get conversation history from a session",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to get messages from",
				},
			},
			Required: []string{"session_id"},
		},
	}, apiServer.handleGetMessages)

	mcpServer.AddTool(&mcp.Tool{
		Name:        "get_status",
		Description: "Get the status of an agent session",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to get status for",
				},
			},
			Required: []string{"session_id"},
		},
	}, apiServer.handleGetStatus)

	addr := fmt.Sprintf(":%d", mcpPort)
	log.Printf("MCP Server listening on %s", addr)

	if err := mcpServer.Serve(addr); err != nil {
		log.Fatalf("Failed to start MCP server: %v", err)
	}
}

func (s *AgentAPIServer) handleStartSession(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	userID, ok := args["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("user_id is required")
	}

	environment := make(map[string]string)
	if env, ok := args["environment"].(map[string]interface{}); ok {
		for k, v := range env {
			if strVal, ok := v.(string); ok {
				environment[k] = strVal
			}
		}
	}

	// Add user_id as a tag since StartRequest doesn't have UserID field
	tags := map[string]string{
		"user_id": userID,
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req := &client.StartRequest{
		Environment: environment,
		Tags:        tags,
	}

	resp, err := s.client.Start(ctx, req)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Failed to start session: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Session started successfully. Session ID: %s", resp.SessionID),
			},
		},
	}, nil
}

func (s *AgentAPIServer) handleSearchSessions(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	userID, _ := args["user_id"].(string)
	status, _ := args["status"].(string)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Create tags filter for user_id if provided
	var tags map[string]string
	if userID != "" {
		tags = map[string]string{
			"user_id": userID,
		}
	}

	resp, err := s.client.SearchWithTags(ctx, status, tags)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Failed to search sessions: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Found %d sessions:\n", len(resp.Sessions))
	for _, session := range resp.Sessions {
		result += fmt.Sprintf("- Session ID: %s, User: %s, Status: %s, Port: %d, Started: %s\n",
			session.SessionID, session.UserID, session.Status, session.Port, session.StartedAt.Format(time.RFC3339))
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *AgentAPIServer) handleSendMessage(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return nil, fmt.Errorf("session_id is required")
	}

	message, ok := args["message"].(string)
	if !ok {
		return nil, fmt.Errorf("message is required")
	}

	messageType, _ := args["type"].(string)
	if messageType == "" {
		messageType = "user"
	}

	// Use client to send message instead of direct HTTP call
	msg := &client.Message{
		Content: message,
		Type:    messageType,
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.client.SendMessage(ctx, sessionID, msg)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Failed to send message: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Message sent successfully. ID: %s", resp.ID),
			},
		},
	}, nil
}

func (s *AgentAPIServer) handleGetMessages(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return nil, fmt.Errorf("session_id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.client.GetMessages(ctx, sessionID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Failed to get messages: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Conversation History (%d messages):\n", len(resp.Messages))
	for _, msg := range resp.Messages {
		result += fmt.Sprintf("[%s] %s: %s\n",
			msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *AgentAPIServer) handleGetStatus(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return nil, fmt.Errorf("session_id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.client.GetStatus(ctx, sessionID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Failed to get status: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []interface{}{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Agent Status: %s", resp.Status),
			},
		},
	}, nil
}