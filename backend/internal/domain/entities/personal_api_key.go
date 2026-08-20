package entities

import (
	"errors"
	"time"
)

// PersonalAPIKey represents a personal API key for a user
type PersonalAPIKey struct {
	userID    UserID
	apiKey    string
	createdAt time.Time
	updatedAt time.Time
}

// NewPersonalAPIKey creates a new personal API key
func NewPersonalAPIKey(userID UserID, apiKey string) *PersonalAPIKey {
	now := time.Now()
	return &PersonalAPIKey{
		userID:    userID,
		apiKey:    apiKey,
		createdAt: now,
		updatedAt: now,
	}
}

// UserID returns the user ID
func (p *PersonalAPIKey) UserID() UserID {
	return p.userID
}

// APIKey returns the API key
func (p *PersonalAPIKey) APIKey() string {
	return p.apiKey
}

// CreatedAt returns the creation time
func (p *PersonalAPIKey) CreatedAt() time.Time {
	return p.createdAt
}

// UpdatedAt returns the last update time
func (p *PersonalAPIKey) UpdatedAt() time.Time {
	return p.updatedAt
}

// UpdateAPIKey updates the API key and sets the updated time
func (p *PersonalAPIKey) UpdateAPIKey(apiKey string) {
	p.apiKey = apiKey
	p.updatedAt = time.Now()
}

// SetCreatedAt sets the created at time (for deserialization)
func (p *PersonalAPIKey) SetCreatedAt(t time.Time) {
	p.createdAt = t
}

// SetUpdatedAt sets the updated at time (for deserialization)
func (p *PersonalAPIKey) SetUpdatedAt(t time.Time) {
	p.updatedAt = t
}

// Validate validates the personal API key
func (p *PersonalAPIKey) Validate() error {
	if p.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if p.apiKey == "" {
		return errors.New("API key cannot be empty")
	}

	if p.createdAt.IsZero() {
		return errors.New("created at time cannot be zero")
	}

	if p.updatedAt.IsZero() {
		return errors.New("updated at time cannot be zero")
	}

	return nil
}
