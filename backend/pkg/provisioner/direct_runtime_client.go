package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type directRuntimeWorker struct {
	cfg        *sessionsettings.ParentRuntimeConfig
	client     *http.Client
	localURL   string
	instanceID string
	active     sync.Map
}

var (
	errDirectRuntimeFenced       = errors.New("direct runtime generation fenced")
	errDirectRuntimeUnauthorized = errors.New("direct runtime credential rejected")
)

func runDirectRuntimeClient(ctx context.Context, transport http.RoundTripper, cfg *sessionsettings.ParentRuntimeConfig, instanceID string) {
	if cfg == nil || !cfg.Enabled || cfg.Endpoint == "" || cfg.SessionID == "" || cfg.Token == "" || cfg.Generation <= 0 {
		return
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	port := os.Getenv("AGENTAPI_PORT")
	if port == "" {
		port = "9000"
	}
	w := &directRuntimeWorker{
		cfg: cfg, client: &http.Client{Transport: transport},
		localURL: "http://127.0.0.1:" + port, instanceID: instanceID,
	}
	go w.reportStatus(ctx)
	w.run(ctx)
}

func (w *directRuntimeWorker) reportStatus(ctx context.Context) {
	var previous string
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status := w.localStatus(ctx)
		if status != "" && status != previous {
			if err := w.postStatus(ctx, status); err != nil {
				log.Printf("[DIRECT_RUNTIME] status push failed: %v", err)
			} else {
				previous = status
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *directRuntimeWorker) localStatus(ctx context.Context) string {
	reqCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, w.localURL+"/status", nil)
	if err != nil {
		return ""
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return ""
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return ""
	}
	return result.Status
}

func (w *directRuntimeWorker) postStatus(ctx context.Context, status string) error {
	raw, _ := json.Marshal(map[string]string{"status": status})
	u := strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/status?generation=" + fmt.Sprintf("%d", w.cfg.Generation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status push returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (w *directRuntimeWorker) run(ctx context.Context) {
	cursorPath := filepath.Join(os.TempDir(), "agentapi-direct-runtime-cursor")
	data, _ := os.ReadFile(cursorPath)
	cursor := strings.TrimSpace(string(data))
	if cursor == "" {
		cursor = "0-0"
	}
	backoff := time.Second
	for ctx.Err() == nil {
		requests, next, err := w.poll(ctx, cursor)
		if err != nil {
			if errors.Is(err, errDirectRuntimeFenced) || errors.Is(err, errDirectRuntimeUnauthorized) {
				log.Printf("[DIRECT_RUNTIME] stopping runtime worker: %v", err)
				return
			}
			log.Printf("[DIRECT_RUNTIME] request poll failed: %v", err)
			w.sleepWithJitter(ctx, backoff)
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, request := range requests {
			if request.CancelRequestID != "" {
				if value, ok := w.active.Load(request.CancelRequestID); ok {
					value.(context.CancelFunc)()
				}
				_ = w.postFrames(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: request.ID, CommandStreamID: request.StreamID, Status: http.StatusNoContent, Done: true, CreatedAt: time.Now().UTC()}})
				continue
			}
			// A slow agent endpoint (for example a message request waiting on an
			// upstream model) must not block status, cancellation, or other HTTP
			// traffic for the session.
			go w.execute(ctx, request)
		}
		if next != "" {
			cursor = next
			if err := persistDirectRuntimeCursor(cursorPath, cursor); err != nil {
				log.Printf("[DIRECT_RUNTIME] persist cursor failed: %v", err)
			}
		}
	}
}

func (w *directRuntimeWorker) poll(ctx context.Context, cursor string) ([]core.Command, string, error) {
	pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	u, err := url.Parse(strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/requests")
	if err != nil {
		return nil, cursor, err
	}
	q := u.Query()
	q.Set("after", cursor)
	q.Set("wait", "30s")
	q.Set("count", "100")
	q.Set("generation", fmt.Sprintf("%d", w.cfg.Generation))
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, cursor, err
	}
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, cursor, nil
	}
	if resp.StatusCode == http.StatusConflict {
		return nil, cursor, errDirectRuntimeFenced
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, cursor, errDirectRuntimeUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, cursor, fmt.Errorf("request poll returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Requests   []core.Command `json:"requests"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, cursor, err
	}
	return result.Requests, result.NextCursor, nil
}

func (w *directRuntimeWorker) execute(ctx context.Context, command core.Command) {
	commandCtx, cancel := context.WithCancel(ctx)
	if !command.Deadline.IsZero() {
		commandCtx, cancel = context.WithDeadline(ctx, command.Deadline)
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
	resp, err := w.client.Do(req)
	if err != nil {
		w.postExecutionError(ctx, command, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	sequence := int64(0)
	start := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, Sequence: sequence, Status: resp.StatusCode, Headers: map[string][]string(resp.Header.Clone()), CreatedAt: time.Now().UTC()}
	if acceptsRuntimeEventStream(command.Headers) || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
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
			uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_ = w.postFrames(uploadCtx, []core.ResponseFrame{frame})
			uploadCancel()
			return
		}
	}
}

func (w *directRuntimeWorker) postExecutionError(ctx context.Context, command core.Command, err error) {
	_ = w.postFrames(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusBadGateway, Error: err.Error(), Done: true, CreatedAt: time.Now().UTC()}})
}

func (w *directRuntimeWorker) postFrames(ctx context.Context, frames []core.ResponseFrame) error {
	body, err := json.Marshal(map[string]interface{}{"frames": frames})
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/frames")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("generation", fmt.Sprintf("%d", w.cfg.Generation))
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	backoff := time.Second
	for ctx.Err() == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodPost, u.String(), bytes.NewReader(body))
		if reqErr != nil {
			cancel()
			return reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		w.authorize(req)
		resp, doErr := w.client.Do(req)
		if doErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				cancel()
				return nil
			}
			doErr = fmt.Errorf("frame upload returned HTTP %d", resp.StatusCode)
		}
		cancel()
		log.Printf("[DIRECT_RUNTIME] frame upload failed: %v", doErr)
		w.sleepWithJitter(ctx, backoff)
		backoff = minDuration(backoff*2, 30*time.Second)
	}
	return ctx.Err()
}

func (w *directRuntimeWorker) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
}

func (w *directRuntimeWorker) sleepWithJitter(ctx context.Context, max time.Duration) {
	if max <= 0 {
		return
	}
	delay := time.Duration(rand.Int63n(int64(max)))
	sleepOrDone(ctx, delay)
}

func persistDirectRuntimeCursor(path, cursor string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(cursor+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func acceptsRuntimeEventStream(headers map[string][]string) bool {
	for key, values := range headers {
		if !strings.EqualFold(key, "Accept") {
			continue
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), "text/event-stream") {
				return true
			}
		}
	}
	return false
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
