package entities

import (
	"time"
)

// SessionProfile represents a named, reusable session configuration for a tenant
type SessionProfile struct {
	id          string
	name        string
	description string
	userID      string
	scope       ResourceScope
	teamID      string
	isDefault   bool
	config      SessionProfileConfig
	createdAt   time.Time
	updatedAt   time.Time
}

// SessionProfileConfig contains the reusable session configuration fields
type SessionProfileConfig struct {
	environment            map[string]string
	tags                   map[string]string
	initialMessageTemplate string
	reuseMessageTemplate   string
	params                 *SessionParams
	reuseSession           bool
	memoryKey              map[string]string
}

// NewSessionProfile creates a new SessionProfile
func NewSessionProfile(id, name, userID string) *SessionProfile {
	now := time.Now()
	return &SessionProfile{
		id:        id,
		name:      name,
		userID:    userID,
		scope:     ScopeUser,
		createdAt: now,
		updatedAt: now,
	}
}

// ID returns the profile ID
func (p *SessionProfile) ID() string { return p.id }

// Name returns the profile name
func (p *SessionProfile) Name() string { return p.name }

// SetName sets the profile name
func (p *SessionProfile) SetName(name string) {
	p.name = name
	p.updatedAt = time.Now()
}

// Description returns the profile description
func (p *SessionProfile) Description() string { return p.description }

// SetDescription sets the profile description
func (p *SessionProfile) SetDescription(desc string) {
	p.description = desc
	p.updatedAt = time.Now()
}

// UserID returns the user ID
func (p *SessionProfile) UserID() string { return p.userID }

// Scope returns the resource scope
func (p *SessionProfile) Scope() ResourceScope {
	if p.scope == "" {
		return ScopeUser
	}
	return p.scope
}

// SetScope sets the resource scope
func (p *SessionProfile) SetScope(scope ResourceScope) {
	p.scope = scope
	p.updatedAt = time.Now()
}

// TeamID returns the team ID
func (p *SessionProfile) TeamID() string { return p.teamID }

// SetTeamID sets the team ID
func (p *SessionProfile) SetTeamID(teamID string) {
	p.teamID = teamID
	p.updatedAt = time.Now()
}

// IsDefault returns whether this profile is the tenant's default
func (p *SessionProfile) IsDefault() bool { return p.isDefault }

// SetIsDefault sets whether this profile is the tenant's default
func (p *SessionProfile) SetIsDefault(isDefault bool) {
	p.isDefault = isDefault
	p.updatedAt = time.Now()
}

// Config returns the session profile configuration
func (p *SessionProfile) Config() SessionProfileConfig { return p.config }

// SetConfig sets the session profile configuration
func (p *SessionProfile) SetConfig(cfg SessionProfileConfig) {
	p.config = cfg
	p.updatedAt = time.Now()
}

// CreatedAt returns the creation time
func (p *SessionProfile) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns the last update time
func (p *SessionProfile) UpdatedAt() time.Time { return p.updatedAt }

// SetCreatedAt sets the creation time
func (p *SessionProfile) SetCreatedAt(t time.Time) { p.createdAt = t }

// SetUpdatedAt sets the update time
func (p *SessionProfile) SetUpdatedAt(t time.Time) { p.updatedAt = t }

// Validate validates the session profile
func (p *SessionProfile) Validate() error {
	if p.id == "" {
		return ErrInvalidSessionProfile{Field: "id", Message: "id is required"}
	}
	if p.name == "" {
		return ErrInvalidSessionProfile{Field: "name", Message: "name is required"}
	}
	if p.userID == "" {
		return ErrInvalidSessionProfile{Field: "user_id", Message: "user_id is required"}
	}
	return nil
}

// --- SessionProfileConfig accessors ---

// NewSessionProfileConfig creates a new SessionProfileConfig
func NewSessionProfileConfig() SessionProfileConfig {
	return SessionProfileConfig{
		environment: make(map[string]string),
		tags:        make(map[string]string),
	}
}

// Environment returns the environment variables
func (c *SessionProfileConfig) Environment() map[string]string { return c.environment }

// SetEnvironment sets the environment variables
func (c *SessionProfileConfig) SetEnvironment(env map[string]string) { c.environment = env }

// Tags returns the tags
func (c *SessionProfileConfig) Tags() map[string]string { return c.tags }

// SetTags sets the tags
func (c *SessionProfileConfig) SetTags(tags map[string]string) { c.tags = tags }

// InitialMessageTemplate returns the initial message template
func (c *SessionProfileConfig) InitialMessageTemplate() string { return c.initialMessageTemplate }

// SetInitialMessageTemplate sets the initial message template
func (c *SessionProfileConfig) SetInitialMessageTemplate(t string) { c.initialMessageTemplate = t }

// ReuseMessageTemplate returns the reuse message template
func (c *SessionProfileConfig) ReuseMessageTemplate() string { return c.reuseMessageTemplate }

// SetReuseMessageTemplate sets the reuse message template
func (c *SessionProfileConfig) SetReuseMessageTemplate(t string) { c.reuseMessageTemplate = t }

// Params returns the session params
func (c *SessionProfileConfig) Params() *SessionParams { return c.params }

// SetParams sets the session params
func (c *SessionProfileConfig) SetParams(p *SessionParams) { c.params = p }

// ReuseSession returns whether to reuse an existing session
func (c *SessionProfileConfig) ReuseSession() bool { return c.reuseSession }

// SetReuseSession sets whether to reuse an existing session
func (c *SessionProfileConfig) SetReuseSession(reuse bool) { c.reuseSession = reuse }

// MemoryKey returns the memory key map
func (c *SessionProfileConfig) MemoryKey() map[string]string { return c.memoryKey }

// SetMemoryKey sets the memory key map
func (c *SessionProfileConfig) SetMemoryKey(key map[string]string) { c.memoryKey = key }

// --- Error types ---

// ErrInvalidSessionProfile represents a validation error for session profiles
type ErrInvalidSessionProfile struct {
	Field   string
	Message string
}

func (e ErrInvalidSessionProfile) Error() string {
	return "invalid session profile: " + e.Field + ": " + e.Message
}

// ErrSessionProfileNotFound is returned when a session profile is not found
type ErrSessionProfileNotFound struct {
	ID string
}

func (e ErrSessionProfileNotFound) Error() string {
	return "session profile not found: " + e.ID
}
