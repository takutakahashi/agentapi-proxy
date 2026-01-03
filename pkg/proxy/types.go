package proxy

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
