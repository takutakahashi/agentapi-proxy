package entities

import (
	"errors"
	"strings"
	"time"
)

// ErrMemoryNotFound is returned when a memory entry is not found
type ErrMemoryNotFound struct {
	ID string
}

func (e ErrMemoryNotFound) Error() string {
	return "memory not found: " + e.ID
}

// Memory represents a single memory entry belonging to a user or team
type Memory struct {
	id        string
	title     string
	content   string
	tags      map[string]string
	scope     ResourceScope
	ownerID   string // user ID of the creator/owner
	teamID    string // populated when scope == ScopeTeam
	createdAt time.Time
	updatedAt time.Time
}

// NewMemory creates a new Memory entry with the given fields.
// id should be a UUID string.
func NewMemory(id, title, content string, scope ResourceScope, ownerID, teamID string) *Memory {
	now := time.Now()
	return &Memory{
		id:        id,
		title:     title,
		content:   content,
		tags:      make(map[string]string),
		scope:     scope,
		ownerID:   ownerID,
		teamID:    teamID,
		createdAt: now,
		updatedAt: now,
	}
}

// NewMemoryWithTags creates a Memory entry with tags set immediately.
func NewMemoryWithTags(id, title, content string, scope ResourceScope, ownerID, teamID string, tags map[string]string) *Memory {
	m := NewMemory(id, title, content, scope, ownerID, teamID)
	if tags != nil {
		m.tags = make(map[string]string, len(tags))
		for k, v := range tags {
			m.tags[k] = v
		}
	}
	return m
}

// ID returns the memory entry ID (UUID)
func (m *Memory) ID() string {
	return m.id
}

// Title returns the memory entry title
func (m *Memory) Title() string {
	return m.title
}

// Content returns the memory entry content
func (m *Memory) Content() string {
	return m.content
}

// Tags returns a copy of the memory entry tags
func (m *Memory) Tags() map[string]string {
	if m.tags == nil {
		return make(map[string]string)
	}
	copy := make(map[string]string, len(m.tags))
	for k, v := range m.tags {
		copy[k] = v
	}
	return copy
}

// Scope returns the resource scope (user or team)
func (m *Memory) Scope() ResourceScope {
	return m.scope
}

// OwnerID returns the user ID of the owner who created this entry
func (m *Memory) OwnerID() string {
	return m.ownerID
}

// TeamID returns the team ID (populated only when scope == ScopeTeam)
func (m *Memory) TeamID() string {
	return m.teamID
}

// CreatedAt returns the creation timestamp
func (m *Memory) CreatedAt() time.Time {
	return m.createdAt
}

// UpdatedAt returns the last update timestamp
func (m *Memory) UpdatedAt() time.Time {
	return m.updatedAt
}

// SetTitle sets the title and updates the updatedAt timestamp
func (m *Memory) SetTitle(title string) {
	m.title = title
	m.updatedAt = time.Now()
}

// SetContent sets the content and updates the updatedAt timestamp
func (m *Memory) SetContent(content string) {
	m.content = content
	m.updatedAt = time.Now()
}

// SetTags replaces all tags and updates the updatedAt timestamp.
// Passing nil clears all tags.
func (m *Memory) SetTags(tags map[string]string) {
	if tags == nil {
		m.tags = make(map[string]string)
	} else {
		m.tags = make(map[string]string, len(tags))
		for k, v := range tags {
			m.tags[k] = v
		}
	}
	m.updatedAt = time.Now()
}

// SetCreatedAt sets the createdAt field (for deserialization only)
func (m *Memory) SetCreatedAt(t time.Time) {
	m.createdAt = t
}

// SetUpdatedAt sets the updatedAt field (for deserialization only)
func (m *Memory) SetUpdatedAt(t time.Time) {
	m.updatedAt = t
}

// Validate returns an error if the memory entry is in an invalid state
func (m *Memory) Validate() error {
	if m.id == "" {
		return errors.New("memory id is required")
	}
	if m.title == "" {
		return errors.New("memory title is required")
	}
	if m.ownerID == "" {
		return errors.New("memory owner_id is required")
	}
	if m.scope != ScopeUser && m.scope != ScopeTeam {
		return errors.New("memory scope must be 'user' or 'team'")
	}
	if m.scope == ScopeTeam && m.teamID == "" {
		return errors.New("memory team_id is required when scope is 'team'")
	}
	return nil
}

// MatchesTags returns true if the memory entry contains ALL of the given tag key-value pairs.
// An empty filter always returns true.
func (m *Memory) MatchesTags(filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	for k, v := range filter {
		if m.tags[k] != v {
			return false
		}
	}
	return true
}

// MatchesText returns true if the title or content contains the search string (case-insensitive).
// An empty query always returns true.
func (m *Memory) MatchesText(query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(m.title), q) ||
		strings.Contains(strings.ToLower(m.content), q)
}
