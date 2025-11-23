package entities

import (
	"fmt"
	"time"
)

type AgentID string

type AgentStatus string

const (
	AgentStatusPending   AgentStatus = "pending"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusStopped   AgentStatus = "stopped"
)

type Agent struct {
	ID           AgentID
	SessionID    SessionID
	PodName      string
	Status       AgentStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastPingAt   time.Time
	Metadata     map[string]string
	ResourceInfo *AgentResourceInfo
}

type AgentResourceInfo struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	NodeName      string
	PodIP         string
}

func NewAgent(sessionID SessionID, podName string) *Agent {
	now := time.Now()
	return &Agent{
		ID:         AgentID(fmt.Sprintf("agent-%d", time.Now().UnixNano())),
		SessionID:  sessionID,
		PodName:    podName,
		Status:     AgentStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastPingAt: now,
		Metadata:   make(map[string]string),
	}
}

func (a *Agent) Start() {
	a.Status = AgentStatusRunning
	a.UpdatedAt = time.Now()
}

func (a *Agent) Stop() {
	a.Status = AgentStatusStopped
	a.UpdatedAt = time.Now()
}

func (a *Agent) Complete() {
	a.Status = AgentStatusCompleted
	a.UpdatedAt = time.Now()
}

func (a *Agent) Fail() {
	a.Status = AgentStatusFailed
	a.UpdatedAt = time.Now()
}

func (a *Agent) UpdatePing() {
	a.LastPingAt = time.Now()
	a.UpdatedAt = time.Now()
}

func (a *Agent) IsActive() bool {
	return a.Status == AgentStatusRunning || a.Status == AgentStatusPending
}

func (a *Agent) IsHealthy(timeout time.Duration) bool {
	return time.Since(a.LastPingAt) <= timeout
}
