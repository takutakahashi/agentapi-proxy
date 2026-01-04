package entities

import (
	"time"
)

// ResourceScope defines the scope of a resource (session, schedule, etc.)
type ResourceScope string

const (
	// ScopeUser indicates the resource is owned by a specific user
	ScopeUser ResourceScope = "user"
	// ScopeTeam indicates the resource is owned by a team
	ScopeTeam ResourceScope = "team"
)

// SessionParams represents session parameters for agentapi server
type SessionParams struct {
	// Message is the initial message to send to the agent after session starts
	Message string `json:"message,omitempty"`
	// GithubToken is a GitHub token to use for authentication instead of GitHub App
	GithubToken string `json:"github_token,omitempty"`
}

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	// Params contains session parameters
	Params *SessionParams `json:"params,omitempty"`
	// Scope defines the ownership scope ("user" or "team"). Defaults to "user".
	Scope ResourceScope `json:"scope,omitempty"`
	// TeamID is the team identifier (e.g., "org/team-slug") when Scope is "team"
	TeamID string `json:"team_id,omitempty"`
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
	Teams          []string      // GitHub team slugs (e.g., ["org/team-a", "org/team-b"])
	GithubToken    string        // GitHub token passed via params.github_token
	Scope          ResourceScope // Resource scope ("user" or "team")
	TeamID         string        // Team identifier when Scope is "team"
}

// Session represents a running agentapi session
type Session interface {
	// ID returns the unique session identifier
	ID() string

	// Addr returns the address (host:port) the session is running on
	// For local sessions, this returns "localhost:{port}"
	// For Kubernetes sessions, this returns "{service-dns}:{port}"
	Addr() string

	// UserID returns the user ID that owns this session
	UserID() string

	// Scope returns the resource scope ("user" or "team")
	Scope() ResourceScope

	// TeamID returns the team ID when Scope is "team"
	TeamID() string

	// Tags returns the session tags
	Tags() map[string]string

	// Status returns the current status of the session
	Status() string

	// StartedAt returns when the session was started
	StartedAt() time.Time

	// Description returns the session description
	// Returns tags["description"] if exists, otherwise returns InitialMessage
	Description() string

	// Cancel cancels the session context to trigger shutdown
	Cancel()
}

// SessionFilter defines filter criteria for listing sessions
type SessionFilter struct {
	UserID  string
	Status  string
	Tags    map[string]string
	Scope   ResourceScope // Filter by scope ("user" or "team")
	TeamID  string        // Filter by specific team ID
	TeamIDs []string      // Filter by multiple team IDs (user's teams)
}
