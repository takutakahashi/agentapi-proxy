package profile

import (
	"encoding/json"
	"time"
)

// Profile represents a user's profile with customizable settings and preferences
type Profile struct {
	UserID      string                 `json:"user_id"`
	Username    string                 `json:"username"`
	Email       string                 `json:"email"`
	DisplayName string                 `json:"display_name"`
	Preferences map[string]interface{} `json:"preferences"`
	Settings    map[string]interface{} `json:"settings"`
	Metadata    map[string]string      `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	LastLoginAt *time.Time             `json:"last_login_at,omitempty"`
}

// NewProfile creates a new profile with the given user ID
func NewProfile(userID string) *Profile {
	now := time.Now()
	return &Profile{
		UserID:      userID,
		Preferences: make(map[string]interface{}),
		Settings:    make(map[string]interface{}),
		Metadata:    make(map[string]string),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// Update updates the profile fields with non-empty values from the update
func (p *Profile) Update(update *ProfileUpdate) {
	if update.Username != "" {
		p.Username = update.Username
	}
	if update.Email != "" {
		p.Email = update.Email
	}
	if update.DisplayName != "" {
		p.DisplayName = update.DisplayName
	}
	if update.Preferences != nil {
		for k, v := range update.Preferences {
			p.Preferences[k] = v
		}
	}
	if update.Settings != nil {
		for k, v := range update.Settings {
			p.Settings[k] = v
		}
	}
	if update.Metadata != nil {
		for k, v := range update.Metadata {
			p.Metadata[k] = v
		}
	}
	p.UpdatedAt = time.Now()
}

// ProfileUpdate represents fields that can be updated in a profile
type ProfileUpdate struct {
	Username    string                 `json:"username,omitempty"`
	Email       string                 `json:"email,omitempty"`
	DisplayName string                 `json:"display_name,omitempty"`
	Preferences map[string]interface{} `json:"preferences,omitempty"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
}

// MarshalJSON implements custom JSON marshaling
func (p *Profile) MarshalJSON() ([]byte, error) {
	type Alias Profile
	return json.Marshal(&struct {
		*Alias
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   string  `json:"updated_at"`
		LastLoginAt *string `json:"last_login_at,omitempty"`
	}{
		Alias:     (*Alias)(p),
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.Format(time.RFC3339),
		LastLoginAt: func() *string {
			if p.LastLoginAt != nil {
				s := p.LastLoginAt.Format(time.RFC3339)
				return &s
			}
			return nil
		}(),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling
func (p *Profile) UnmarshalJSON(data []byte) error {
	type Alias Profile
	aux := &struct {
		*Alias
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   string  `json:"updated_at"`
		LastLoginAt *string `json:"last_login_at,omitempty"`
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	p.CreatedAt, err = time.Parse(time.RFC3339, aux.CreatedAt)
	if err != nil {
		return err
	}

	p.UpdatedAt, err = time.Parse(time.RFC3339, aux.UpdatedAt)
	if err != nil {
		return err
	}

	if aux.LastLoginAt != nil {
		t, err := time.Parse(time.RFC3339, *aux.LastLoginAt)
		if err != nil {
			return err
		}
		p.LastLoginAt = &t
	}

	return nil
}
