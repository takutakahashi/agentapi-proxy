package sessionallocation

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusAllocating Status = "allocating"
	StatusAssigned   Status = "assigned"
	StatusError      Status = "error"
)

// AllocationRequest is the cluster-visible request consumed by allocator workers.
type AllocationRequest struct {
	SessionID          string                           `json:"session_id"`
	ManagerID          string                           `json:"manager_id,omitempty"`
	Request            *entities.RunServerRequest       `json:"request"`
	ProvisionSettings  *sessionsettings.SessionSettings `json:"provision_settings,omitempty"`
	WebhookPayload     []byte                           `json:"webhook_payload,omitempty"`
	Status             Status                           `json:"status"`
	Message            string                           `json:"message,omitempty"`
	AllocatedSessionID string                           `json:"allocated_session_id,omitempty"`
	Requirements       Requirements                     `json:"requirements"`
	UpdatedAt          time.Time                        `json:"updated_at"`
}

type AllocationResult struct {
	Status             Status `json:"status"`
	Message            string `json:"message,omitempty"`
	AllocatedSessionID string `json:"allocated_session_id,omitempty"`
	ProxyURL           string `json:"proxy_url,omitempty"`
}

// Requirements captures pod capabilities used for stock matching.
type Requirements struct {
	AgentType string `json:"agent_type,omitempty"`
	Sandbox   bool   `json:"sandbox"`
	DinD      bool   `json:"dind"`
}

func RequirementsFromRunServerRequest(req *entities.RunServerRequest) Requirements {
	if req == nil {
		return Requirements{}
	}
	return Requirements{
		AgentType: req.AgentType,
		Sandbox:   req.Sandbox != nil && req.Sandbox.Enabled,
		DinD:      req.Docker != nil && req.Docker.Enabled,
	}
}
