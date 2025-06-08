package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AgentStatus represents the current status of the agent
type AgentStatus string

const (
	StatusStable  AgentStatus = "stable"
	StatusRunning AgentStatus = "running"
)

// ConversationRole represents the role in a conversation
type ConversationRole string

const (
	RoleUser  ConversationRole = "user"
	RoleAgent ConversationRole = "agent"
)

// MessageType represents the type of message
type MessageType string

const (
	MessageTypeUser MessageType = "user"
	MessageTypeRaw  MessageType = "raw"
)

// Message represents a conversation message
type Message struct {
	ID      int              `json:"id"`
	Content string           `json:"content"`
	Role    ConversationRole `json:"role"`
	Time    string           `json:"time"`
}

// MessageRequest represents a request to send a message
type MessageRequest struct {
	Content string      `json:"content"`
	Type    MessageType `json:"type"`
}

// StatusResponse represents the status response
type StatusResponse struct {
	Status AgentStatus `json:"status"`
	Schema string      `json:"$schema"`
}

// MessagesResponse represents the messages response
type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

// MessageResponse represents the response when sending a message
type MessageResponse struct {
	OK bool `json:"ok"`
}

// ErrorModel represents error responses
type ErrorModel struct {
	Status   int    `json:"status"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Type     string `json:"type"`
	Instance string `json:"instance,omitempty"`
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// MessageUpdateData represents data for message_update events
type MessageUpdateData struct {
	ID      int              `json:"id"`
	Role    ConversationRole `json:"role"`
	Message string           `json:"message"`
	Time    string           `json:"time"`
}

// StatusChangeData represents data for status_change events
type StatusChangeData struct {
	Status AgentStatus `json:"status"`
}

// MockAgentServer represents the mock agentapi server
type MockAgentServer struct {
	status    AgentStatus
	messages  []Message
	nextID    int
	mu        sync.RWMutex
	clients   map[chan SSEEvent]bool
	clientsMu sync.RWMutex
	port      int
	verbose   bool
}

// NewMockAgentServer creates a new mock agent server
func NewMockAgentServer(port int, verbose bool) *MockAgentServer {
	return &MockAgentServer{
		status:   StatusStable,
		messages: make([]Message, 0),
		nextID:   1,
		clients:  make(map[chan SSEEvent]bool),
		port:     port,
		verbose:  verbose,
	}
}

// Log prints a message if verbose mode is enabled
func (s *MockAgentServer) Log(format string, args ...interface{}) {
	if s.verbose {
		log.Printf("[MockAgent:%d] "+format, append([]interface{}{s.port}, args...)...)
	}
}

// addClient adds a new SSE client
func (s *MockAgentServer) addClient(client chan SSEEvent) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[client] = true
	s.Log("Added SSE client, total clients: %d", len(s.clients))
}

// removeClient removes an SSE client
func (s *MockAgentServer) removeClient(client chan SSEEvent) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, client)
	close(client)
	s.Log("Removed SSE client, remaining clients: %d", len(s.clients))
}

// broadcast sends an event to all connected SSE clients
func (s *MockAgentServer) broadcast(event SSEEvent) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for client := range s.clients {
		select {
		case client <- event:
		default:
			// Client buffer full, remove it
			go s.removeClient(client)
		}
	}
}

// getStatus returns the current agent status
func (s *MockAgentServer) getStatus() AgentStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// setStatus sets the agent status and broadcasts the change
func (s *MockAgentServer) setStatus(status AgentStatus) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()

	s.broadcast(SSEEvent{
		Event: "status_change",
		Data:  StatusChangeData{Status: status},
	})
	s.Log("Status changed to: %s", status)
}

// addMessage adds a new message and broadcasts the update
func (s *MockAgentServer) addMessage(content string, role ConversationRole) Message {
	s.mu.Lock()
	message := Message{
		ID:      s.nextID,
		Content: content,
		Role:    role,
		Time:    time.Now().UTC().Format(time.RFC3339),
	}
	s.messages = append(s.messages, message)
	s.nextID++
	s.mu.Unlock()

	s.broadcast(SSEEvent{
		Event: "message_update",
		Data: MessageUpdateData{
			ID:      message.ID,
			Role:    message.Role,
			Message: message.Content,
			Time:    message.Time,
		},
	})
	s.Log("Added message (ID: %d, Role: %s): %s", message.ID, message.Role, content)
	return message
}

// updateLastMessage updates the last message content and broadcasts the update
func (s *MockAgentServer) updateLastMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.messages) == 0 {
		return
	}

	lastMsg := &s.messages[len(s.messages)-1]
	lastMsg.Content = content
	lastMsg.Time = time.Now().UTC().Format(time.RFC3339)

	s.broadcast(SSEEvent{
		Event: "message_update",
		Data: MessageUpdateData{
			ID:      lastMsg.ID,
			Role:    lastMsg.Role,
			Message: lastMsg.Content,
			Time:    lastMsg.Time,
		},
	})
}

// formatContent formats content to 80 characters per line as specified in agentapi
func (s *MockAgentServer) formatContent(content string) string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		for len(line) > 80 {
			lines = append(lines, line[:80])
			line = line[80:]
		}
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// simulateAgentWork simulates agent processing with realistic responses
func (s *MockAgentServer) simulateAgentWork(userMessage string) {
	s.Log("Starting agent simulation for message: %s", userMessage)

	// Change status to running
	s.setStatus(StatusRunning)

	// Add initial agent response
	s.addMessage("I'll help you with that. Let me think...", RoleAgent)

	// Simulate progressive updates to the agent message
	responses := []string{
		"I'll help you with that. Let me think...",
		"I'll help you with that. Let me think... \nAnalyzing your request...",
		"I'll help you with that. Let me think... \nAnalyzing your request...\nProcessing...",
		fmt.Sprintf("I understand you want: %s\n\nHere's how I would approach this:\n1. First, I'd analyze the requirements\n2. Then implement the solution\n3. Finally, test the implementation\n\nThis is a simulated response for e2e testing.", s.formatContent(userMessage)),
	}

	for i, response := range responses {
		time.Sleep(time.Duration(500+i*300) * time.Millisecond)
		s.updateLastMessage(response)
	}

	// Change status back to stable
	time.Sleep(500 * time.Millisecond)
	s.setStatus(StatusStable)
	s.Log("Agent simulation completed")
}

// statusHandler handles GET /status
func (s *MockAgentServer) statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Only GET method is supported")
		return
	}

	response := StatusResponse{
		Status: s.getStatus(),
		Schema: "https://example.com/schemas/StatusResponseBody.json",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.Log("Error encoding status response: %v", err)
	}
	s.Log("Status request: %s", response.Status)
}

// messageHandler handles POST /message
func (s *MockAgentServer) messageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Only POST method is supported")
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid JSON", "Could not parse request body")
		return
	}

	// Check if agent is stable for user messages
	if req.Type == MessageTypeUser && s.getStatus() != StatusStable {
		s.writeError(w, http.StatusConflict, "Agent Busy", "Agent is currently running and cannot accept user messages")
		return
	}

	// Add user message to conversation (if it's a user type message)
	if req.Type == MessageTypeUser {
		s.addMessage(req.Content, RoleUser)

		// Start agent simulation in goroutine
		go s.simulateAgentWork(req.Content)
	} else {
		// Raw messages are not saved to conversation history but logged
		s.Log("Raw message received: %s", req.Content)
	}

	response := MessageResponse{OK: true}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.Log("Error encoding message response: %v", err)
	}
	s.Log("Message received (Type: %s): %s", req.Type, req.Content)
}

// messagesHandler handles GET /messages
func (s *MockAgentServer) messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Only GET method is supported")
		return
	}

	s.mu.RLock()
	messages := make([]Message, len(s.messages))
	copy(messages, s.messages)
	s.mu.RUnlock()

	response := MessagesResponse{Messages: messages}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.Log("Error encoding messages response: %v", err)
	}
	s.Log("Messages request: returning %d messages", len(messages))
}

// eventsHandler handles GET /events (Server-Sent Events)
func (s *MockAgentServer) eventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Only GET method is supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client channel
	client := make(chan SSEEvent, 100)
	s.addClient(client)
	defer s.removeClient(client)

	// Send current state reconstruction events
	s.mu.RLock()
	currentStatus := s.status
	currentMessages := make([]Message, len(s.messages))
	copy(currentMessages, s.messages)
	s.mu.RUnlock()

	// Send current status
	s.writeSSEEvent(w, SSEEvent{
		Event: "status_change",
		Data:  StatusChangeData{Status: currentStatus},
	})

	// Send current messages
	for _, msg := range currentMessages {
		s.writeSSEEvent(w, SSEEvent{
			Event: "message_update",
			Data: MessageUpdateData{
				ID:      msg.ID,
				Role:    msg.Role,
				Message: msg.Content,
				Time:    msg.Time,
			},
		})
	}

	// Flush initial events
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	s.Log("SSE client connected")

	// Listen for events or client disconnect
	for {
		select {
		case event, ok := <-client:
			if !ok {
				return
			}
			s.writeSSEEvent(w, event)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			s.Log("SSE client disconnected")
			return
		}
	}
}

// writeSSEEvent writes a Server-Sent Event to the response writer
func (s *MockAgentServer) writeSSEEvent(w http.ResponseWriter, event SSEEvent) {
	data, _ := json.Marshal(event.Data)
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Event); err != nil {
		s.Log("Error writing SSE event: %v", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", string(data)); err != nil {
		s.Log("Error writing SSE data: %v", err)
	}
}

// writeError writes an error response
func (s *MockAgentServer) writeError(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	error := ErrorModel{
		Status: status,
		Title:  title,
		Detail: detail,
		Type:   "https://example.com/errors",
	}

	if err := json.NewEncoder(w).Encode(error); err != nil {
		s.Log("Error encoding error response: %v", err)
	}
	s.Log("Error response: %d %s - %s", status, title, detail)
}

// Start starts the mock agent server
func (s *MockAgentServer) Start() {
	http.HandleFunc("/status", s.statusHandler)
	http.HandleFunc("/message", s.messageHandler)
	http.HandleFunc("/messages", s.messagesHandler)
	http.HandleFunc("/events", s.eventsHandler)

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintf(w, `{"status": "healthy", "service": "mock-agentapi"}`); err != nil {
			s.Log("Error writing health response: %v", err)
		}
	})

	addr := fmt.Sprintf(":%d", s.port)
	s.Log("Starting mock agentapi server on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func main() {
	// Get port from command line argument or use default
	port := 8080
	verbose := false

	if len(os.Args) > 1 {
		if p, err := strconv.Atoi(os.Args[1]); err == nil {
			port = p
		}
	}

	// Check for verbose flag
	for _, arg := range os.Args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}

	server := NewMockAgentServer(port, verbose)
	server.Start()
}
