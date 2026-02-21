package entities

import (
	"testing"
	"time"
)

func TestNewMemory(t *testing.T) {
	m := NewMemory("id-1", "Test Title", "Test Content", ScopeUser, "user-1", "")

	if m.ID() != "id-1" {
		t.Errorf("expected id 'id-1', got '%s'", m.ID())
	}
	if m.Title() != "Test Title" {
		t.Errorf("expected title 'Test Title', got '%s'", m.Title())
	}
	if m.Content() != "Test Content" {
		t.Errorf("expected content 'Test Content', got '%s'", m.Content())
	}
	if m.Scope() != ScopeUser {
		t.Errorf("expected scope 'user', got '%s'", m.Scope())
	}
	if m.OwnerID() != "user-1" {
		t.Errorf("expected ownerID 'user-1', got '%s'", m.OwnerID())
	}
	if m.TeamID() != "" {
		t.Errorf("expected empty teamID, got '%s'", m.TeamID())
	}
	if m.Tags() == nil {
		t.Error("expected non-nil tags map")
	}
	if len(m.Tags()) != 0 {
		t.Errorf("expected empty tags, got %v", m.Tags())
	}
}

func TestNewMemoryWithTags(t *testing.T) {
	tags := map[string]string{"category": "meeting", "project": "alpha"}
	m := NewMemoryWithTags("id-2", "Team Note", "Content here", ScopeTeam, "user-1", "org/team", tags)

	if m.Scope() != ScopeTeam {
		t.Errorf("expected scope 'team', got '%s'", m.Scope())
	}
	if m.TeamID() != "org/team" {
		t.Errorf("expected teamID 'org/team', got '%s'", m.TeamID())
	}
	if len(m.Tags()) != 2 {
		t.Errorf("expected 2 tags, got %d", len(m.Tags()))
	}
	if m.Tags()["category"] != "meeting" {
		t.Errorf("expected tag category=meeting, got %s", m.Tags()["category"])
	}
}

func TestMemory_Validate(t *testing.T) {
	tests := []struct {
		name      string
		memory    *Memory
		expectErr bool
	}{
		{
			name:      "valid user-scope memory",
			memory:    NewMemory("id", "title", "content", ScopeUser, "owner", ""),
			expectErr: false,
		},
		{
			name:      "valid team-scope memory",
			memory:    NewMemory("id", "title", "content", ScopeTeam, "owner", "org/team"),
			expectErr: false,
		},
		{
			name:      "missing id",
			memory:    &Memory{title: "t", scope: ScopeUser, ownerID: "o"},
			expectErr: true,
		},
		{
			name:      "missing title",
			memory:    &Memory{id: "id", scope: ScopeUser, ownerID: "o"},
			expectErr: true,
		},
		{
			name:      "missing ownerID",
			memory:    &Memory{id: "id", title: "t", scope: ScopeUser},
			expectErr: true,
		},
		{
			name:      "invalid scope",
			memory:    &Memory{id: "id", title: "t", ownerID: "o", scope: ResourceScope("invalid")},
			expectErr: true,
		},
		{
			name:      "team scope without teamID",
			memory:    &Memory{id: "id", title: "t", ownerID: "o", scope: ScopeTeam},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.memory.Validate()
			if tt.expectErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestMemory_SetTitle_UpdatesUpdatedAt(t *testing.T) {
	m := NewMemory("id", "original", "content", ScopeUser, "user", "")
	originalUpdated := m.UpdatedAt()

	time.Sleep(time.Millisecond)
	m.SetTitle("updated")

	if m.Title() != "updated" {
		t.Error("expected title to be updated")
	}
	if !m.UpdatedAt().After(originalUpdated) {
		t.Error("expected updatedAt to be updated after SetTitle")
	}
}

func TestMemory_SetContent_UpdatesUpdatedAt(t *testing.T) {
	m := NewMemory("id", "title", "original", ScopeUser, "user", "")
	originalUpdated := m.UpdatedAt()

	time.Sleep(time.Millisecond)
	m.SetContent("updated content")

	if m.Content() != "updated content" {
		t.Error("expected content to be updated")
	}
	if !m.UpdatedAt().After(originalUpdated) {
		t.Error("expected updatedAt to be updated after SetContent")
	}
}

func TestMemory_SetTags(t *testing.T) {
	m := NewMemory("id", "title", "content", ScopeUser, "user", "")
	m.SetTags(map[string]string{"key": "value"})

	if m.Tags()["key"] != "value" {
		t.Error("expected tag to be set")
	}

	// Setting nil clears all tags
	m.SetTags(nil)
	if len(m.Tags()) != 0 {
		t.Errorf("expected tags to be cleared, got %v", m.Tags())
	}
}

func TestMemory_SetTags_UpdatesUpdatedAt(t *testing.T) {
	m := NewMemory("id", "title", "content", ScopeUser, "user", "")
	originalUpdated := m.UpdatedAt()

	time.Sleep(time.Millisecond)
	m.SetTags(map[string]string{"k": "v"})

	if !m.UpdatedAt().After(originalUpdated) {
		t.Error("expected updatedAt to be updated after SetTags")
	}
}

func TestMemory_Tags_ReturnsCopy(t *testing.T) {
	m := NewMemory("id", "title", "content", ScopeUser, "user", "")
	m.SetTags(map[string]string{"k": "v"})

	// Modifying the returned copy should not affect the internal state
	tags := m.Tags()
	tags["extra"] = "should-not-appear"

	if _, ok := m.Tags()["extra"]; ok {
		t.Error("modifying returned tags map should not affect internal state")
	}
}

func TestMemory_MatchesTags(t *testing.T) {
	m := NewMemoryWithTags("id", "title", "content", ScopeUser, "user", "", map[string]string{
		"category": "meeting",
		"project":  "alpha",
	})

	tests := []struct {
		name     string
		filter   map[string]string
		expected bool
	}{
		{"empty filter always matches", map[string]string{}, true},
		{"nil filter always matches", nil, true},
		{"all matching tags", map[string]string{"category": "meeting", "project": "alpha"}, true},
		{"partial match - one tag", map[string]string{"category": "meeting"}, true},
		{"one tag missing", map[string]string{"category": "other"}, false},
		{"extra tag not present", map[string]string{"category": "meeting", "extra": "value"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.MatchesTags(tt.filter)
			if got != tt.expected {
				t.Errorf("MatchesTags(%v) = %v, want %v", tt.filter, got, tt.expected)
			}
		})
	}
}

func TestMemory_MatchesText(t *testing.T) {
	m := NewMemory("id", "Q1 Roadmap Discussion", "Discussed milestones for Q1 2025", ScopeUser, "user", "")

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{"empty query always matches", "", true},
		{"exact title match", "Q1 Roadmap Discussion", true},
		{"partial title match", "Roadmap", true},
		{"case insensitive title", "roadmap", true},
		{"content match", "milestones", true},
		{"case insensitive content", "MILESTONES", true},
		{"no match", "completely different", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.MatchesText(tt.query)
			if got != tt.expected {
				t.Errorf("MatchesText(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestErrMemoryNotFound(t *testing.T) {
	err := ErrMemoryNotFound{ID: "test-id"}
	if err.Error() != "memory not found: test-id" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}
