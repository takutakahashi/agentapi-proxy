package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp/jsonrpc"
)

// ProtocolVersion is the ACP version this client targets.
// Per spec: uint16, only bumped for breaking changes.
const ProtocolVersion = 0

// PermissionRequest is an inbound permission request from the agent.
type PermissionRequest struct {
	Params RequestPermissionParams
	// Reply must be called exactly once to unblock the agent.
	Reply func(optionId string) error
}

// Client is a high-level ACP client backed by a JSON-RPC 2.0 connection.
// It manages the lifecycle of a single ACP session.
type Client struct {
	rpc     *jsonrpc.Client
	verbose bool

	sessionId string

	// updateCh is closed when the client is stopped.
	updateCh chan SessionUpdate
	// permCh receives inbound permission requests from the agent.
	permCh chan PermissionRequest
}

// NewClient creates an ACP client using the given reader (agent stdout) and
// writer (agent stdin).
func NewClient(r io.Reader, w io.Writer, verbose bool) *Client {
	c := &Client{
		rpc:      jsonrpc.New(r, w, verbose),
		verbose:  verbose,
		updateCh: make(chan SessionUpdate, 64),
		permCh:   make(chan PermissionRequest, 8),
	}
	c.registerHandlers()
	return c
}

// registerHandlers wires up the inbound handlers before Listen is called.
func (c *Client) registerHandlers() {
	// session/update notifications (agent→client)
	c.rpc.RegisterNotificationHandler("session/update", func(raw json.RawMessage) {
		var n SessionUpdateNotification
		if err := json.Unmarshal(raw, &n); err != nil {
			log.Printf("[acp] session/update parse error: %v", err)
			return
		}
		select {
		case c.updateCh <- n.Update:
		default:
			log.Printf("[acp] updateCh full, dropping update kind=%s", n.Update.Kind)
		}
	})

	// session/request_permission (agent→client, bidirectional RPC)
	c.rpc.RegisterRequestHandler("session/request_permission", func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
		var p RequestPermissionParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("parse request_permission: %w", err)
		}

		// We use a reply channel so the bridge can asynchronously respond.
		replyCh := make(chan string, 1)
		req := PermissionRequest{
			Params: p,
			Reply: func(optionId string) error {
				replyCh <- optionId
				return nil
			},
		}

		select {
		case c.permCh <- req:
		default:
			// If nobody is listening, auto-approve with first option.
			if len(p.Options) > 0 {
				return RequestPermissionResult{
					Outcome: RequestPermissionOutcome{
						Outcome:  "selected",
						OptionId: p.Options[0].OptionId,
					},
				}, nil
			}
			return nil, fmt.Errorf("no options available")
		}

		// Wait for the reply (or context cancellation).
		select {
		case <-ctx.Done():
			return RequestPermissionResult{
				Outcome: RequestPermissionOutcome{Outcome: "cancelled"},
			}, nil
		case optionId := <-replyCh:
			return RequestPermissionResult{
				Outcome: RequestPermissionOutcome{
					Outcome:  "selected",
					OptionId: optionId,
				},
			}, nil
		}
	})

	// fs/read_text_file (agent→client, bidirectional RPC)
	c.rpc.RegisterRequestHandler("fs/read_text_file", func(_ context.Context, raw json.RawMessage) (interface{}, error) {
		var p FsReadTextFileParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("parse fs/read_text_file: %w", err)
		}
		data, err := os.ReadFile(p.Path)
		if err != nil {
			return nil, fmt.Errorf("read file %s: %w", p.Path, err)
		}
		return FsReadTextFileResult{Text: string(data)}, nil
	})

	// fs/write_text_file (agent→client, bidirectional RPC)
	c.rpc.RegisterRequestHandler("fs/write_text_file", func(_ context.Context, raw json.RawMessage) (interface{}, error) {
		var p FsWriteTextFileParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("parse fs/write_text_file: %w", err)
		}
		if err := os.WriteFile(p.Path, []byte(p.Text), 0644); err != nil {
			return nil, fmt.Errorf("write file %s: %w", p.Path, err)
		}
		return FsWriteTextFileResult{}, nil
	})
}

// Listen starts the read loop. It blocks until the context is cancelled or
// the underlying connection closes. Call this in a goroutine.
func (c *Client) Listen(ctx context.Context) error {
	err := c.rpc.Listen(ctx)
	close(c.updateCh)
	return err
}

// Initialize performs the ACP handshake. Must be called before any other method.
func (c *Client) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientCapabilities: ClientCapabilities{
			Filesystem: &FilesystemCapability{Enabled: true},
		},
	}
	raw, err := c.rpc.Call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("acp initialize: %w", err)
	}
	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("acp initialize: parse result: %w", err)
	}
	if c.verbose {
		log.Printf("[acp] initialized: protocol=%d agentCaps=%+v", result.ProtocolVersion, result.AgentCapabilities)
	}
	return nil
}

// NewSession creates a new ACP session and stores the session ID.
func (c *Client) NewSession(ctx context.Context, cwd string, mcpServers []McpServer) error {
	if mcpServers == nil {
		mcpServers = []McpServer{}
	}
	params := SessionNewParams{
		Cwd:        cwd,
		McpServers: mcpServers,
	}
	raw, err := c.rpc.Call(ctx, "session/new", params)
	if err != nil {
		return fmt.Errorf("acp session/new: %w", err)
	}
	var result SessionNewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("acp session/new: parse result: %w", err)
	}
	c.sessionId = result.SessionId
	if c.verbose {
		log.Printf("[acp] session created: id=%s modes=%v", result.SessionId, result.Modes)
	}
	return nil
}

// SessionID returns the current session ID (set after NewSession).
func (c *Client) SessionID() string {
	return c.sessionId
}

// Prompt sends a user message and returns when the agent has finished the turn.
// While the agent is working, session/update notifications are dispatched to
// the channel returned by Updates().
func (c *Client) Prompt(ctx context.Context, text string) (StopReason, error) {
	if c.sessionId == "" {
		return "", fmt.Errorf("acp: no active session; call NewSession first")
	}
	params := PromptParams{
		SessionId: c.sessionId,
		Prompt: []ContentBlock{
			{Type: ContentBlockTypeText, Text: text},
		},
	}
	raw, err := c.rpc.Call(ctx, "session/prompt", params)
	if err != nil {
		return StopReasonCancelled, fmt.Errorf("acp session/prompt: %w", err)
	}
	var result PromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("acp session/prompt: parse result: %w", err)
	}
	return result.StopReason, nil
}

// Cancel sends a session/cancel notification to abort the current turn.
func (c *Client) Cancel(ctx context.Context) error {
	if c.sessionId == "" {
		return nil
	}
	return c.rpc.Notify("session/cancel", SessionCancelParams{SessionId: c.sessionId})
}

// Updates returns a channel that receives session/update notifications from
// the agent. The channel is closed when the client stops.
func (c *Client) Updates() <-chan SessionUpdate {
	return c.updateCh
}

// PermissionRequests returns a channel that receives inbound permission
// requests from the agent.
func (c *Client) PermissionRequests() <-chan PermissionRequest {
	return c.permCh
}
