package cmd

import (
	"context"
	"encoding/json"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var (
	mcpPort     int
	proxyURL    string
	mcpVerbose  bool
)

type MCPRequest struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MCPCapabilities struct {
	Tools     *MCPToolsCapability     `json:"tools,omitempty"`
	Resources *MCPResourcesCapability `json:"resources,omitempty"`
}

type MCPToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type MCPResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type MCPToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type MCPServer struct {
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

	server := &MCPServer{
		client: client.NewClient(proxyURL),
	}

	http.HandleFunc("/", server.handleMCP)
	
	addr := fmt.Sprintf(":%d", mcpPort)
	log.Printf("MCP Server listening on %s", addr)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start MCP server: %v", err)
	}
}

func (s *MCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req MCPRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if mcpVerbose {
		log.Printf("Received MCP request: %s", req.Method)
	}

	var response MCPResponse
	response.ID = req.ID

	switch req.Method {
	case "initialize":
		response.Result = s.handleInitialize()
	case "tools/list":
		response.Result = s.handleToolsList()
	case "tools/call":
		result, err := s.handleToolsCall(req.Params)
		if err != nil {
			response.Error = &MCPError{
				Code:    -32000,
				Message: err.Error(),
			}
		} else {
			response.Result = result
		}
	default:
		response.Error = &MCPError{
			Code:    -32601,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func (s *MCPServer) handleInitialize() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": MCPCapabilities{
			Tools: &MCPToolsCapability{
				ListChanged: false,
			},
		},
		"serverInfo": MCPInfo{
			Name:    "agentapi-proxy-mcp",
			Version: "1.0.0",
		},
	}
}

func (s *MCPServer) handleToolsList() map[string]interface{} {
	tools := []MCPTool{
		{
			Name:        "start_session",
			Description: "Start a new agentapi session",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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
				"required": []string{"user_id"},
			},
		},
		{
			Name:        "search_sessions",
			Description: "Search for active sessions",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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
		},
		{
			Name:        "send_message",
			Description: "Send a message to an agent session",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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
				"required": []string{"session_id", "message"},
			},
		},
		{
			Name:        "get_messages",
			Description: "Get conversation history from a session",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{
						"type":        "string",
						"description": "Session ID to get messages from",
					},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "get_status",
			Description: "Get the status of an agent session",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{
						"type":        "string",
						"description": "Session ID to get status for",
					},
				},
				"required": []string{"session_id"},
			},
		},
	}

	return map[string]interface{}{
		"tools": tools,
	}
}

func (s *MCPServer) handleToolsCall(params interface{}) (MCPToolResult, error) {
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return MCPToolResult{}, fmt.Errorf("invalid params format")
	}

	toolCall, ok := paramsMap["name"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("missing tool name")
	}

	arguments, _ := paramsMap["arguments"].(map[string]interface{})

	switch toolCall {
	case "start_session":
		return s.handleStartSession(arguments)
	case "search_sessions":
		return s.handleSearchSessions(arguments)
	case "send_message":
		return s.handleSendMessage(arguments)
	case "get_messages":
		return s.handleGetMessages(arguments)
	case "get_status":
		return s.handleGetStatus(arguments)
	default:
		return MCPToolResult{}, fmt.Errorf("unknown tool: %s", toolCall)
	}
}

func (s *MCPServer) handleStartSession(args map[string]interface{}) (MCPToolResult, error) {
	userID, ok := args["user_id"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("user_id is required")
	}

	environment := make(map[string]string)
	if env, ok := args["environment"].(map[string]interface{}); ok {
		for k, v := range env {
			if strVal, ok := v.(string); ok {
				environment[k] = strVal
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &client.StartRequest{
		UserID:      userID,
		Environment: environment,
	}

	resp, err := s.client.Start(ctx, req)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to start session: %v", err),
			}},
			IsError: true,
		}, nil
	}

	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("Session started successfully. Session ID: %s", resp.SessionID),
		}},
	}, nil
}

func (s *MCPServer) handleSearchSessions(args map[string]interface{}) (MCPToolResult, error) {
	userID, _ := args["user_id"].(string)
	status, _ := args["status"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := s.client.Search(ctx, userID, status)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to search sessions: %v", err),
			}},
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Found %d sessions:\n", len(resp.Sessions))
	for _, session := range resp.Sessions {
		result += fmt.Sprintf("- Session ID: %s, User: %s, Status: %s, Port: %d, Started: %s\n",
			session.SessionID, session.UserID, session.Status, session.Port, session.StartedAt.Format(time.RFC3339))
	}

	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: result,
		}},
	}, nil
}

func (s *MCPServer) handleSendMessage(args map[string]interface{}) (MCPToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("session_id is required")
	}

	message, ok := args["message"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("message is required")
	}

	messageType, _ := args["type"].(string)
	if messageType == "" {
		messageType = "user"
	}

	url := fmt.Sprintf("%s/%s/message", proxyURL, sessionID)
	
	payload := map[string]interface{}{
		"content": message,
		"type":    messageType,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to marshal message: %v", err),
			}},
			IsError: true,
		}, nil
	}

	resp, err := http.Post(url, "application/json", 
		bytes.NewBuffer(jsonData))
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to send message: %v", err),
			}},
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to read response: %v", err),
			}},
			IsError: true,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(body)),
			}},
			IsError: true,
		}, nil
	}

	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("Message sent successfully: %s", string(body)),
		}},
	}, nil
}

func (s *MCPServer) handleGetMessages(args map[string]interface{}) (MCPToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("session_id is required")
	}

	url := fmt.Sprintf("%s/%s/messages", proxyURL, sessionID)
	
	resp, err := http.Get(url)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to get messages: %v", err),
			}},
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to read response: %v", err),
			}},
			IsError: true,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(body)),
			}},
			IsError: true,
		}, nil
	}

	var messages []MessageResponse
	if err := json.Unmarshal(body, &messages); err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to parse messages: %v", err),
			}},
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Conversation History (%d messages):\n", len(messages))
	for _, msg := range messages {
		result += fmt.Sprintf("[%s] %s: %s\n", 
			msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}

	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: result,
		}},
	}, nil
}

func (s *MCPServer) handleGetStatus(args map[string]interface{}) (MCPToolResult, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok {
		return MCPToolResult{}, fmt.Errorf("session_id is required")
	}

	url := fmt.Sprintf("%s/%s/status", proxyURL, sessionID)
	
	resp, err := http.Get(url)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to get status: %v", err),
			}},
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to read response: %v", err),
			}},
			IsError: true,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(body)),
			}},
			IsError: true,
		}, nil
	}

	var status StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return MCPToolResult{
			Content: []MCPContent{{
				Type: "text",
				Text: fmt.Sprintf("Failed to parse status: %v", err),
			}},
			IsError: true,
		}, nil
	}

	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("Agent Status: %s", status.Status),
		}},
	}, nil
}