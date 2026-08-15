package sessionmanagerapi

import (
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// RoutePrefix is the private, versioned API exposed by a session-manager
// process. It is intentionally separate from the public proxy API.
const RoutePrefix = "/internal/session-manager/v1"

// SessionDTO is the transport representation of a session. It contains every
// field the API process needs for authorization, routing, and public response
// rendering without depending on a concrete Kubernetes session type.
type SessionDTO struct {
	ID              string                      `json:"id"`
	Addr            string                      `json:"addr"`
	UserID          string                      `json:"user_id"`
	Scope           entities.ResourceScope      `json:"scope"`
	TeamID          string                      `json:"team_id,omitempty"`
	Tags            map[string]string           `json:"tags"`
	Status          string                      `json:"status"`
	StartedAt       time.Time                   `json:"started_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
	LastMessageAt   time.Time                   `json:"last_message_at"`
	Description     string                      `json:"description,omitempty"`
	Annotations     entities.SessionAnnotations `json:"annotations"`
	SandboxPolicyID string                      `json:"sandbox_policy_id,omitempty"`
}

type createSessionRequest struct {
	Request        *entities.RunServerRequest `json:"request"`
	WebhookPayload []byte                     `json:"webhook_payload,omitempty"`
}

type provisionSettingsRequest struct {
	Request *entities.RunServerRequest `json:"request"`
}

type sessionsResponse struct {
	Sessions []SessionDTO `json:"sessions"`
}

type messageRequest struct {
	Message string `json:"message"`
}

type touchRequest struct {
	At time.Time `json:"at"`
}

type messagesResponse struct {
	Messages []portrepos.Message `json:"messages"`
}

type ensureWorkloadResponse struct {
	Session   *SessionDTO `json:"session,omitempty"`
	Restoring bool        `json:"restoring"`
}

type annotationsResponse struct {
	Annotations entities.SessionAnnotations `json:"annotations"`
}

type stockCountResponse struct {
	Count int `json:"count"`
}

type pendingAllocationDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

type submitExternalAllocationRequest struct {
	ManagerID         string                           `json:"manager_id"`
	ProvisionSettings *sessionsettings.SessionSettings `json:"provision_settings,omitempty"`
	Request           *entities.RunServerRequest       `json:"request"`
	Runtime           *coreallocation.RuntimeBootstrap `json:"runtime,omitempty"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type sessionAnnotationsProvider interface {
	Annotations() entities.SessionAnnotations
}

type sessionRequestProvider interface {
	Request() *entities.RunServerRequest
}

func newSessionDTO(session entities.Session) SessionDTO {
	tags := cloneTags(session.Tags())
	dto := SessionDTO{
		ID:            session.ID(),
		Addr:          session.Addr(),
		UserID:        session.UserID(),
		Scope:         session.Scope(),
		TeamID:        session.TeamID(),
		Tags:          tags,
		Status:        session.Status(),
		StartedAt:     session.StartedAt(),
		UpdatedAt:     session.UpdatedAt(),
		LastMessageAt: session.LastMessageAt(),
		Description:   session.Description(),
	}
	if provider, ok := session.(sessionAnnotationsProvider); ok {
		dto.Annotations = provider.Annotations()
	}
	if provider, ok := session.(sessionRequestProvider); ok {
		if req := provider.Request(); req != nil {
			if req.Sandbox != nil {
				dto.SandboxPolicyID = req.Sandbox.PolicyID
			}
			if req.SessionTTL != "" {
				if dto.Tags == nil {
					dto.Tags = make(map[string]string)
				}
				if _, exists := dto.Tags["session_ttl"]; !exists {
					dto.Tags["session_ttl"] = req.SessionTTL
				}
			}
		}
	}
	return dto
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(tags))
	for key, value := range tags {
		result[key] = value
	}
	return result
}

// remoteSession is the API-side entity returned by Client. Besides satisfying
// entities.Session, it exposes annotations and sandbox policy as optional
// capabilities used by the public API layer.
type remoteSession struct {
	dto SessionDTO
}

func (s *remoteSession) ID() string                               { return s.dto.ID }
func (s *remoteSession) Addr() string                             { return s.dto.Addr }
func (s *remoteSession) UserID() string                           { return s.dto.UserID }
func (s *remoteSession) Scope() entities.ResourceScope            { return s.dto.Scope }
func (s *remoteSession) TeamID() string                           { return s.dto.TeamID }
func (s *remoteSession) Tags() map[string]string                  { return cloneTags(s.dto.Tags) }
func (s *remoteSession) Status() string                           { return s.dto.Status }
func (s *remoteSession) StartedAt() time.Time                     { return s.dto.StartedAt }
func (s *remoteSession) UpdatedAt() time.Time                     { return s.dto.UpdatedAt }
func (s *remoteSession) LastMessageAt() time.Time                 { return s.dto.LastMessageAt }
func (s *remoteSession) Description() string                      { return s.dto.Description }
func (s *remoteSession) Cancel()                                  {}
func (s *remoteSession) Annotations() entities.SessionAnnotations { return s.dto.Annotations }
func (s *remoteSession) SandboxPolicyID() string                  { return s.dto.SandboxPolicyID }

func (d SessionDTO) entity() entities.Session {
	d.Tags = cloneTags(d.Tags)
	return &remoteSession{dto: d}
}
