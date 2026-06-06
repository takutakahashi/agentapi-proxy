package entities

import "time"

// ProxySession represents a session that lives on an external session manager (Proxy B).
// It implements the Session interface so it can be used anywhere a Session is expected.
type ProxySession struct {
	id        string
	userID    string
	scope     ResourceScope
	teamID    string
	tags      map[string]string
	status    string
	startedAt time.Time
}

// NewProxySession creates a new ProxySession
func NewProxySession(id, userID string, scope ResourceScope, teamID string, tags map[string]string, startedAt time.Time) *ProxySession {
	return NewProxySessionWithStatus(id, userID, scope, teamID, tags, startedAt, "running")
}

// NewProxySessionWithStatus creates a new ProxySession with an explicit status.
func NewProxySessionWithStatus(id, userID string, scope ResourceScope, teamID string, tags map[string]string, startedAt time.Time, status string) *ProxySession {
	if tags == nil {
		tags = make(map[string]string)
	}
	if status == "" {
		status = "running"
	}
	return &ProxySession{
		id:        id,
		userID:    userID,
		scope:     scope,
		teamID:    teamID,
		tags:      tags,
		status:    status,
		startedAt: startedAt,
	}
}

func (p *ProxySession) ID() string               { return p.id }
func (p *ProxySession) Addr() string             { return "" }
func (p *ProxySession) UserID() string           { return p.userID }
func (p *ProxySession) Scope() ResourceScope     { return p.scope }
func (p *ProxySession) TeamID() string           { return p.teamID }
func (p *ProxySession) Tags() map[string]string  { return p.tags }
func (p *ProxySession) Status() string           { return p.status }
func (p *ProxySession) StartedAt() time.Time     { return p.startedAt }
func (p *ProxySession) UpdatedAt() time.Time     { return p.startedAt }
func (p *ProxySession) LastMessageAt() time.Time { return p.startedAt }
func (p *ProxySession) Description() string      { return p.tags["description"] }
func (p *ProxySession) Cancel()                  {}
