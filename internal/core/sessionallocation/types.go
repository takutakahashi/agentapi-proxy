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
// Note: Sandbox (network filter) and scia sidecar are now always enabled.
// Only DinD remains configurable.
type Requirements struct {
	AgentType string `json:"agent_type,omitempty"`
	// Sandbox is always true (network filter cannot be opted out).
	// The field is kept for backward compatibility with existing JSON.
	Sandbox bool `json:"sandbox"`
	DinD    bool   `json:"dind"`
}

func RequirementsFromRunServerRequest(req *entities.RunServerRequest) Requirements {
	if req == nil {
		return Requirements{Sandbox: true}
	}
	return Requirements{
		AgentType: req.AgentType,
		Sandbox:   true, // Always enabled
		DinD:      req.Docker != nil && req.Docker.Enabled,
	}
}
