package profile

import (
	"encoding/json"
	"fmt"
	"time"
)

// PromptTemplate represents a prompt template with name and content
type PromptTemplate struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Content     string            `json:"content"`
	Variables   map[string]string `json:"variables,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ProfileConfig represents a single profile configuration
type ProfileConfig struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description,omitempty"`
	APIEndpoint     string                 `json:"api_endpoint,omitempty"`
	ClaudeJSON      map[string]interface{} `json:"claude_json,omitempty"`
	PromptTemplates []PromptTemplate       `json:"prompt_templates,omitempty"`
	Preferences     map[string]interface{} `json:"preferences,omitempty"`
	Settings        map[string]interface{} `json:"settings,omitempty"`
	Metadata        map[string]string      `json:"metadata,omitempty"`
	IsDefault       bool                   `json:"is_default"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// UserProfiles represents a user's collection of profiles
type UserProfiles struct {
	UserID      string          `json:"user_id"`
	Username    string          `json:"username"`
	Email       string          `json:"email"`
	DisplayName string          `json:"display_name"`
	Profiles    []ProfileConfig `json:"profiles"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	LastLoginAt *time.Time      `json:"last_login_at,omitempty"`
}

// NewUserProfiles creates a new user profiles collection
func NewUserProfiles(userID string) *UserProfiles {
	now := time.Now()
	return &UserProfiles{
		UserID:    userID,
		Profiles:  make([]ProfileConfig, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewProfileConfig creates a new profile configuration
func NewProfileConfig(name string) *ProfileConfig {
	now := time.Now()
	return &ProfileConfig{
		Name:            name,
		Preferences:     make(map[string]interface{}),
		Settings:        make(map[string]interface{}),
		Metadata:        make(map[string]string),
		PromptTemplates: make([]PromptTemplate, 0),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// NewPromptTemplate creates a new prompt template
func NewPromptTemplate(name, content string) *PromptTemplate {
	now := time.Now()
	return &PromptTemplate{
		Name:      name,
		Content:   content,
		Variables: make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddProfile adds a new profile configuration
func (up *UserProfiles) AddProfile(profile *ProfileConfig) error {
	// Check if profile name already exists
	for _, p := range up.Profiles {
		if p.Name == profile.Name {
			return fmt.Errorf("profile with name '%s' already exists", profile.Name)
		}
	}

	// If this is the first profile, make it default
	if len(up.Profiles) == 0 {
		profile.IsDefault = true
	}

	up.Profiles = append(up.Profiles, *profile)
	up.UpdatedAt = time.Now()
	return nil
}

// GetProfile gets a profile by name
func (up *UserProfiles) GetProfile(name string) (*ProfileConfig, error) {
	for i, p := range up.Profiles {
		if p.Name == name {
			return &up.Profiles[i], nil
		}
	}
	return nil, fmt.Errorf("profile '%s' not found", name)
}

// UpdateProfile updates a profile by name
func (up *UserProfiles) UpdateProfile(name string, update *ProfileConfigUpdate) error {
	for i, p := range up.Profiles {
		if p.Name == name {
			up.Profiles[i].updateFrom(update)
			up.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("profile '%s' not found", name)
}

// DeleteProfile deletes a profile by name
func (up *UserProfiles) DeleteProfile(name string) error {
	for i, p := range up.Profiles {
		if p.Name == name {
			// If deleting default profile, make another one default
			if p.IsDefault && len(up.Profiles) > 1 {
				for j := range up.Profiles {
					if j != i {
						up.Profiles[j].IsDefault = true
						break
					}
				}
			}

			up.Profiles = append(up.Profiles[:i], up.Profiles[i+1:]...)
			up.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("profile '%s' not found", name)
}

// SetDefaultProfile sets a profile as default by name
func (up *UserProfiles) SetDefaultProfile(name string) error {
	found := false
	for i := range up.Profiles {
		if up.Profiles[i].Name == name {
			up.Profiles[i].IsDefault = true
			found = true
		} else {
			up.Profiles[i].IsDefault = false
		}
	}

	if !found {
		return fmt.Errorf("profile '%s' not found", name)
	}

	up.UpdatedAt = time.Now()
	return nil
}

// GetDefaultProfile returns the default profile
func (up *UserProfiles) GetDefaultProfile() *ProfileConfig {
	for i, p := range up.Profiles {
		if p.IsDefault {
			return &up.Profiles[i]
		}
	}
	// If no default found, return first profile if exists
	if len(up.Profiles) > 0 {
		return &up.Profiles[0]
	}
	return nil
}

// ListProfileNames returns all profile names
func (up *UserProfiles) ListProfileNames() []string {
	names := make([]string, len(up.Profiles))
	for i, p := range up.Profiles {
		names[i] = p.Name
	}
	return names
}

// updateFrom updates profile config fields from update struct
func (pc *ProfileConfig) updateFrom(update *ProfileConfigUpdate) {
	if update.Description != "" {
		pc.Description = update.Description
	}
	if update.APIEndpoint != "" {
		pc.APIEndpoint = update.APIEndpoint
	}
	if update.ClaudeJSON != nil {
		pc.ClaudeJSON = update.ClaudeJSON
	}
	if update.PromptTemplates != nil {
		pc.PromptTemplates = update.PromptTemplates
	}
	if update.Preferences != nil {
		if pc.Preferences == nil {
			pc.Preferences = make(map[string]interface{})
		}
		for k, v := range update.Preferences {
			pc.Preferences[k] = v
		}
	}
	if update.Settings != nil {
		if pc.Settings == nil {
			pc.Settings = make(map[string]interface{})
		}
		for k, v := range update.Settings {
			pc.Settings[k] = v
		}
	}
	if update.Metadata != nil {
		if pc.Metadata == nil {
			pc.Metadata = make(map[string]string)
		}
		for k, v := range update.Metadata {
			pc.Metadata[k] = v
		}
	}
	pc.UpdatedAt = time.Now()
}

// Update updates the user profiles fields with non-empty values from the update
func (up *UserProfiles) Update(update *UserProfilesUpdate) {
	if update.Username != "" {
		up.Username = update.Username
	}
	if update.Email != "" {
		up.Email = update.Email
	}
	if update.DisplayName != "" {
		up.DisplayName = update.DisplayName
	}
	up.UpdatedAt = time.Now()
}

// UserProfilesUpdate represents fields that can be updated in user profiles
type UserProfilesUpdate struct {
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// ProfileConfigUpdate represents fields that can be updated in a profile config
type ProfileConfigUpdate struct {
	Description     string                 `json:"description,omitempty"`
	APIEndpoint     string                 `json:"api_endpoint,omitempty"`
	ClaudeJSON      map[string]interface{} `json:"claude_json,omitempty"`
	PromptTemplates []PromptTemplate       `json:"prompt_templates,omitempty"`
	Preferences     map[string]interface{} `json:"preferences,omitempty"`
	Settings        map[string]interface{} `json:"settings,omitempty"`
	Metadata        map[string]string      `json:"metadata,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for UserProfiles
func (up *UserProfiles) MarshalJSON() ([]byte, error) {
	type Alias UserProfiles
	return json.Marshal(&struct {
		*Alias
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   string  `json:"updated_at"`
		LastLoginAt *string `json:"last_login_at,omitempty"`
	}{
		Alias:     (*Alias)(up),
		CreatedAt: up.CreatedAt.Format(time.RFC3339),
		UpdatedAt: up.UpdatedAt.Format(time.RFC3339),
		LastLoginAt: func() *string {
			if up.LastLoginAt != nil {
				s := up.LastLoginAt.Format(time.RFC3339)
				return &s
			}
			return nil
		}(),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling for UserProfiles
func (up *UserProfiles) UnmarshalJSON(data []byte) error {
	type Alias UserProfiles
	aux := &struct {
		*Alias
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   string  `json:"updated_at"`
		LastLoginAt *string `json:"last_login_at,omitempty"`
	}{
		Alias: (*Alias)(up),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	up.CreatedAt, err = time.Parse(time.RFC3339, aux.CreatedAt)
	if err != nil {
		return err
	}

	up.UpdatedAt, err = time.Parse(time.RFC3339, aux.UpdatedAt)
	if err != nil {
		return err
	}

	if aux.LastLoginAt != nil {
		t, err := time.Parse(time.RFC3339, *aux.LastLoginAt)
		if err != nil {
			return err
		}
		up.LastLoginAt = &t
	}

	return nil
}
