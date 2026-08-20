package controlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// SessionManager is a worker-side port that delegates every session operation
// to the control API. It has no Kubernetes dependency.
type SessionManager struct {
	baseURL, token string
	client         *http.Client
}

type sessionInfo struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Scope         entities.ResourceScope `json:"scope"`
	TeamID        string                 `json:"team_id"`
	Tags          map[string]string      `json:"tags"`
	Status        string                 `json:"status"`
	StartedAt     time.Time              `json:"started_at"`
	LastMessageAt time.Time              `json:"last_message_at"`
}

func NewSessionManager(baseURL, token string) *SessionManager {
	return &SessionManager{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 35 * time.Second}}
}

func (m *SessionManager) CreateSession(ctx context.Context, id string, request *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	var info sessionInfo
	if err := m.do(ctx, http.MethodPost, "/internal/worker/sessions/"+url.PathEscape(id), request, &info); err != nil {
		return nil, err
	}
	return info.entity(), nil
}
func (m *SessionManager) GetSession(id string) entities.Session {
	sessions := m.ListSessions(entities.SessionFilter{})
	for _, session := range sessions {
		if session.ID() == id {
			return session
		}
	}
	return nil
}
func (m *SessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	result, _ := m.ListSessionsContext(context.Background(), filter)
	return result
}
func (m *SessionManager) ListSessionsContext(ctx context.Context, filter entities.SessionFilter) ([]entities.Session, error) {
	var infos []sessionInfo
	if err := m.do(ctx, http.MethodGet, "/internal/worker/sessions", nil, &infos); err != nil {
		return nil, err
	}
	result := make([]entities.Session, 0, len(infos))
	for _, info := range infos {
		session := info.entity()
		if matches(session, filter) {
			result = append(result, session)
		}
	}
	return result, nil
}
func (m *SessionManager) DeleteSession(id string) error {
	return m.do(context.Background(), http.MethodDelete, "/internal/worker/sessions/"+url.PathEscape(id), nil, nil)
}
func (m *SessionManager) SendMessage(ctx context.Context, id, message string) error {
	return m.do(ctx, http.MethodPost, "/internal/worker/sessions/"+url.PathEscape(id)+"/messages", map[string]string{"message": message}, nil)
}
func (m *SessionManager) StopAgent(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, "/internal/worker/sessions/"+url.PathEscape(id)+"/stop", nil, nil)
}
func (m *SessionManager) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return nil, fmt.Errorf("messages are not supported by worker control API")
}
func (m *SessionManager) Shutdown(time.Duration) error { return nil }
func (m *SessionManager) CreateStockSession(ctx context.Context, dind bool) error {
	return m.do(ctx, http.MethodPost, "/internal/worker/stock?dind="+strconv.FormatBool(dind), nil, nil)
}
func (m *SessionManager) CountStockSessions(ctx context.Context, dind bool) (int, error) {
	var result struct {
		Count int `json:"count"`
	}
	err := m.do(ctx, http.MethodGet, "/internal/worker/stock?dind="+strconv.FormatBool(dind), nil, &result)
	return result.Count, err
}
func (m *SessionManager) PurgeStaleStockSessions(ctx context.Context) error {
	return m.do(ctx, http.MethodDelete, "/internal/worker/stock", nil, nil)
}

func (m *SessionManager) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("control API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if output != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	return nil
}

func (i sessionInfo) entity() entities.Session {
	session := entities.NewProxySessionWithStatus(i.ID, i.UserID, i.Scope, i.TeamID, i.Tags, i.StartedAt, i.Status)
	session.SetLastMessageAt(i.LastMessageAt)
	return session
}
func matches(session entities.Session, filter entities.SessionFilter) bool {
	if filter.UserID != "" && session.UserID() != filter.UserID {
		return false
	}
	if filter.Status != "" && session.Status() != filter.Status {
		return false
	}
	if filter.Scope != "" && session.Scope() != filter.Scope {
		return false
	}
	if filter.TeamID != "" && session.TeamID() != filter.TeamID {
		return false
	}
	for key, value := range filter.Tags {
		if session.Tags()[key] != value {
			return false
		}
	}
	return true
}
