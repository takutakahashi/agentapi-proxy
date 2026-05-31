package entities

import (
	"errors"
	"time"
)

// ErrSandboxPolicyNotFound is returned when a sandbox policy is not found.
type ErrSandboxPolicyNotFound struct {
	ID string
}

func (e ErrSandboxPolicyNotFound) Error() string {
	return "sandbox policy not found: " + e.ID
}

// SandboxPolicy is a named, reusable set of network filter rules.
// Sessions reference a policy by ID; the policy's domain lists are merged
// with any session-level overrides at creation time.
type SandboxPolicy struct {
	id             string
	name           string
	description    string
	allowedDomains []string
	deniedDomains  []string
	scope          ResourceScope
	ownerID        string
	teamID         string
	createdAt      time.Time
	updatedAt      time.Time
}

// NewSandboxPolicy creates a new SandboxPolicy.
func NewSandboxPolicy(id, name, description string, allowedDomains, deniedDomains []string, scope ResourceScope, ownerID, teamID string) *SandboxPolicy {
	now := time.Now()
	return &SandboxPolicy{
		id:             id,
		name:           name,
		description:    description,
		allowedDomains: copyStringSlice(allowedDomains),
		deniedDomains:  copyStringSlice(deniedDomains),
		scope:          scope,
		ownerID:        ownerID,
		teamID:         teamID,
		createdAt:      now,
		updatedAt:      now,
	}
}

func (p *SandboxPolicy) ID() string             { return p.id }
func (p *SandboxPolicy) Name() string           { return p.name }
func (p *SandboxPolicy) Description() string    { return p.description }
func (p *SandboxPolicy) AllowedDomains() []string { return copyStringSlice(p.allowedDomains) }
func (p *SandboxPolicy) DeniedDomains() []string  { return copyStringSlice(p.deniedDomains) }
func (p *SandboxPolicy) Scope() ResourceScope    { return p.scope }
func (p *SandboxPolicy) OwnerID() string         { return p.ownerID }
func (p *SandboxPolicy) TeamID() string          { return p.teamID }
func (p *SandboxPolicy) CreatedAt() time.Time    { return p.createdAt }
func (p *SandboxPolicy) UpdatedAt() time.Time    { return p.updatedAt }

func (p *SandboxPolicy) SetName(name string) {
	p.name = name
	p.updatedAt = time.Now()
}

func (p *SandboxPolicy) SetDescription(description string) {
	p.description = description
	p.updatedAt = time.Now()
}

func (p *SandboxPolicy) SetAllowedDomains(domains []string) {
	p.allowedDomains = copyStringSlice(domains)
	p.updatedAt = time.Now()
}

func (p *SandboxPolicy) SetDeniedDomains(domains []string) {
	p.deniedDomains = copyStringSlice(domains)
	p.updatedAt = time.Now()
}

func (p *SandboxPolicy) SetCreatedAt(t time.Time) { p.createdAt = t }
func (p *SandboxPolicy) SetUpdatedAt(t time.Time) { p.updatedAt = t }

func (p *SandboxPolicy) Validate() error {
	if p.id == "" {
		return errors.New("sandbox policy id is required")
	}
	if p.name == "" {
		return errors.New("sandbox policy name is required")
	}
	if p.ownerID == "" {
		return errors.New("sandbox policy owner_id is required")
	}
	if p.scope != ScopeUser && p.scope != ScopeTeam {
		return errors.New("sandbox policy scope must be 'user' or 'team'")
	}
	if p.scope == ScopeTeam && p.teamID == "" {
		return errors.New("sandbox policy team_id is required when scope is 'team'")
	}
	return nil
}

func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}
