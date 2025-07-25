package entities

import (
	"errors"
	"fmt"
	"time"
)

// SessionID represents a unique session identifier
type SessionID string

// Port represents a network port
type Port int

// SessionStatus represents the status of a session
type SessionStatus string

const (
	SessionStatusActive      SessionStatus = "active"
	SessionStatusStarting    SessionStatus = "starting"
	SessionStatusTerminating SessionStatus = "terminating"
	SessionStatusFailed      SessionStatus = "failed"
	SessionStatusStopped     SessionStatus = "stopped"
)

// Environment represents session environment variables
type Environment map[string]string

// Tags represents session tags
type Tags map[string]string

// ProcessInfo contains information about the running process
type ProcessInfo struct {
	pid       int
	startedAt time.Time
	command   string
	args      []string
}

// NewProcessInfo creates a new ProcessInfo
func NewProcessInfo(pid int, startedAt time.Time) *ProcessInfo {
	return &ProcessInfo{
		pid:       pid,
		startedAt: startedAt,
	}
}

// PID returns the process ID
func (p *ProcessInfo) PID() int {
	return p.pid
}

// StartedAt returns when the process started
func (p *ProcessInfo) StartedAt() time.Time {
	return p.startedAt
}

// Command returns the command
func (p *ProcessInfo) Command() string {
	return p.command
}

// Args returns the command arguments
func (p *ProcessInfo) Args() []string {
	args := make([]string, len(p.args))
	copy(args, p.args)
	return args
}

// SetCommand sets the command and arguments
func (p *ProcessInfo) SetCommand(command string, args []string) {
	p.command = command
	p.args = make([]string, len(args))
	copy(p.args, args)
}

// Session represents a session domain entity
type Session struct {
	id          SessionID
	userID      UserID
	port        Port
	status      SessionStatus
	startedAt   time.Time
	environment Environment
	tags        Tags
	repository  *Repository
	processInfo *ProcessInfo
}

// NewSession creates a new session
func NewSession(id SessionID, userID UserID, port Port, environment Environment, tags Tags, repository *Repository) *Session {
	return &Session{
		id:          id,
		userID:      userID,
		port:        port,
		status:      SessionStatusStarting,
		startedAt:   time.Now(),
		environment: environment,
		tags:        tags,
		repository:  repository,
	}
}

// ID returns the session ID
func (s *Session) ID() SessionID {
	return s.id
}

// UserID returns the user ID
func (s *Session) UserID() UserID {
	return s.userID
}

// Port returns the session port
func (s *Session) Port() Port {
	return s.port
}

// Status returns the current session status
func (s *Session) Status() SessionStatus {
	return s.status
}

// StartedAt returns when the session was started
func (s *Session) StartedAt() time.Time {
	return s.startedAt
}

// Environment returns the session environment
func (s *Session) Environment() Environment {
	if s.environment == nil {
		return make(Environment)
	}
	// Return a copy to prevent external modification
	env := make(Environment)
	for k, v := range s.environment {
		env[k] = v
	}
	return env
}

// Tags returns the session tags
func (s *Session) Tags() Tags {
	if s.tags == nil {
		return make(Tags)
	}
	// Return a copy to prevent external modification
	tags := make(Tags)
	for k, v := range s.tags {
		tags[k] = v
	}
	return tags
}

// Repository returns the repository information
func (s *Session) Repository() *Repository {
	return s.repository
}

// ProcessInfo returns the process information
func (s *Session) ProcessInfo() *ProcessInfo {
	return s.processInfo
}

// Start marks the session as active and sets process info
func (s *Session) Start(processInfo *ProcessInfo) error {
	if s.status != SessionStatusStarting {
		return errors.New("session can only be started from starting status")
	}

	if processInfo == nil {
		return errors.New("process info is required to start session")
	}

	s.status = SessionStatusActive
	s.processInfo = processInfo
	return nil
}

// Terminate marks the session as terminating
func (s *Session) Terminate() error {
	if s.status != SessionStatusActive {
		return fmt.Errorf("session cannot be terminated from status: %s", s.status)
	}

	s.status = SessionStatusTerminating
	return nil
}

// MarkFailed marks the session as failed
func (s *Session) MarkFailed(reason string) {
	s.status = SessionStatusFailed
	// In a real implementation, we might want to store the failure reason
}

// MarkStopped marks the session as stopped
func (s *Session) MarkStopped() error {
	if s.status != SessionStatusTerminating {
		return fmt.Errorf("session cannot be stopped from status: %s", s.status)
	}

	s.status = SessionStatusStopped
	s.processInfo = nil
	return nil
}

// IsActive returns true if the session is active
func (s *Session) IsActive() bool {
	return s.status == SessionStatusActive
}

// IsStopped returns true if the session is stopped
func (s *Session) IsStopped() bool {
	return s.status == SessionStatusStopped
}

// IsFailed returns true if the session has failed
func (s *Session) IsFailed() bool {
	return s.status == SessionStatusFailed
}

// IsHealthy returns true if the session is healthy (active with valid process)
func (s *Session) IsHealthy() bool {
	return s.status == SessionStatusActive && s.processInfo != nil && s.processInfo.PID() > 0
}

// CanBeTerminated returns true if the session can be terminated
func (s *Session) CanBeTerminated() bool {
	return s.status == SessionStatusActive || s.status == SessionStatusFailed
}

// UpdateEnvironment updates the session environment
func (s *Session) UpdateEnvironment(newEnv Environment) error {
	if s.status == SessionStatusActive {
		return errors.New("cannot update environment of active session")
	}

	if s.environment == nil {
		s.environment = make(Environment)
	}

	for k, v := range newEnv {
		s.environment[k] = v
	}

	return nil
}

// UpdateTags updates the session tags
func (s *Session) UpdateTags(newTags Tags) {
	if s.tags == nil {
		s.tags = make(Tags)
	}

	for k, v := range newTags {
		s.tags[k] = v
	}
}

// Duration returns how long the session has been running
func (s *Session) Duration() time.Duration {
	return time.Since(s.startedAt)
}

// Validate ensures the session is in a valid state
func (s *Session) Validate() error {
	if s.id == "" {
		return errors.New("session ID cannot be empty")
	}

	if s.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if s.port <= 0 || s.port > 65535 {
		return fmt.Errorf("invalid port: %d", s.port)
	}

	if s.startedAt.IsZero() {
		return errors.New("started at time cannot be zero")
	}

	return nil
}
