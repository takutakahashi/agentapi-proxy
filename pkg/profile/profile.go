package profile

import (
	"time"
)

// Profile represents a user profile with environment variables, repository history, system prompt, and message templates
type Profile struct {
	ID                string            `json:"id"`
	UserID            string            `json:"user_id"`
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	Environment       map[string]string `json:"environment"`
	RepositoryHistory []RepositoryEntry `json:"repository_history"`
	SystemPrompt      string            `json:"system_prompt"`
	MessageTemplates  []MessageTemplate `json:"message_templates"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	LastUsedAt        *time.Time        `json:"last_used_at,omitempty"`
}

// RepositoryEntry represents a repository in the user's history
type RepositoryEntry struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Name       string            `json:"name"`
	Branch     string            `json:"branch,omitempty"`
	LastCommit string            `json:"last_commit,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	AccessedAt time.Time         `json:"accessed_at"`
}

// MessageTemplate represents a predefined message template
type MessageTemplate struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Content   string            `json:"content"`
	Variables []string          `json:"variables,omitempty"`
	Category  string            `json:"category,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// CreateProfileRequest represents the request body for creating a new profile
type CreateProfileRequest struct {
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	Environment       map[string]string `json:"environment"`
	RepositoryHistory []RepositoryEntry `json:"repository_history,omitempty"`
	SystemPrompt      string            `json:"system_prompt"`
	MessageTemplates  []MessageTemplate `json:"message_templates,omitempty"`
}

// UpdateProfileRequest represents the request body for updating a profile
type UpdateProfileRequest struct {
	Name              *string           `json:"name,omitempty"`
	Description       *string           `json:"description,omitempty"`
	Environment       map[string]string `json:"environment,omitempty"`
	RepositoryHistory []RepositoryEntry `json:"repository_history,omitempty"`
	SystemPrompt      *string           `json:"system_prompt,omitempty"`
	MessageTemplates  []MessageTemplate `json:"message_templates,omitempty"`
}

// ProfileResponse represents the response for profile operations
type ProfileResponse struct {
	Profile *Profile `json:"profile"`
}

// ProfileListResponse represents the response for listing profiles
type ProfileListResponse struct {
	Profiles []Profile `json:"profiles"`
	Total    int       `json:"total"`
}

// StartSessionWithProfileRequest extends StartRequest with profile support
type StartSessionWithProfileRequest struct {
	ProfileID   string            `json:"profile_id,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Message     string            `json:"message,omitempty"`
}
