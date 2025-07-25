package presenters

import (
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"net/http"
	"time"
)

// SessionPresenter defines the interface for presenting session data
type SessionPresenter interface {
	PresentCreateSession(w http.ResponseWriter, response *session.CreateSessionResponse)
	PresentSession(w http.ResponseWriter, session *entities.Session)
	PresentSessionList(w http.ResponseWriter, response *session.ListSessionsResponse)
	PresentDeleteSession(w http.ResponseWriter, response *session.DeleteSessionResponse)
	PresentMonitorSession(w http.ResponseWriter, response *session.MonitorSessionResponse)
	PresentError(w http.ResponseWriter, message string, statusCode int)
}

// HTTPSessionPresenter implements SessionPresenter for HTTP responses
type HTTPSessionPresenter struct{}

// NewHTTPSessionPresenter creates a new HTTPSessionPresenter
func NewHTTPSessionPresenter() *HTTPSessionPresenter {
	return &HTTPSessionPresenter{}
}

// SessionResponse represents a session in HTTP responses
type SessionResponse struct {
	ID          string               `json:"id"`
	UserID      string               `json:"user_id"`
	Port        int                  `json:"port"`
	Status      string               `json:"status"`
	StartedAt   string               `json:"started_at"`
	Environment map[string]string    `json:"environment"`
	Tags        map[string]string    `json:"tags"`
	Repository  *RepositoryResponse  `json:"repository,omitempty"`
	ProcessInfo *ProcessInfoResponse `json:"process_info,omitempty"`
	URL         string               `json:"url,omitempty"`
}

// RepositoryResponse represents repository information in HTTP responses
type RepositoryResponse struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
}

// ProcessInfoResponse represents process information in HTTP responses
type ProcessInfoResponse struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// CreateSessionResponse represents the response for creating a session
type CreateSessionResponse struct {
	Session *SessionResponse `json:"session"`
	URL     string           `json:"url"`
}

// SessionListResponse represents the response for listing sessions
type SessionListResponse struct {
	Sessions   []*SessionResponse `json:"sessions"`
	TotalCount int                `json:"total_count"`
	HasMore    bool               `json:"has_more"`
}

// DeleteSessionResponse represents the response for deleting a session
type DeleteSessionResponse struct {
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
}

// MonitorSessionResponse represents the response for monitoring a session
type MonitorSessionResponse struct {
	Session     *SessionResponse     `json:"session"`
	HealthCheck *HealthCheckResponse `json:"health_check"`
	Updated     bool                 `json:"updated"`
}

// HealthCheckResponse represents health check results in HTTP responses
type HealthCheckResponse struct {
	ProcessStatus string  `json:"process_status"`
	IsReachable   bool    `json:"is_reachable"`
	ResponseTime  *int64  `json:"response_time_ms,omitempty"`
	Error         *string `json:"error,omitempty"`
	CheckedAt     string  `json:"checked_at"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// PresentCreateSession presents a create session response
func (p *HTTPSessionPresenter) PresentCreateSession(w http.ResponseWriter, response *session.CreateSessionResponse) {
	sessionResp := p.convertSessionToResponse(response.Session)

	createResp := &CreateSessionResponse{
		Session: sessionResp,
		URL:     response.URL,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createResp)
}

// PresentSession presents a single session
func (p *HTTPSessionPresenter) PresentSession(w http.ResponseWriter, session *entities.Session) {
	sessionResp := p.convertSessionToResponse(session)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(sessionResp)
}

// PresentSessionList presents a list of sessions
func (p *HTTPSessionPresenter) PresentSessionList(w http.ResponseWriter, response *session.ListSessionsResponse) {
	sessionResps := make([]*SessionResponse, len(response.Sessions))
	for i, sess := range response.Sessions {
		sessionResps[i] = p.convertSessionToResponse(sess)
	}

	listResp := &SessionListResponse{
		Sessions:   sessionResps,
		TotalCount: response.TotalCount,
		HasMore:    response.HasMore,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(listResp)
}

// PresentDeleteSession presents a delete session response
func (p *HTTPSessionPresenter) PresentDeleteSession(w http.ResponseWriter, response *session.DeleteSessionResponse) {
	deleteResp := &DeleteSessionResponse{
		SessionID: string(response.SessionID),
		Success:   response.Success,
		Message:   response.Message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(deleteResp)
}

// PresentMonitorSession presents a monitor session response
func (p *HTTPSessionPresenter) PresentMonitorSession(w http.ResponseWriter, response *session.MonitorSessionResponse) {
	sessionResp := p.convertSessionToResponse(response.Session)

	var healthCheckResp *HealthCheckResponse
	if response.HealthCheck != nil {
		healthCheckResp = &HealthCheckResponse{
			ProcessStatus: string(response.HealthCheck.ProcessStatus),
			IsReachable:   response.HealthCheck.IsReachable,
			CheckedAt:     response.HealthCheck.CheckedAt.Format(time.RFC3339),
		}

		if response.HealthCheck.ResponseTime != nil {
			responseTimeMs := response.HealthCheck.ResponseTime.Nanoseconds() / int64(time.Millisecond)
			healthCheckResp.ResponseTime = &responseTimeMs
		}

		if response.HealthCheck.Error != nil {
			errorMsg := response.HealthCheck.Error.Error()
			healthCheckResp.Error = &errorMsg
		}
	}

	monitorResp := &MonitorSessionResponse{
		Session:     sessionResp,
		HealthCheck: healthCheckResp,
		Updated:     response.Updated,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(monitorResp)
}

// PresentError presents an error response
func (p *HTTPSessionPresenter) PresentError(w http.ResponseWriter, message string, statusCode int) {
	errorResp := &ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errorResp)
}

// convertSessionToResponse converts a domain session to HTTP response format
func (p *HTTPSessionPresenter) convertSessionToResponse(session *entities.Session) *SessionResponse {
	resp := &SessionResponse{
		ID:          string(session.ID()),
		UserID:      string(session.UserID()),
		Port:        int(session.Port()),
		Status:      string(session.Status()),
		StartedAt:   session.StartedAt().Format(time.RFC3339),
		Environment: map[string]string(session.Environment()),
		Tags:        map[string]string(session.Tags()),
	}

	// Convert repository if present
	if repo := session.Repository(); repo != nil {
		resp.Repository = &RepositoryResponse{
			URL:    string(repo.URL()),
			Branch: repo.Branch(),
		}
	}

	// Convert process info if present
	if processInfo := session.ProcessInfo(); processInfo != nil {
		resp.ProcessInfo = &ProcessInfoResponse{
			PID:       processInfo.PID(),
			StartedAt: processInfo.StartedAt().Format(time.RFC3339),
		}
	}

	return resp
}
