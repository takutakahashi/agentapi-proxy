// Package jsonrpc implements a JSON-RPC 2.0 client over an io.ReadWriter (e.g. stdio).
// It supports bidirectional RPC: both the caller and the remote peer can initiate requests,
// and either side may send notifications (messages without an id).
package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
)

// ----------------------------------------------------------------------------
// Wire types
// ----------------------------------------------------------------------------

// Message is the union type for all JSON-RPC 2.0 messages.
type Message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

// ----------------------------------------------------------------------------
// Handler types
// ----------------------------------------------------------------------------

// RequestHandler handles an inbound request from the remote peer.
// It must return a result (to be serialised) or an error.
type RequestHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// NotificationHandler handles an inbound notification (no response expected).
type NotificationHandler func(params json.RawMessage)

// ----------------------------------------------------------------------------
// Client
// ----------------------------------------------------------------------------

// Client is a bidirectional JSON-RPC 2.0 client that communicates over a
// pair of io.Reader/io.Writer (typically stdin/stdout of a subprocess).
type Client struct {
	encoder *json.Encoder
	decoder *json.Decoder

	mu     sync.Mutex // guards encoder
	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan *Message // id → response channel

	reqHandlers   map[string]RequestHandler
	notifHandlers map[string]NotificationHandler

	verbose bool
}

// New creates a new Client.
func New(r io.Reader, w io.Writer, verbose bool) *Client {
	return &Client{
		encoder:       json.NewEncoder(w),
		decoder:       json.NewDecoder(r),
		pending:       make(map[string]chan *Message),
		reqHandlers:   make(map[string]RequestHandler),
		notifHandlers: make(map[string]NotificationHandler),
		verbose:       verbose,
	}
}

// RegisterRequestHandler registers a handler for inbound requests from the
// remote peer (bidirectional RPC – the peer initiates the call).
func (c *Client) RegisterRequestHandler(method string, h RequestHandler) {
	c.reqHandlers[method] = h
}

// RegisterNotificationHandler registers a handler for inbound notifications.
func (c *Client) RegisterNotificationHandler(method string, h NotificationHandler) {
	c.notifHandlers[method] = h
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	idStr := fmt.Sprintf("%d", id)
	idRaw, _ := json.Marshal(idStr)

	paramBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: marshal params: %w", err)
	}

	msg := Message{
		JSONRPC: "2.0",
		ID:      (*json.RawMessage)(&idRaw),
		Method:  method,
		Params:  paramBytes,
	}

	replyCh := make(chan *Message, 1)
	c.pendingMu.Lock()
	c.pending[idStr] = replyCh
	c.pendingMu.Unlock()

	if err := c.send(msg); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, idStr)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, idStr)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case reply := <-replyCh:
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	}
}

// Notify sends a JSON-RPC notification (no id, no response expected).
func (c *Client) Notify(method string, params interface{}) error {
	paramBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	msg := Message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramBytes,
	}
	return c.send(msg)
}

// Respond sends a JSON-RPC response to a remote request.
func (c *Client) Respond(id json.RawMessage, result interface{}, rpcErr *RPCError) error {
	msg := Message{
		JSONRPC: "2.0",
		ID:      (*json.RawMessage)(&id),
	}
	if rpcErr != nil {
		msg.Error = rpcErr
	} else {
		b, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("jsonrpc: marshal result: %w", err)
		}
		msg.Result = b
	}
	return c.send(msg)
}

func (c *Client) send(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.verbose {
		b, _ := json.Marshal(msg)
		log.Printf("[jsonrpc] --> %s", b)
	}
	return c.encoder.Encode(msg)
}

// Listen starts the receive loop. It blocks until the context is cancelled or
// the underlying reader returns an error (e.g. EOF on process exit).
// Call this in a goroutine.
func (c *Client) Listen(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var msg Message
		if err := c.decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return fmt.Errorf("jsonrpc: connection closed")
			}
			return fmt.Errorf("jsonrpc: decode: %w", err)
		}

		if c.verbose {
			b, _ := json.Marshal(msg)
			log.Printf("[jsonrpc] <-- %s", b)
		}

		c.dispatch(ctx, &msg)
	}
}

func (c *Client) dispatch(ctx context.Context, msg *Message) {
	switch {
	// Response to one of our outgoing calls
	case msg.ID != nil && msg.Method == "":
		idStr := ""
		_ = json.Unmarshal(*msg.ID, &idStr)
		c.pendingMu.Lock()
		ch, ok := c.pending[idStr]
		if ok {
			delete(c.pending, idStr)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- msg
		}

	// Inbound request from the remote peer (bidirectional RPC)
	case msg.ID != nil && msg.Method != "":
		id := *msg.ID
		h, ok := c.reqHandlers[msg.Method]
		if !ok {
			_ = c.Respond(id, nil, &RPCError{
				Code:    -32601,
				Message: "Method not found: " + msg.Method,
			})
			return
		}
		go func() {
			result, err := h(ctx, msg.Params)
			if err != nil {
				_ = c.Respond(id, nil, &RPCError{Code: -32000, Message: err.Error()})
				return
			}
			_ = c.Respond(id, result, nil)
		}()

	// Notification (no id)
	case msg.ID == nil && msg.Method != "":
		if h, ok := c.notifHandlers[msg.Method]; ok {
			go h(msg.Params)
		}
	}
}
