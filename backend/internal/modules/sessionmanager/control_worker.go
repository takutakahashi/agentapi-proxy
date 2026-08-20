package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

type ControlWorker struct {
	upstreamURL     string
	connectionToken string
	localHMACSecret string
	upstreamAuth    string
	localURL        string
	instanceID      string
	client          *http.Client
	active          sync.Map
	executeSlots    chan struct{}
}

const maxConcurrentControlRequests = 32

func NewControlWorker(upstreamURL, connectionToken, upstreamAuth, localURL, instanceID, localHMACSecret string) *ControlWorker {
	if localURL == "" {
		localURL = "http://127.0.0.1:8080"
	}
	return &ControlWorker{
		upstreamURL: strings.TrimRight(upstreamURL, "/"), connectionToken: connectionToken,
		upstreamAuth: upstreamAuth, localURL: strings.TrimRight(localURL, "/"), instanceID: instanceID,
		localHMACSecret: localHMACSecret,
		client:          &http.Client{},
		executeSlots:    make(chan struct{}, maxConcurrentControlRequests),
	}
}

func (w *ControlWorker) Start(ctx context.Context) {
	cursor := "0-0"
	for ctx.Err() == nil {
		commands, next, err := w.poll(ctx, cursor)
		if err != nil {
			log.Printf("[ESM_CONTROL] command poll failed: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		for _, command := range commands {
			if command.CancelRequestID != "" {
				if value, ok := w.active.Load(command.CancelRequestID); ok {
					value.(context.CancelFunc)()
				}
				_ = w.postFrames(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusNoContent, Done: true, CreatedAt: time.Now().UTC()}})
				continue
			}
			if time.Now().After(command.Deadline) {
				_ = w.postFrames(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusGatewayTimeout, Error: "command deadline exceeded", Done: true, CreatedAt: time.Now().UTC()}})
				continue
			}
			// The control connection is a multiplexed transport. Running ordinary
			// requests serially here turns a page's independent bootstrap requests
			// into a latency waterfall, and a slow request blocks every request behind
			// it. Bound concurrency so unrelated sessions and endpoints can progress
			// independently without allowing an unbounded goroutine fan-out.
			select {
			case w.executeSlots <- struct{}{}:
				go func(command core.Command) {
					defer func() { <-w.executeSlots }()
					w.execute(ctx, command)
				}(command)
			case <-ctx.Done():
				return
			}
		}
		if next != "" {
			cursor = next
		}
	}
}

func (w *ControlWorker) poll(ctx context.Context, cursor string) ([]core.Command, string, error) {
	u, _ := url.Parse(w.upstreamURL + "/internal/external-session-manager/control/commands")
	q := u.Query()
	q.Set("after", cursor)
	q.Set("wait", "30s")
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, cursor, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, cursor, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		Commands   []core.Command `json:"commands"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, cursor, err
	}
	return result.Commands, result.NextCursor, nil
}

func (w *ControlWorker) execute(ctx context.Context, command core.Command) {
	var commandCtx context.Context
	var cancel context.CancelFunc
	if !command.Deadline.IsZero() {
		commandCtx, cancel = context.WithDeadline(ctx, command.Deadline)
	} else {
		commandCtx, cancel = context.WithCancel(ctx)
	}
	w.active.Store(command.ID, context.CancelFunc(cancel))
	defer func() {
		w.active.Delete(command.ID)
		cancel()
	}()
	target := w.localURL + command.Path
	if command.RawQuery != "" {
		target += "?" + command.RawQuery
	}
	req, err := http.NewRequestWithContext(commandCtx, command.Method, target, bytes.NewReader(command.Body))
	if err != nil {
		w.postExecutionError(ctx, command, err)
		return
	}
	req.Header = http.Header(command.Headers).Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	// The local ESM endpoint retains its HMAC middleware as defense in depth.
	ts := hmacutil.NowTimestamp()
	msg := hmacutil.BuildMessage(req.Method, req.URL.RequestURI(), ts, command.Body)
	secret := w.localHMACSecret
	if secret == "" {
		secret = w.connectionToken
	}
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(secret), msg))
	req.Header.Set(hmacutil.TimestampHeader, ts)
	resp, err := w.client.Do(req)
	if err != nil {
		w.postExecutionError(ctx, command, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	sequence := int64(0)
	start := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, Sequence: sequence, Status: resp.StatusCode, Headers: map[string][]string(resp.Header.Clone()), CreatedAt: time.Now().UTC()}
	if acceptsEventStream(command.Headers) || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		start.CommandStreamID = command.StreamID
	}
	if err := w.postFrames(ctx, []core.ResponseFrame{start}); err != nil {
		return
	}
	buffer := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			sequence++
			frame := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, Sequence: sequence, Body: append([]byte(nil), buffer[:n]...), CreatedAt: time.Now().UTC()}
			if err := w.postFrames(ctx, []core.ResponseFrame{frame}); err != nil {
				return
			}
		}
		if readErr != nil {
			sequence++
			frame := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: sequence, Done: true, CreatedAt: time.Now().UTC()}
			if readErr != io.EOF {
				frame.Error = readErr.Error()
			}
			_ = w.postFrames(context.Background(), []core.ResponseFrame{frame})
			return
		}
	}
}

func (w *ControlWorker) postExecutionError(ctx context.Context, command core.Command, err error) {
	_ = w.postFrames(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusBadGateway, Error: err.Error(), Done: true, CreatedAt: time.Now().UTC()}})
}

func (w *ControlWorker) postFrames(ctx context.Context, frames []core.ResponseFrame) error {
	body, _ := json.Marshal(map[string]interface{}{"frames": frames})
	u, _ := url.Parse(w.upstreamURL + "/internal/external-session-manager/control/frames")
	q := u.Query()
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w.authorize(req)
		resp, err := w.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		log.Printf("[ESM_CONTROL] frame upload failed: %v", err)
		sleepOrDone(ctx, 2*time.Second)
	}
	return ctx.Err()
}

func (w *ControlWorker) authorize(req *http.Request) {
	if w.upstreamAuth != "" {
		req.Header.Set("Authorization", "Bearer "+w.upstreamAuth)
		req.Header.Set("X-Session-Manager-Token", w.connectionToken)
		return
	}
	req.Header.Set("Authorization", "Bearer "+w.connectionToken)
}

func acceptsEventStream(headers map[string][]string) bool {
	for key, values := range headers {
		if strings.EqualFold(key, "Accept") {
			for _, value := range values {
				if strings.Contains(strings.ToLower(value), "text/event-stream") {
					return true
				}
			}
		}
	}
	return false
}
