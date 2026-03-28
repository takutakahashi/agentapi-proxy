package entities

import (
	"errors"
	"net/url"
	"time"
)

// ExternalSessionManager represents a registered external session manager (Proxy B)
type ExternalSessionManager struct {
	id         string
	name       string
	url        string
	userID     string
	scope      ResourceScope
	teamID     string
	hmacSecret string
	createdAt  time.Time
	updatedAt  time.Time
}

// NewExternalSessionManager creates a new ExternalSessionManager
func NewExternalSessionManager(id, name, u, userID string) *ExternalSessionManager {
	now := time.Now()
	return &ExternalSessionManager{
		id:        id,
		name:      name,
		url:       u,
		userID:    userID,
		scope:     ScopeUser,
		createdAt: now,
		updatedAt: now,
	}
}

// ID returns the manager ID
func (e *ExternalSessionManager) ID() string { return e.id }

// Name returns the manager name
func (e *ExternalSessionManager) Name() string { return e.name }

// URL returns the manager URL
func (e *ExternalSessionManager) URL() string { return e.url }

// UserID returns the owner user ID
func (e *ExternalSessionManager) UserID() string { return e.userID }

// Scope returns the resource scope
func (e *ExternalSessionManager) Scope() ResourceScope { return e.scope }

// TeamID returns the team ID (non-empty only when scope is team)
func (e *ExternalSessionManager) TeamID() string { return e.teamID }

// HMACSecret returns the full HMAC secret
func (e *ExternalSessionManager) HMACSecret() string { return e.hmacSecret }

// MaskedSecret returns a masked version of the HMAC secret (last 4 chars only)
func (e *ExternalSessionManager) MaskedSecret() string {
	if len(e.hmacSecret) <= 4 {
		return "****"
	}
	return "****" + e.hmacSecret[len(e.hmacSecret)-4:]
}

// CreatedAt returns the creation time
func (e *ExternalSessionManager) CreatedAt() time.Time { return e.createdAt }

// UpdatedAt returns the last update time
func (e *ExternalSessionManager) UpdatedAt() time.Time { return e.updatedAt }

// SetName updates the name
func (e *ExternalSessionManager) SetName(name string) {
	e.name = name
	e.updatedAt = time.Now()
}

// SetURL updates the URL
func (e *ExternalSessionManager) SetURL(u string) {
	e.url = u
	e.updatedAt = time.Now()
}

// SetScope sets the resource scope
func (e *ExternalSessionManager) SetScope(scope ResourceScope) {
	e.scope = scope
	e.updatedAt = time.Now()
}

// SetTeamID sets the team ID
func (e *ExternalSessionManager) SetTeamID(teamID string) {
	e.teamID = teamID
	e.updatedAt = time.Now()
}

// SetHMACSecret sets the HMAC secret
func (e *ExternalSessionManager) SetHMACSecret(secret string) {
	e.hmacSecret = secret
	e.updatedAt = time.Now()
}

// SetCreatedAt sets the created at time (for deserialization)
func (e *ExternalSessionManager) SetCreatedAt(t time.Time) { e.createdAt = t }

// SetUpdatedAt sets the updated at time (for deserialization)
func (e *ExternalSessionManager) SetUpdatedAt(t time.Time) { e.updatedAt = t }

// Validate validates the external session manager
func (e *ExternalSessionManager) Validate() error {
	if e.id == "" {
		return errors.New("id cannot be empty")
	}
	if e.name == "" {
		return errors.New("name cannot be empty")
	}
	if e.url == "" {
		return errors.New("url cannot be empty")
	}
	if e.userID == "" {
		return errors.New("user_id cannot be empty")
	}

	// Validate URL format
	parsed, err := url.ParseRequestURI(e.url)
	if err != nil {
		return errors.New("url must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https scheme")
	}

	if e.scope != ScopeUser && e.scope != ScopeTeam {
		return errors.New("scope must be 'user' or 'team'")
	}
	if e.scope == ScopeTeam && e.teamID == "" {
		return errors.New("team_id is required when scope is 'team'")
	}

	return nil
}
