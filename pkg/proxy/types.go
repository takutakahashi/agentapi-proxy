package proxy

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// RepositoryInfo contains repository information extracted from tags
type RepositoryInfo struct {
	FullName string
	CloneDir string
}

// RunServerRequest contains parameters needed to run an agentapi server
type RunServerRequest struct {
	Port           int
	UserID         string
	Environment    map[string]string
	Tags           map[string]string
	RepoInfo       *RepositoryInfo
	InitialMessage string
}

// AgentSession represents a running agentapi server instance
type AgentSession struct {
	ID           string
	Request      *RunServerRequest
	Process      *exec.Cmd
	Cancel       context.CancelFunc
	StartedAt    time.Time
	Status       string
	processMutex sync.RWMutex
}
