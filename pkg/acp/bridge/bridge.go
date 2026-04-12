// Package bridge translates between the ACP client and an agentapi-compatible
// HTTP interface (compatible with takutakahashi/claude-agentapi).
package bridge

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

// ----------------------------------------------------------------------------
// agentapi-compatible types
// ----------------------------------------------------------------------------

// AgentStatus mirrors coder/agentapi status values.
type AgentStatus string

const (
	AgentStatusRunning AgentStatus = "running"
	AgentStatusStable  AgentStatus = "stable"
)

// MessageRole mirrors takutakahashi/claude-agentapi message roles.
type MessageRole string

const (
	MessageRoleUser       MessageRole = "user"
	MessageRoleAssistant  MessageRole = "assistant"
	MessageRoleAgent      MessageRole = "agent"
	MessageRoleToolResult MessageRole = "tool_result"
)

// MessageType mirrors takutakahashi/claude-agentapi message types.
type MessageType string

const (
	MessageTypeNormal   MessageType = "normal"
	MessageTypeQuestion MessageType = "question"
	MessageTypePlan     MessageType = "plan"
)

// Message is an agentapi-compatible message entry.
type Message struct {
	ID              int64       `json:"id"`
	Role            MessageRole `json:"role"`
	Content         string      `json:"content"`
	Time            time.Time   `json:"time"`
	Type            MessageType `json:"type"`
	ToolUseId       string      `json:"toolUseId,omitempty"`
	ParentToolUseId string      `json:"parentToolUseId,omitempty"`
	Status          string      `json:"status,omitempty"` // "success"|"error"
}

// StatusResponse mirrors coder/agentapi /status response.
type StatusResponse struct {
	AgentType string      `json:"agent_type"`
	Status    AgentStatus `json:"status"`
	Transport string      `json:"transport"`
}

// ----------------------------------------------------------------------------
// SSE events
// ----------------------------------------------------------------------------

// EventType names the SSE event.
type EventType string

const (
	EventTypeMessageUpdate EventType = "message_update"
	EventTypeStatusChange  EventType = "status_change"
	EventTypeAgentError    EventType = "agent_error"
)

// Event is a single SSE event.
type Event struct {
	Type EventType
	Data interface{}
}

// MessageUpdateData is the data for message_update events.
type MessageUpdateData struct {
	ID      int64       `json:"id"`
	Message string      `json:"message"`
	Role    MessageRole `json:"role"`
	Time    time.Time   `json:"time"`
}

// StatusChangeData is the data for status_change events.
type StatusChangeData struct {
	Status    AgentStatus `json:"status"`
	AgentType string      `json:"agent_type"`
}

// AgentErrorData is the data for agent_error events.
type AgentErrorData struct {
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// ----------------------------------------------------------------------------
// Actions (takutakahashi/claude-agentapi extension)
// ----------------------------------------------------------------------------

// ActionType names a pending action.
type ActionType string

const (
	ActionTypeAnswerQuestion ActionType = "answer_question"
	ActionTypeApprovePlan    ActionType = "approve_plan"
	ActionTypeStopAgent      ActionType = "stop_agent"
)

// PendingAction is an action waiting for the HTTP client to respond.
type PendingAction struct {
	Type      ActionType  `json:"type"`
	ToolUseId string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"`
}

// PendingActionsResponse is the body for GET /action.
type PendingActionsResponse struct {
	PendingActions []PendingAction `json:"pending_actions"`
}

// ActionRequest is the body for POST /action.
type ActionRequest struct {
	Type     ActionType        `json:"type"`
	Answers  map[string]string `json:"answers,omitempty"`  // answer_question
	Approved *bool             `json:"approved,omitempty"` // approve_plan
}

// ----------------------------------------------------------------------------
// subscriber
// ----------------------------------------------------------------------------

type subscriber struct {
	ch chan Event
}

// ----------------------------------------------------------------------------
// Bridge
// ----------------------------------------------------------------------------

// Bridge holds all state for bridging an ACP client to the agentapi HTTP interface.
type Bridge struct {
	acp     *acp.Client
	verbose bool

	// serverCtx is the long-lived context from Run(); used for ACP Prompt calls
	// so they survive beyond the HTTP request that triggered them.
	serverCtx context.Context

	mu       sync.RWMutex
	status   AgentStatus
	messages []Message
	nextID   int64

	subsMu sync.Mutex
	subs   []*subscriber

	actionsMu      sync.Mutex
	pendingActions []PendingAction
	actionReplyCh  map[string]chan string // toolUseId → selected optionId
}

// New creates a Bridge backed by the given ACP client.
func New(client *acp.Client, verbose bool) *Bridge {
	return &Bridge{
		acp:           client,
		verbose:       verbose,
		status:        AgentStatusStable,
		actionReplyCh: make(map[string]chan string),
	}
}

// Run starts the background goroutines that consume ACP notifications and
// feed the bridge state. Call this in a goroutine; it blocks until ctx is done.
// The ctx is stored as the server context for use in background operations
// (e.g., ACP Prompt calls that outlive the HTTP request that triggered them).
func (b *Bridge) Run(ctx context.Context) {
	b.serverCtx = ctx
	updates := b.acp.Updates()
	perms := b.acp.PermissionRequests()

	for {
		select {
		case <-ctx.Done():
			return

		case update, ok := <-updates:
			if !ok {
				// ACP client closed – mark error
				b.setStatus(AgentStatusStable)
				b.broadcastError("agent connection closed")
				return
			}
			b.handleUpdate(update)

		case req, ok := <-perms:
			if !ok {
				return
			}
			b.handlePermissionRequest(req)
		}
	}
}

// ----------------------------------------------------------------------------
// Update handling
// ----------------------------------------------------------------------------

func (b *Bridge) handleUpdate(update acp.SessionUpdate) {
	if b.verbose {
		log.Printf("[bridge] update kind=%s content=%s", update.Kind, update.Content)
	}

	switch update.Kind {
	case acp.SessionUpdateKindAgentMessageChunk:
		text := acp.ExtractTextContent(update.Content)
		b.appendOrCreateMessage(MessageRoleAgent, text, MessageTypeNormal, "", "")

	case acp.SessionUpdateKindAgentThoughtChunk:
		// Thoughts are surfaced as assistant messages for transparency.
		text := acp.ExtractTextContent(update.Content)
		b.appendOrCreateMessage(MessageRoleAssistant, text, MessageTypeNormal, "", "")

	case acp.SessionUpdateKindToolCall:
		// New tool invocation – create a distinct message.
		content := fmt.Sprintf("[tool:%s] %v", update.ToolKind, update.RawInput)
		b.addNewMessage(MessageRoleAgent, content, MessageTypeNormal, update.ToolCallId, "")

	case acp.SessionUpdateKindToolCallUpdate:
		// Update existing tool message status.
		statusStr := string(update.Status)
		if update.Status == acp.ToolCallStatusSuccess || update.Status == acp.ToolCallStatusError {
			content := fmt.Sprintf("%v", update.RawOutput)
			b.addNewMessage(MessageRoleToolResult, content, MessageTypeNormal, "", update.ToolCallId)
			_ = statusStr
		}

	case acp.SessionUpdateKindPlan:
		b.addNewMessage(MessageRoleAgent, update.Plan, MessageTypePlan, "", "")
	}
}

// appendOrCreateMessage appends content to the last agent/assistant message if
// it has the same role and is not a tool message; otherwise creates a new one.
func (b *Bridge) appendOrCreateMessage(role MessageRole, content string, msgType MessageType, toolUseId, parentToolUseId string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.messages) > 0 {
		last := &b.messages[len(b.messages)-1]
		if last.Role == role && last.ToolUseId == "" && last.Type == msgType {
			last.Content += content
			last.Time = time.Now()
			b.broadcastMessageUpdateLocked(*last)
			return
		}
	}
	b.nextID++
	msg := Message{
		ID:              b.nextID,
		Role:            role,
		Content:         content,
		Time:            time.Now(),
		Type:            msgType,
		ToolUseId:       toolUseId,
		ParentToolUseId: parentToolUseId,
	}
	b.messages = append(b.messages, msg)
	b.broadcastMessageUpdateLocked(msg)
}

func (b *Bridge) addNewMessage(role MessageRole, content string, msgType MessageType, toolUseId, parentToolUseId string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	msg := Message{
		ID:              b.nextID,
		Role:            role,
		Content:         content,
		Time:            time.Now(),
		Type:            msgType,
		ToolUseId:       toolUseId,
		ParentToolUseId: parentToolUseId,
	}
	b.messages = append(b.messages, msg)
	b.broadcastMessageUpdateLocked(msg)
}

// ----------------------------------------------------------------------------
// Permission request handling
// ----------------------------------------------------------------------------

func (b *Bridge) handlePermissionRequest(req acp.PermissionRequest) {
	p := req.Params

	// Build the "question" message visible in the HTTP conversation.
	questions := make([]map[string]string, 0, len(p.Options))
	for _, opt := range p.Options {
		questions = append(questions, map[string]string{
			"id":          opt.Id,
			"label":       opt.Label,
			"description": opt.Description,
		})
	}
	b.mu.Lock()
	b.nextID++
	qMsg := Message{
		ID:        b.nextID,
		Role:      MessageRoleAgent,
		Content:   p.Description,
		Time:      time.Now(),
		Type:      MessageTypeQuestion,
		ToolUseId: p.ToolCallId,
	}
	b.messages = append(b.messages, qMsg)
	b.broadcastMessageUpdateLocked(qMsg)
	b.mu.Unlock()

	// Register a pending action.
	replyCh := make(chan string, 1)
	b.actionsMu.Lock()
	b.pendingActions = append(b.pendingActions, PendingAction{
		Type:      ActionTypeAnswerQuestion,
		ToolUseId: p.ToolCallId,
		Content: map[string]interface{}{
			"description": p.Description,
			"questions":   questions,
		},
	})
	b.actionReplyCh[p.ToolCallId] = replyCh
	b.actionsMu.Unlock()

	// Wait for the HTTP client to POST /action and provide an answer.
	go func() {
		optionId := <-replyCh
		_ = req.Reply(optionId)
	}()
}

// ----------------------------------------------------------------------------
// Public API (called by HTTP handlers)
// ----------------------------------------------------------------------------

// SendMessage posts a user message to the ACP agent. It sets status to
// "running" immediately and returns; the status reverts to "stable" when
// the agent finishes (via a goroutine watching the Prompt call).
func (b *Bridge) SendMessage(ctx context.Context, content string) error {
	b.mu.Lock()
	if b.status == AgentStatusRunning {
		b.mu.Unlock()
		return fmt.Errorf("agent is busy")
	}
	// Add the user message to history.
	b.nextID++
	userMsg := Message{
		ID:      b.nextID,
		Role:    MessageRoleUser,
		Content: content,
		Time:    time.Now(),
		Type:    MessageTypeNormal,
	}
	b.messages = append(b.messages, userMsg)
	b.broadcastMessageUpdateLocked(userMsg)
	b.status = AgentStatusRunning
	b.mu.Unlock()

	b.broadcast(Event{
		Type: EventTypeStatusChange,
		Data: StatusChangeData{Status: AgentStatusRunning, AgentType: "acp"},
	})

	// Run the prompt in the background using the long-lived server context,
	// NOT the HTTP request context (which is cancelled when the response returns).
	promptCtx := b.serverCtx
	if promptCtx == nil {
		promptCtx = ctx
	}
	go func() {
		stopReason, err := b.acp.Prompt(promptCtx, content)
		if err != nil {
			log.Printf("[bridge] prompt error: %v", err)
			b.broadcastError(err.Error())
		}
		if b.verbose {
			log.Printf("[bridge] prompt done: stopReason=%s", stopReason)
		}
		b.setStatus(AgentStatusStable)
	}()

	return nil
}

// StopAgent cancels the current agent turn.
func (b *Bridge) StopAgent(ctx context.Context) error {
	return b.acp.Cancel(ctx)
}

// GetMessages returns the current conversation history.
func (b *Bridge) GetMessages() []Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Message, len(b.messages))
	copy(result, b.messages)
	return result
}

// GetStatus returns the current agent status.
func (b *Bridge) GetStatus() StatusResponse {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return StatusResponse{
		AgentType: "acp",
		Status:    b.status,
		Transport: "acp",
	}
}

// Subscribe returns a channel that receives SSE events and a cancel function.
// On subscription, the subscriber immediately receives replayed events to
// reconstruct the current state.
func (b *Bridge) Subscribe() (<-chan Event, func()) {
	sub := &subscriber{ch: make(chan Event, 128)}

	b.subsMu.Lock()
	b.subs = append(b.subs, sub)
	b.subsMu.Unlock()

	// Replay all existing messages.
	b.mu.RLock()
	msgs := make([]Message, len(b.messages))
	copy(msgs, b.messages)
	status := b.status
	b.mu.RUnlock()

	go func() {
		for _, msg := range msgs {
			sub.ch <- Event{
				Type: EventTypeMessageUpdate,
				Data: MessageUpdateData{
					ID:      msg.ID,
					Message: msg.Content,
					Role:    msg.Role,
					Time:    msg.Time,
				},
			}
		}
		sub.ch <- Event{
			Type: EventTypeStatusChange,
			Data: StatusChangeData{Status: status, AgentType: "acp"},
		}
	}()

	cancel := func() {
		b.subsMu.Lock()
		defer b.subsMu.Unlock()
		for i, s := range b.subs {
			if s == sub {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		close(sub.ch)
	}
	return sub.ch, cancel
}

// GetPendingActions returns the list of actions waiting for a user response.
func (b *Bridge) GetPendingActions() PendingActionsResponse {
	b.actionsMu.Lock()
	defer b.actionsMu.Unlock()
	result := make([]PendingAction, len(b.pendingActions))
	copy(result, b.pendingActions)
	return PendingActionsResponse{PendingActions: result}
}

// SubmitAction processes a user response to a pending action.
func (b *Bridge) SubmitAction(ctx context.Context, req ActionRequest) error {
	switch req.Type {
	case ActionTypeStopAgent:
		return b.StopAgent(ctx)

	case ActionTypeAnswerQuestion:
		b.actionsMu.Lock()
		defer b.actionsMu.Unlock()

		// req.Answers maps toolUseId → optionId
		for toolUseId, optionId := range req.Answers {
			ch, ok := b.actionReplyCh[toolUseId]
			if !ok {
				return fmt.Errorf("no pending action for tool_use_id %q", toolUseId)
			}
			ch <- optionId
			delete(b.actionReplyCh, toolUseId)
			// Remove from pending list.
			b.removePendingActionLocked(toolUseId)
		}
		return nil

	case ActionTypeApprovePlan:
		// Find the plan action (no specific toolUseId – pick first plan).
		b.actionsMu.Lock()
		defer b.actionsMu.Unlock()
		for i, pa := range b.pendingActions {
			if pa.Type == ActionTypeApprovePlan {
				ch, ok := b.actionReplyCh[pa.ToolUseId]
				if ok {
					if req.Approved != nil && *req.Approved {
						ch <- "approve"
					} else {
						ch <- "reject"
					}
					delete(b.actionReplyCh, pa.ToolUseId)
					b.pendingActions = append(b.pendingActions[:i], b.pendingActions[i+1:]...)
				}
				return nil
			}
		}
		return fmt.Errorf("no pending approve_plan action")

	default:
		return fmt.Errorf("unknown action type: %s", req.Type)
	}
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

func (b *Bridge) setStatus(s AgentStatus) {
	b.mu.Lock()
	b.status = s
	b.mu.Unlock()
	b.broadcast(Event{
		Type: EventTypeStatusChange,
		Data: StatusChangeData{Status: s, AgentType: "acp"},
	})
}

func (b *Bridge) broadcastError(msg string) {
	b.broadcast(Event{
		Type: EventTypeAgentError,
		Data: AgentErrorData{Level: "error", Message: msg, Time: time.Now()},
	})
}

func (b *Bridge) broadcast(e Event) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, sub := range b.subs {
		select {
		case sub.ch <- e:
		default:
		}
	}
}

// broadcastMessageUpdateLocked must be called with b.mu held (at least read).
func (b *Bridge) broadcastMessageUpdateLocked(msg Message) {
	e := Event{
		Type: EventTypeMessageUpdate,
		Data: MessageUpdateData{
			ID:      msg.ID,
			Message: msg.Content,
			Role:    msg.Role,
			Time:    msg.Time,
		},
	}
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, sub := range b.subs {
		select {
		case sub.ch <- e:
		default:
		}
	}
}

func (b *Bridge) removePendingActionLocked(toolUseId string) {
	for i, pa := range b.pendingActions {
		if pa.ToolUseId == toolUseId {
			b.pendingActions = append(b.pendingActions[:i], b.pendingActions[i+1:]...)
			return
		}
	}
}
