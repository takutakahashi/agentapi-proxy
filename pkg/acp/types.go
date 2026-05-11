// Package acp provides types and a client for the Agent Client Protocol (ACP).
// Spec: https://github.com/agentclientprotocol/agent-client-protocol
package acp

import (
	"encoding/json"
	"time"
)

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

// SessionMode is a single mode available in a session.
type SessionMode struct {
	Id          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// SessionModeState is the modes object returned in session/new response.
// Per spec: modes is an object with currentModeId and availableModes, not a flat array.
type SessionModeState struct {
	CurrentModeId  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// ConfigOptionValue is one selectable value in a ConfigOption.
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is a runtime config option offered by the agent.
// Field names match the ACP spec (session/new, session/load, config_option_update).
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"` // "mode", "model", "thought_level", or "_"-prefixed custom
	Type         string              `json:"type"`               // currently only "select"
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options"`
}

// SessionNewResult is the response to "session/new".
type SessionNewResult struct {
	SessionId     string            `json:"sessionId"`
	Modes         *SessionModeState `json:"modes,omitempty"`
	ConfigOptions []ConfigOption    `json:"configOptions,omitempty"`
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

// SessionLoadParams is the params for "session/load" (client→agent).
// Only valid when AgentCapabilities.SessionLoad is true.
type SessionLoadParams struct {
	SessionId string `json:"sessionId"`
}

// SessionLoadResult is the response to "session/load".
type SessionLoadResult struct {
	SessionId     string            `json:"sessionId"`
	Modes         *SessionModeState `json:"modes,omitempty"`
	ConfigOptions []ConfigOption    `json:"configOptions,omitempty"`
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
	SessionUpdateKindConfigOptionUpdate      SessionUpdateKind = "config_option_update"
	SessionUpdateKindSessionInfoUpdate       SessionUpdateKind = "session_info_update"
	SessionUpdateKindCurrentModeUpdate       SessionUpdateKind = "current_mode_update"
)

// ToolCallStatus describes the lifecycle state of a tool call.
type ToolCallStatus string

const (
	ToolCallStatusPending    ToolCallStatus = "pending"
	ToolCallStatusInProgress ToolCallStatus = "in_progress"
	ToolCallStatusCompleted  ToolCallStatus = "completed"
	ToolCallStatusFailed     ToolCallStatus = "failed"
)

// PlanEntry is a single entry in an ACP plan update.
// See ACP spec: sessionUpdate="plan" entries field.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`   // "pending", "in_progress", "completed", "cancelled"
	Priority string `json:"priority"` // "high", "medium", "low"
}

// ToolCallLocation is a file/line reference attached to a tool_call update.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"` // 1-based
}

// AvailableCommand is a single slash command reported in available_commands_update.
type AvailableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionUpdate is a discriminated union; Kind determines which fields apply.
type SessionUpdate struct {
	// Discriminant field
	Kind SessionUpdateKind `json:"sessionUpdate"`

	// agent_message_chunk / user_message_chunk / agent_thought_chunk
	// Content is a ContentBlock object (not a string) per ACP spec.
	Content json.RawMessage `json:"content,omitempty"`

	// tool_call (required: toolCallId, title); Title also carries session_info_update.title
	ToolCallId string             `json:"toolCallId,omitempty"`
	Title      string             `json:"title,omitempty"`
	ToolKind   string             `json:"kind,omitempty"` // "read", "edit", "delete", "move", "search", "execute", "think", "fetch", "other"
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   interface{}        `json:"rawInput,omitempty"`

	// tool_call_update
	Status    ToolCallStatus `json:"status,omitempty"`
	RawOutput interface{}    `json:"rawOutput,omitempty"`

	// plan – ACP sends entries (structured list), not a plain string.
	Entries []PlanEntry `json:"entries,omitempty"`

	// available_commands_update
	Commands []AvailableCommand `json:"commands,omitempty"`

	// config_option_update
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`

	// current_mode_update (deprecated; prefer config_option_update)
	CurrentModeId string `json:"currentModeId,omitempty"`
}

// SessionUpdateNotification is the params for the "session/update" notification
// sent from agent→client.
type SessionUpdateNotification struct {
	SessionId string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// ContentBlockText is the text variant of a ContentBlock (type="text").
type ContentBlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ExtractTextContent extracts the text string from a raw ContentBlock JSON value.
// If the block is not a text block or is invalid, returns an empty string.
func ExtractTextContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var block ContentBlockText
	if err := json.Unmarshal(raw, &block); err != nil {
		return ""
	}
	if block.Type != "text" {
		return ""
	}
	return block.Text
}

// ----------------------------------------------------------------------------
// Permission request (agent→client bidirectional RPC)
// ----------------------------------------------------------------------------

// PermissionOption is one choice the user can make.
// Field names match the ACP spec: optionId (identifier) and name (display label).
type PermissionOption struct {
	Kind     string `json:"kind,omitempty"` // "allow_always", "allow_once", "reject_once"
	Name     string `json:"name"`           // human-readable display label
	OptionId string `json:"optionId"`       // machine identifier sent back in the response
}

// RequestPermissionToolCall contains the tool call details nested in RequestPermissionParams.
type RequestPermissionToolCall struct {
	ToolCallId string          `json:"toolCallId"`
	Kind       string          `json:"kind,omitempty"`     // e.g. "switch_mode" for ExitPlanMode
	RawInput   json.RawMessage `json:"rawInput,omitempty"` // raw tool input JSON
}

// RequestPermissionParams is the params for "session/request_permission"
// (agent→client request).
type RequestPermissionParams struct {
	SessionId string                    `json:"sessionId"`
	Options   []PermissionOption        `json:"options"`
	ToolCall  RequestPermissionToolCall `json:"toolCall"`
}

// RequestPermissionOutcome is the inner outcome object of RequestPermissionResult.
type RequestPermissionOutcome struct {
	Outcome  string `json:"outcome"`            // "selected" or "cancelled"
	OptionId string `json:"optionId,omitempty"` // set when outcome="selected"
}

// RequestPermissionResult is the response to "session/request_permission".
// The ACP spec requires: {outcome: {outcome: "selected", optionId: "<id>"}}
type RequestPermissionResult struct {
	Outcome RequestPermissionOutcome `json:"outcome"`
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
