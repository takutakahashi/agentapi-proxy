package esmcontrol

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

type Tunnel struct{ store core.Store }

func NewTunnel(store core.Store) *Tunnel { return &Tunnel{store: store} }

func (t *Tunnel) IsConnected(ctx context.Context, managerID string) bool {
	if t == nil || t.store == nil || managerID == "" {
		return false
	}
	ok, err := t.store.IsManagerConnected(ctx, managerID)
	return err == nil && ok
}

func (t *Tunnel) Do(ctx context.Context, managerID, sessionID, remoteSessionID string, req *http.Request) (*http.Response, error) {
	if !t.IsConnected(ctx, managerID) {
		return nil, fmt.Errorf("external session manager %s has no outbound control lease", managerID)
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	deadline := time.Now().Add(30 * time.Minute)
	if value, ok := ctx.Deadline(); ok {
		deadline = value
	}
	command := core.Command{
		ID: uuid.NewString(), ManagerID: managerID, SessionID: sessionID,
		RemoteSessionID: remoteSessionID, Method: req.Method, Path: req.URL.Path,
		RawQuery: req.URL.RawQuery, Headers: cloneHeaders(req.Header), Body: body,
		Deadline: deadline, CreatedAt: time.Now().UTC(),
	}
	if _, err := t.store.EnqueueCommand(ctx, managerID, command); err != nil {
		return nil, err
	}

	frames, cursor, err := t.waitForFirstFrame(ctx, command.ID)
	if err != nil {
		return nil, err
	}
	first := frames[0]
	if first.Error != "" {
		return nil, fmt.Errorf("external session manager: %s", first.Error)
	}
	pipeReader, pipeWriter := io.Pipe()
	go t.streamBody(ctx, command.ID, cursor, frames, pipeWriter)
	return &http.Response{
		StatusCode: first.Status, Status: fmt.Sprintf("%d %s", first.Status, http.StatusText(first.Status)),
		Header: http.Header(first.Headers), Body: &cancelBody{ReadCloser: pipeReader, cancel: func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = t.store.EnqueueCommand(cancelCtx, managerID, core.Command{
				ID: uuid.NewString(), CancelRequestID: command.ID, ManagerID: managerID,
				Deadline: time.Now().Add(30 * time.Second), CreatedAt: time.Now().UTC(),
			})
		}}, ContentLength: -1, Request: req,
	}, nil
}

type cancelBody struct {
	io.ReadCloser
	once   sync.Once
	cancel func()
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

func (t *Tunnel) waitForFirstFrame(ctx context.Context, requestID string) ([]core.ResponseFrame, string, error) {
	cursor := "0-0"
	for {
		frames, err := t.store.ReadFrames(ctx, requestID, cursor, 30*time.Second, 100)
		if err != nil {
			return nil, cursor, err
		}
		if len(frames) > 0 {
			return frames, frames[len(frames)-1].StreamID, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, cursor, err
		}
	}
}

func (t *Tunnel) streamBody(ctx context.Context, requestID, cursor string, initial []core.ResponseFrame, writer *io.PipeWriter) {
	defer func() { _ = writer.Close() }()
	if writeFrames(writer, initial) {
		return
	}
	for {
		frames, err := t.store.ReadFrames(ctx, requestID, cursor, 30*time.Second, 100)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if len(frames) > 0 {
			cursor = frames[len(frames)-1].StreamID
			if writeFrames(writer, frames) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
	}
}

func writeFrames(writer io.Writer, frames []core.ResponseFrame) bool {
	for _, frame := range frames {
		if len(frame.Body) > 0 {
			_, _ = io.Copy(writer, bytes.NewReader(frame.Body))
		}
		if frame.Done {
			return true
		}
	}
	return false
}

func cloneHeaders(source http.Header) map[string][]string {
	result := make(map[string][]string, len(source))
	for key, values := range source {
		switch http.CanonicalHeaderKey(key) {
		case "Authorization", "Cookie", "X-Api-Key", "X-Hub-Signature-256", "X-Timestamp", "X-Session-Manager-Token":
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}
