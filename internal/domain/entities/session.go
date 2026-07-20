package entities

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// ResourceScope defines the scope of a resource (session, schedule, etc.)
type ResourceScope string

const (
	// ScopeUser indicates the resource is owned by a specific user
	ScopeUser ResourceScope = "user"
	// ScopeTeam indicates the resource is owned by a team
	ScopeTeam ResourceScope = "team"
)

// SlackParams represents Slack integration parameters
type SlackParams struct {
	// Channel is the Slack channel ID (e.g., "C1234567890")
	Channel string `json:"channel,omitempty"`
	// ThreadTS is the thread timestamp for threaded messages (e.g., "1234567890.123456")
	ThreadTS string `json:"thread_ts,omitempty"`
	// BotTokenSecretName is the K8s Secret name holding the custom bot token (xoxb-...).
	// When set, overrides the server-default SlackBotTokenSecretName for this session.
	BotTokenSecretName string `json:"bot_token_secret_name,omitempty"`
	// BotTokenSecretKey is the key within BotTokenSecretName holding the bot token.
	// Defaults to "bot-token" when empty.
	BotTokenSecretKey string `json:"bot_token_secret_key,omitempty"`
}

// SandboxParams holds network sandbox configuration requested at session creation.
type SandboxParams struct {
	// Enabled activates the network filter sidecar (iptables redirect + transparent proxy).
	Enabled bool `json:"enabled,omitempty"`
	// PolicyID references a SandboxPolicy resource whose domain lists are merged into the session.
	// Session-level AllowedDomains and DeniedDomains are appended on top of the policy.
	PolicyID string `json:"policy_id,omitempty"`
	// AllowedDomains is the list of hostnames, IPv4 addresses, and IPv4 CIDR ranges whose
	// traffic is permitted (allowlist mode). IP addresses and ranges bypass the proxy via
	// direct iptables rules. Takes precedence over DeniedDomains.
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	// DeniedDomains is the list of hostnames whose traffic should be blocked (denylist mode).
	// Used only when AllowedDomains is empty.
	DeniedDomains []string `json:"denied_domains,omitempty"`
	// CountMode enables count mode: policy rules are evaluated and blocked domains are recorded,
	// but traffic is not actually blocked. Useful for auditing policies before enforcing them.
	// Requires nfa v0.6.0+.
	CountMode bool `json:"count_mode,omitempty"`
}

// DockerParams holds Docker-in-Docker (DinD) configuration for session creation.
type DockerParams struct {
	// Enabled activates the DinD sidecar for this session
	Enabled bool `json:"enabled,omitempty"`
	// Registries specifies authenticated container registries
	Registries []DockerRegistry `json:"registries,omitempty"`
}

// DockerRegistry holds authentication for a container registry.
type DockerRegistry struct {
	// Server is the registry server address (e.g., "ghcr.io", "registry.example.com")
	Server string `json:"server"`
	// Username is the registry username for inline credentials
	Username string `json:"username,omitempty"`
	// Password is the registry password or access token for inline credentials
	Password string `json:"password,omitempty"`
	// SecretName is the name of a K8s Secret containing a "config.json" key
	// with docker config JSON format for registry authentication.
	SecretName string `json:"secret_name,omitempty"`
	// Insecure allows the DinD daemon to communicate with this registry over plain HTTP.
	Insecure bool `json:"insecure,omitempty"`
}

// SessionParams represents session parameters for agentapi server
type SessionParams struct {
	// Message is the initial message to send to the agent after session starts
	Message string `json:"message,omitempty"`
	// GithubToken is a GitHub token to use for authentication instead of GitHub App
	GithubToken string `json:"github_token,omitempty"`
	// AgentType specifies the type of agent to use for the session
	AgentType string `json:"agent_type,omitempty"`
	// Slack contains Slack integration parameters
	Slack *SlackParams `json:"slack,omitempty"`
	// Oneshot indicates whether the session should automatically delete itself after stopping
	Oneshot bool `json:"oneshot,omitempty"`
	// InitialMessageWaitSecond is the number of seconds to wait before sending the initial message.
	// Defaults to 2 seconds if not specified.
	InitialMessageWaitSecond *int `json:"initial_message_wait_second,omitempty"`
	// ManagerID is the ID of an external session manager (External Session Manager) to forward the session to.
	// When set, 親プロキシ will proxy this session creation to the specified External Session Manager instance.
	// The ID must match an ExternalSessionManagerEntry registered in the user's or team's settings.
	ManagerID string `json:"manager_id,omitempty"`
	// CycleMessage is the message to send to the session after each Claude stop event.
	// When set, a Stop hook is injected into the session's Claude settings that runs
	// `agentapi-proxy client cycle <message>`. The cycle continues until
	// /tmp/check/CYCLE_OK exists or CycleMaxCount is reached.
	CycleMessage string `json:"cycle_message,omitempty"`
	// CycleMaxCount is the maximum number of cycles to run. 0 means unlimited.
	// Requires CycleMessage to be set. The cycle count is tracked in /tmp/check/CYCLE_COUNT.
	CycleMaxCount int `json:"cycle_max_count,omitempty"`
	// RepoFullName is the full name of the GitHub repository to clone (e.g. "org/repo").
	// When set, AGENTAPI_REPO_FULLNAME and AGENTAPI_CLONE_DIR are injected into the session
	// environment so the repository is cloned at session startup.
	// For SlackBot sessions, this takes priority over any repository auto-detected from the message text.
	RepoFullName string `json:"repo_full_name,omitempty"`
	// Sandbox configures network isolation for the session via a sidecar transparent proxy.
	Sandbox *SandboxParams `json:"sandbox,omitempty"`
	// Docker configures Docker-in-Docker (DinD) for the session.
	Docker *DockerParams `json:"docker,omitempty"`
	// AuthProxy controls whether the session auth proxy sidecar is injected.
	// nil means use the global server configuration.
	AuthProxy *bool `json:"auth_proxy,omitempty"`
	// SessionTTL is the duration after the last message before this session is automatically deleted.
	// Accepted format: Go duration string (e.g. "48h", "7d" where d=24h, "168h").
	// Empty string means the global cleanup worker TTL is used for Slackbot sessions;
	// non-Slackbot sessions without this field are not auto-deleted.
	SessionTTL string `json:"session_ttl,omitempty"`
	// UnsyncedFilePaths excludes managed file paths from syncing changes back to storage.
	UnsyncedFilePaths []string `json:"unsynced_file_paths,omitempty"`
	// CredentialSource selects which managed credentials are injected into the session.
	// Valid values are "session_user", "team", and "none". Empty preserves the
	// legacy behavior (session user for user scope, none for team scope).
	CredentialSource string `json:"credential_source,omitempty"`
}

// SessionAnnotations contains user-managed annotations attached to a session.
type SessionAnnotations struct {
	PRURL       string `json:"pr_url,omitempty"`
	IssueURL    string `json:"issue_url,omitempty"`
	Description string `json:"description,omitempty"`
	RunningTask string `json:"running_task,omitempty"`
}

// UpdateSessionAnnotationsRequest partially updates user-managed session annotations.
// Nil fields are left unchanged; an explicit empty string clears that annotation.
type UpdateSessionAnnotationsRequest struct {
	PRURL       *string `json:"pr_url,omitempty"`
	IssueURL    *string `json:"issue_url,omitempty"`
	Description *string `json:"description,omitempty"`
	RunningTask *string `json:"running_task,omitempty"`
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
	// MemoryKey is a custom tag map to identify memories for this session.
	// If non-empty, memories matching these tags are fetched and injected into CLAUDE.md at startup.
	// If empty, memory integration is disabled.
	MemoryKey map[string]string `json:"memory_key,omitempty"`
	// SessionProfileID is an optional reference to a SessionProfile.
	// When set, the profile's config is used as a base; explicit fields override it.
	SessionProfileID string `json:"session_profile_id,omitempty"`
}

// RepositoryInfo contains repository information extracted from tags
type RepositoryInfo struct {
	FullName string
	CloneDir string
	Branch   string
	PR       string
}

// RunServerRequest contains parameters needed to run an agentapi server
type RunServerRequest struct {
	UserID string
	// ManagerID selects a specific External Session Manager. Empty means placement
	// is resolved from allocator tags, the tenant default, or the local manager.
	ManagerID                string
	Environment              map[string]string
	Tags                     map[string]string
	RepoInfo                 *RepositoryInfo
	InitialMessage           string
	Teams                    []string          // GitHub team slugs (e.g., ["org/team-a", "org/team-b"])
	GithubToken              string            // GitHub token passed via params.github_token
	Scope                    ResourceScope     // Resource scope ("user" or "team")
	TeamID                   string            // Team identifier when Scope is "team"
	AgentType                string            // Agent type for the session
	SlackParams              *SlackParams      // Slack integration parameters
	Oneshot                  bool              // Oneshot indicates whether the session should automatically delete itself after stopping
	InitialMessageWaitSecond *int              // Seconds to wait before sending initial message (default: 2)
	MemoryKey                map[string]string // Tag map to identify memories; nil means use Tags
	CycleMessage             string            // Message to send to session after each Claude stop event (injects Stop hook)
	CycleMaxCount            int               // Maximum number of cycles (0 = unlimited); requires CycleMessage
	// Sandbox configures network isolation for the session.
	Sandbox *SandboxParams
	// Docker configures Docker-in-Docker (DinD) for the session.
	Docker *DockerParams
	// AuthProxy controls whether the auth proxy sidecar is injected.
	// nil means use the global server configuration.
	AuthProxy *bool
	// ProvisionSettings, when non-nil, is used directly as the provision payload
	// instead of building it from the other request fields.
	// Used by the session manager forwarding path (small-cluster mode).
	ProvisionSettings *sessionsettings.SessionSettings
	// SessionTTL is the duration after the last message before this session is auto-deleted.
	// Stored as a Go duration string (e.g. "48h"). Empty means use the global cleanup TTL.
	SessionTTL string
	// UnsyncedFilePaths excludes managed file paths from syncing changes back to storage.
	UnsyncedFilePaths []string
	// CredentialSource selects the owner of managed credential files.
	CredentialSource string
}

// Session represents a running agentapi session
type Session interface {
	// ID returns the unique session identifier
	ID() string

	// Addr returns the address (host:port) the session is running on
	// Returns "{service-dns}:{port}" for Kubernetes sessions
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

	// UpdatedAt returns when the session was last updated
	UpdatedAt() time.Time

	// LastMessageAt returns when the last message was sent to the session.
	// Set at session creation and updated on every SendMessage call.
	LastMessageAt() time.Time

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
