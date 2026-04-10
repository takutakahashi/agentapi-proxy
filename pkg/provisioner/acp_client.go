package provisioner

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// acpClient is a minimal JSON-RPC 2.0 client over WebSocket for ACP.
type acpClient struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	pending map[int64]chan json.RawMessage
	idSeq   atomic.Int64
	done    chan struct{}
}

type acpMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func dialACP(url string) (*acpClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	c := &acpClient{
		conn:    conn,
		pending: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *acpClient) readLoop() {
	defer close(c.done)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg acpMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- msg.Error
				} else {
					ch <- msg.Result
				}
			}
		}
	}
	// drain pending on disconnect
	c.mu.Lock()
	for _, ch := range c.pending {
		ch <- json.RawMessage(`{"error":"disconnected"}`)
	}
	c.pending = make(map[int64]chan json.RawMessage)
	c.mu.Unlock()
}

func (c *acpClient) call(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	id := c.idSeq.Add(1)
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	msg := acpMsg{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case result := <-ch:
		return result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("ACP call %q timed out after %v", method, timeout)
	case <-c.done:
		return nil, fmt.Errorf("ACP connection closed")
	}
}

func (c *acpClient) close() {
	_ = c.conn.Close()
}
