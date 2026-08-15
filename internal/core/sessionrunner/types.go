package sessionrunner

import "time"

const (
	CapabilityRunnerClaimV1   = "runner_claim_v1"
	CapabilityDirectRuntimeV1 = "direct_session_runtime_v1"
)

type SubjectType string

const (
	SubjectUser SubjectType = "user"
	SubjectTeam SubjectType = "team"
)

type Manager struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	ConnectionTokenHash string            `json:"connection_token_hash,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	Capabilities        []string          `json:"capabilities,omitempty"`
	Enabled             bool              `json:"enabled"`
	Draining            bool              `json:"draining,omitempty"`
	LastHeartbeatAt     time.Time         `json:"last_heartbeat_at,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type Pool struct {
	Name         string            `json:"name"`
	ManagerID    string            `json:"manager_id"`
	Labels       map[string]string `json:"labels,omitempty"`
	MinIdle      int               `json:"min_idle,omitempty"`
	MaxRunners   int               `json:"max_runners,omitempty"`
	Enabled      bool              `json:"enabled"`
	Draining     bool              `json:"draining,omitempty"`
	IsDefault    bool              `json:"default,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	IdleRunners  int               `json:"idle_runners,omitempty"`
	TotalRunners int               `json:"total_runners,omitempty"`
}

type Binding struct {
	ID          string      `json:"id"`
	Pool        string      `json:"pool"`
	SubjectType SubjectType `json:"subject_type"`
	SubjectID   string      `json:"subject_id"`
	Enabled     bool        `json:"enabled"`
	Priority    int         `json:"priority,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Preference struct {
	SubjectType SubjectType `json:"subject_type"`
	SubjectID   string      `json:"subject_id"`
	Enabled     bool        `json:"enabled"`
	DefaultPool string      `json:"default_pool,omitempty"`
	Fallback    string      `json:"fallback,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type RunnerStatus string

const (
	RunnerIdle     RunnerStatus = "idle"
	RunnerClaiming RunnerStatus = "claiming"
	RunnerRunning  RunnerStatus = "running"
	RunnerOffline  RunnerStatus = "offline"
)

type Runner struct {
	ID        string       `json:"id"`
	ManagerID string       `json:"manager_id"`
	Pool      string       `json:"pool"`
	TokenHash string       `json:"token_hash,omitempty"`
	Status    RunnerStatus `json:"status"`
	PodName   string       `json:"pod_name,omitempty"`
	Namespace string       `json:"namespace,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	LastSeen  time.Time    `json:"last_seen,omitempty"`
}

type AllocationStatus string

const (
	AllocationPending   AllocationStatus = "pending"
	AllocationLeased    AllocationStatus = "leased"
	AllocationClaimed   AllocationStatus = "claimed"
	AllocationRunning   AllocationStatus = "running"
	AllocationCompleted AllocationStatus = "completed"
	AllocationFailed    AllocationStatus = "failed"
)

type Allocation struct {
	SessionID         string            `json:"session_id"`
	Pool              string            `json:"pool"`
	ManagerID         string            `json:"manager_id,omitempty"`
	RunnerID          string            `json:"runner_id,omitempty"`
	Status            AllocationStatus  `json:"status"`
	LeaseID           string            `json:"lease_id,omitempty"`
	LeaseExpiresAt    time.Time         `json:"lease_expires_at,omitempty"`
	Generation        int64             `json:"generation"`
	Attempts          int               `json:"attempts"`
	Requirements      map[string]string `json:"requirements,omitempty"`
	RuntimeToken      string            `json:"runtime_token,omitempty"`
	RuntimeTokenHash  string            `json:"runtime_token_hash,omitempty"`
	ProvisionSettings []byte            `json:"provision_settings,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Claim struct {
	Allocation *Allocation `json:"allocation"`
	Runner     *Runner     `json:"runner"`
}
