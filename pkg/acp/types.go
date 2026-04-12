// Package acp implements an Agent Client Protocol (ACP) server that bridges
// ACP clients (e.g. code editors) to a locally running claude-agentapi HTTP
// server.
//
// Protocol reference:
//
//	https://github.com/agentclientprotocol/agent-client-protocol
//
// Compatibility target:
//
//	https://github.com/agentclientprotocol/claude-agent-acp
//
// Transport: WebSocket. Each WebSocket text frame carries exactly one
// JSON-RPC-style message (no extra ndjson framing is needed because WebSocket
// frames are already self-delimiting).
package acp

import "encoding/json"

// ProtocolVersion is the ACP protocol version this server implements.
const ProtocolVersion = 1

// ---- JSON-RPC base types ------------------------------------------------

// Message is the universal envelope for all ACP messages.
//
//   - Request:      has Method + Params (+optional ID)
//   - Response:     has ID + Result or Error  (no Method)
//   - Notification: has Method + Params       (no ID)
type Message struct {
	ID     interface{}     `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- ACP method name constants ------------------------------------------

const (
	// Client → Agent
	MethodInitialize     = "initialize"
	MethodAuthenticate   = "authenticate"
	MethodSessionNew     = "session/new"
	MethodSessionLoad    = "session/load"
	MethodSessionPrompt  = "session/prompt"
	MethodSessionCancel  = "session/cancel"
	MethodSessionList    = "session/list"
	MethodSessionSetMode = "session/set_mode"

	// Agent → Client (notifications / requests)
	MethodSessionUpdate      = "session/update"
	MethodSessionRequestPerm = "session/request_permission"
)

// ---- initialize ---------------------------------------------------------

// InitializeParams is the params block for the "initialize" request.
type InitializeParams struct {
	ProtocolVersion    int                 `json:"protocolVersion"`
	ClientCapabilities *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientInfo         *AgentInfo          `json:"clientInfo,omitempty"`
}

// ClientCapabilities describes what the connecting client supports.
type ClientCapabilities struct {
	FS struct {
		ReadTextFile  bool `json:"readTextFile,omitempty"`
		WriteTextFile bool `json:"writeTextFile,omitempty"`
	} `json:"fs,omitempty"`
	Terminal bool `json:"terminal,omitempty"`
}

// InitializeResult is the result for an "initialize" request.
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         *AgentInfo        `json:"agentInfo,omitempty"`
	AuthMethods       []string          `json:"authMethods"`
}

// AgentInfo carries name / version metadata about the agent.
type AgentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// AgentCapabilities describes what the server supports.
type AgentCapabilities struct {
	SessionCapabilities SessionCapabilities `json:"sessionCapabilities"`
}

// SessionCapabilities describes per-session feature availability.
type SessionCapabilities struct {
	List   bool `json:"list,omitempty"`
	Fork   bool `json:"fork,omitempty"`
	Resume bool `json:"resume,omitempty"`
	Close  bool `json:"close,omitempty"`
}

// ---- session/new --------------------------------------------------------

// NewSessionParams is the params block for "session/new".
type NewSessionParams struct {
	CWD        string      `json:"cwd"`
	MCPServers interface{} `json:"mcpServers,omitempty"`
}

// NewSessionResult is the result for "session/new".
type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

// ---- session/prompt -----------------------------------------------------

// PromptParams is the params block for "session/prompt".
type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResult is the result for "session/prompt".
// StopReason values: "end_turn", "max_tokens", "max_turn_requests",
// "refusal", "cancelled".
type PromptResult struct {
	StopReason string `json:"stopReason"`
}

// ContentBlock is a single content element in a prompt or agent message.
type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "resource", "resource_link"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // base64 for image
	MimeType string `json:"mimeType,omitempty"` // for image
	URI      string `json:"uri,omitempty"`      // for resource_link
}

// ---- session/cancel (notification, client → agent) ----------------------

// CancelParams is the params block for the "session/cancel" notification.
type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// ---- session/update (notification, agent → client) ----------------------

// SessionUpdateNotification is the params block for "session/update"
// notifications sent by the agent to the client during prompt processing.
type SessionUpdateNotification struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// SessionUpdate is discriminated by the Type ("sessionUpdate") field.
// Only fields relevant to the active update type are populated.
type SessionUpdate struct {
	// Type maps to the "sessionUpdate" discriminator in the spec.
	Type string `json:"sessionUpdate"`

	// agent_message_chunk / user_message_chunk
	Chunk *ContentBlock `json:"chunk,omitempty"`

	// tool_call / tool_call_update
	ToolCallID string            `json:"toolCallId,omitempty"`
	Title      string            `json:"title,omitempty"`
	Kind       string            `json:"kind,omitempty"`   // "read","edit","execute","search","fetch","think","other"
	Status     string            `json:"status,omitempty"` // "pending","in_progress","completed","failed"
	Content    []ToolCallContent `json:"content,omitempty"`

	// plan
	Plan *PlanUpdate `json:"plan,omitempty"`
}

// ToolCallContent wraps content produced by or about a tool call.
type ToolCallContent struct {
	Type    string         `json:"type"` // "content", "diff", "terminal"
	Content []ContentBlock `json:"content,omitempty"`
}

// PlanUpdate carries the current todo list from a plan-mode agent.
type PlanUpdate struct {
	Items []PlanItem `json:"items"`
}

// PlanItem is one entry in a plan list.
type PlanItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "completed", "in_progress"
}

// ---- session/request_permission (agent → client, requires response) -----

// RequestPermissionParams is the params block for "session/request_permission".
type RequestPermissionParams struct {
	SessionID         string  `json:"sessionId"`
	PermissionRequest PermReq `json:"permissionRequest"`
}

// PermReq describes the pending tool use that needs approval.
type PermReq struct {
	ToolUseID   string         `json:"toolUseId"`
	Description string         `json:"description"`
	Content     []ContentBlock `json:"content,omitempty"`
}

// RequestPermissionResult is the client's reply to a permission request.
// Permission values: "allow_always", "allow", "reject".
type RequestPermissionResult struct {
	Permission string `json:"permission"`
	// Answer is present only for answer_question actions.
	Answer string `json:"answer,omitempty"`
}
