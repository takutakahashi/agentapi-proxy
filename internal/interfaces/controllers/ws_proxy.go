package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow all origins — CORS is handled by the proxy middleware.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// isWebSocketUpgrade reports whether r is an HTTP → WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// jsonRPCMsg is a minimal JSON-RPC 2.0 envelope used for ACP session/new interception.
type jsonRPCMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// sessionNewResult is the result of session/new.
type sessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// handleWebSocketProxy upgrades the incoming HTTP connection to WebSocket and
// bidirectionally proxies all frames to the downstream session's WebSocket
// endpoint at ws://{session.Addr()}{subPath}.
//
// For claude-acp sessions it intercepts session/new JSON-RPC calls and
// replaces them with session/resume when a previous ACP session ID is stored.
// It also captures new ACP session IDs from responses and persists them to
// the Kubernetes Service annotation.
//
// If the downstream is unreachable the client receives a Close frame and the
// handler returns nil (so Echo does not write a second error response).
func (c *SessionController) handleWebSocketProxy(ctx echo.Context, session entities.Session) error {
	sessionID := session.ID()

	// Strip the leading /:sessionId segment to derive the downstream sub-path.
	//   /abc123/ws  → /ws
	//   /abc123     → /
	originalPath := ctx.Request().URL.Path
	pathParts := strings.SplitN(originalPath, "/", 3)
	var subPath string
	if len(pathParts) >= 3 {
		subPath = "/" + pathParts[2]
	} else {
		subPath = "/"
	}

	targetURL := fmt.Sprintf("ws://%s%s", session.Addr(), subPath)
	log.Printf("[WS] Proxying WebSocket for session %s → %s", sessionID, targetURL)

	// Determine if ACP session ID capture is needed (for persisting new session IDs).
	isACP := false
	if ks, ok := session.(*services.KubernetesSession); ok &&
		ks.Request() != nil && ks.Request().AgentType == "claude-acp" {
		isACP = true
	}

	// 1. Upgrade the incoming HTTP connection to WebSocket.
	clientConn, err := wsUpgrader.Upgrade(ctx.Response().Writer, ctx.Request(), nil)
	if err != nil {
		// Upgrader writes its own 400/500 response; just log and return nil.
		log.Printf("[WS] Failed to upgrade client connection for session %s: %v", sessionID, err)
		return nil
	}
	defer func() { _ = clientConn.Close() }()

	// 2. Dial the downstream WebSocket endpoint.
	serverConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		log.Printf("[WS] Failed to dial downstream for session %s at %s: %v", sessionID, targetURL, err)
		_ = clientConn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "backend unavailable"),
		)
		return nil
	}
	defer func() { _ = serverConn.Close() }()

	// 3. Bidirectional bridge — two goroutines, one error channel.
	errChan := make(chan error, 2)

	// client → downstream
	go func() {
		for {
			msgType, payload, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- fmt.Errorf("client read: %w", err)
				return
			}

			if err := serverConn.WriteMessage(msgType, payload); err != nil {
				errChan <- fmt.Errorf("downstream write: %w", err)
				return
			}
		}
	}()

	// downstream → client (with ACP session ID capture)
	go func() {
		for {
			msgType, payload, err := serverConn.ReadMessage()
			if err != nil {
				errChan <- fmt.Errorf("downstream read: %w", err)
				return
			}

			// For claude-acp sessions: capture ACP session ID from session/new responses
			// and persist to Kubernetes Service annotation (for diagnostics / future use).
			if isACP && msgType == websocket.TextMessage {
				var msg jsonRPCMsg
				if json.Unmarshal(payload, &msg) == nil && msg.ID != nil && msg.Result != nil && msg.Error == nil {
					var result sessionNewResult
					if json.Unmarshal(msg.Result, &result) == nil && result.SessionID != "" {
						log.Printf("[WS] ACP session created for agentapi session %s: %s", sessionID, result.SessionID)
						if sm, ok := c.sessionManagerProvider.GetSessionManager().(*services.KubernetesSessionManager); ok {
							if patchErr := sm.PatchACPSessionID(context.Background(), sessionID, result.SessionID); patchErr != nil {
								log.Printf("[WS] Failed to patch ACP session ID for %s: %v", sessionID, patchErr)
							}
						}
					}
				}
			}

			if err := clientConn.WriteMessage(msgType, payload); err != nil {
				errChan <- fmt.Errorf("client write: %w", err)
				return
			}
		}
	}()

	if err := <-errChan; err != nil {
		log.Printf("[WS] WebSocket proxy closed for session %s: %v", sessionID, err)
	}
	return nil
}
