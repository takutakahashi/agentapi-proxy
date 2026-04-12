// Package acp provides types and a client for the Agent Client Protocol (ACP).
// Spec: https://github.com/agentclientprotocol/agent-client-protocol
package acp

import "time"

// ----------------------------------------------------------------------------
// Capabilities
// ----------------------------------------------------------------------------

// ClientCapabilities describes what the client supports.
type ClientCapabilities struct {
	// Filesystem access (fs/read_text_file, fs/write_text_file)
	Filesystem *FilesystemCapability `json:"filesystem,omitempty"`
	// Terminal management
	Terminal *TerminalCapability `json:"terminal,omitempty"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	// Session restore / load
	SessionLoad bool `json:"sessionLoad,omitempty"`
	// Image content blocks in prompts
	Image bool `json:"image,omitempty"`
	// Audio content blocks in prompts
	Audio bool `json:"audio,omitempty"`
}

// FilesystemCapability indicates the client can handle fs requests.
type FilesystemCapability struct {
	Enabled bool `json:"enabled"`
}

// TerminalCapability indicates the client can handle terminal requests.
type TerminalCapability struct {
	Enabled bool `json:"enabled"`
}

// ----------------------------------------------------------------------------
// Initialization
// ----------------------------------------------------------------------------

// InitializeParams is the params for the "initialize" request (client→agent).
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
}

// InitializeResult is the response to "initialize".
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
}

// ----------------------------------------------------------------------------
// Session management
// ----------------------------------------------------------------------------

// McpServer describes an MCP server to connect to.
type McpServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// SessionNewParams is the params for "session/new" (client→agent).
type SessionNewParams struct {
	Cwd        string      `json:"cwd"`
	McpServers []McpServer `json:"mcpServers"`
}

// Mode is an agent operating mode.
type Mode struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is a runtime config option offered by the agent.
type ConfigOption struct {
	Key         string      `json:"key"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// SessionNewResult is the response to "session/new".
type SessionNewResult struct {
	SessionId     string         `json:"sessionId"`
	Modes         []Mode         `json:"modes,omitempty"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
}

// SessionInfo describes a session returned by "session/list".
type SessionInfo struct {
	SessionId string    `json:"sessionId"`
	Cwd       string    `json:"cwd"`
	Title     string    `json:"title,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// SessionListResult is the response to "session/list".
type SessionListResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionSetModeParams is the params for "session/set_mode".
type SessionSetModeParams struct {
	SessionId string `json:"sessionId"`
	Mode      string `json:"mode"`
}

// ----------------------------------------------------------------------------
// Prompt
// ----------------------------------------------------------------------------

// ContentBlockType distinguishes prompt content blocks.
type ContentBlockType string

const (
	ContentBlockTypeText         ContentBlockType = "text"
	ContentBlockTypeImage        ContentBlockType = "image"
	ContentBlockTypeAudio        ContentBlockType = "audio"
	ContentBlockTypeResource     ContentBlockType = "resource"
	ContentBlockTypeResourceLink ContentBlockType = "resource_link"
)

// ContentBlock is a single element of a prompt.
type ContentBlock struct {
	Type ContentBlockType `json:"type"`

	// type=text
	Text string `json:"text,omitempty"`

	// type=image
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // base64

	// type=resource_link
	URI   string `json:"uri,omitempty"`
	Title string `json:"title,omitempty"`
}

// PromptParams is the params for "session/prompt" (client→agent).
type PromptParams struct {
	SessionId string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// StopReason describes why an agent turn ended.
type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonRefusal         StopReason = "refusal"
	StopReasonCancelled       StopReason = "cancelled"
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
)

// PromptResult is the response to "session/prompt".
type PromptResult struct {
	StopReason StopReason `json:"stopReason"`
}

// SessionCancelParams is the params for "session/cancel" notification.
type SessionCancelParams struct {
	SessionId string `json:"sessionId"`
}

// ----------------------------------------------------------------------------
// Session updates (notifications: session/update)
// ----------------------------------------------------------------------------

// SessionUpdateKind is the discriminant for SessionUpdate.
type SessionUpdateKind string

const (
	SessionUpdateKindUserMessageChunk        SessionUpdateKind = "user_message_chunk"
	SessionUpdateKindAgentMessageChunk       SessionUpdateKind = "agent_message_chunk"
	SessionUpdateKindAgentThoughtChunk       SessionUpdateKind = "agent_thought_chunk"
	SessionUpdateKindToolCall                SessionUpdateKind = "tool_call"
	SessionUpdateKindToolCallUpdate          SessionUpdateKind = "tool_call_update"
	SessionUpdateKindPlan                    SessionUpdateKind = "plan"
	SessionUpdateKindAvailableCommandsUpdate SessionUpdateKind = "available_commands_update"
	SessionUpdateKindSessionInfoUpdate       SessionUpdateKind = "session_info_update"
	SessionUpdateKindCurrentModeUpdate       SessionUpdateKind = "current_mode_update"
)

// ToolCallStatus describes the lifecycle state of a tool call.
type ToolCallStatus string

const (
	ToolCallStatusRunning   ToolCallStatus = "running"
	ToolCallStatusSuccess   ToolCallStatus = "success"
	ToolCallStatusError     ToolCallStatus = "error"
	ToolCallStatusCancelled ToolCallStatus = "cancelled"
)

// SessionUpdate is a discriminated union; Kind determines which fields apply.
type SessionUpdate struct {
	// Discriminant field
	Kind SessionUpdateKind `json:"sessionUpdate"`

	// agent_message_chunk / user_message_chunk / agent_thought_chunk
	Content string `json:"content,omitempty"`

	// tool_call
	ToolCallId string      `json:"toolCallId,omitempty"`
	ToolKind   string      `json:"kind,omitempty"` // e.g. "bash", "edit", ...
	RawInput   interface{} `json:"rawInput,omitempty"`

	// tool_call_update
	Status    ToolCallStatus `json:"status,omitempty"`
	RawOutput interface{}    `json:"rawOutput,omitempty"`

	// plan
	Plan string `json:"plan,omitempty"`

	// session_info_update
	Title string `json:"title,omitempty"`

	// current_mode_update
	Mode string `json:"mode,omitempty"`
}

// SessionUpdateNotification is the params for the "session/update" notification
// sent from agent→client.
type SessionUpdateNotification struct {
	SessionId string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// ----------------------------------------------------------------------------
// Permission request (agent→client bidirectional RPC)
// ----------------------------------------------------------------------------

// PermissionOption is one choice the user can make.
type PermissionOption struct {
	Id          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// RequestPermissionParams is the params for "session/request_permission"
// (agent→client request).
type RequestPermissionParams struct {
	SessionId   string             `json:"sessionId"`
	ToolCallId  string             `json:"toolCallId"`
	Description string             `json:"description"`
	Options     []PermissionOption `json:"options"`
}

// RequestPermissionResult is the response to "session/request_permission".
type RequestPermissionResult struct {
	OptionId string `json:"optionId"`
}

// ----------------------------------------------------------------------------
// Filesystem requests (agent→client bidirectional RPC)
// ----------------------------------------------------------------------------

// FsReadTextFileParams is the params for "fs/read_text_file".
type FsReadTextFileParams struct {
	Path string `json:"path"`
}

// FsReadTextFileResult is the result of "fs/read_text_file".
type FsReadTextFileResult struct {
	Text string `json:"text"`
}

// FsWriteTextFileParams is the params for "fs/write_text_file".
type FsWriteTextFileParams struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// FsWriteTextFileResult is the result of "fs/write_text_file".
type FsWriteTextFileResult struct{}
