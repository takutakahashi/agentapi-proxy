package entities

import (
	"errors"
	"time"
)

// ServiceAccount represents a service account domain entity
type ServiceAccount struct {
	teamID      string
	userID      UserID
	apiKey      string
	permissions []Permission
	createdAt   time.Time
	updatedAt   time.Time
}

// NewServiceAccount creates a new service account
func NewServiceAccount(teamID string, userID UserID, apiKey string, permissions []Permission) *ServiceAccount {
	now := time.Now()
	return &ServiceAccount{
		teamID:      teamID,
		userID:      userID,
		apiKey:      apiKey,
		permissions: permissions,
		createdAt:   now,
		updatedAt:   now,
	}
}

// TeamID returns the team ID
func (sa *ServiceAccount) TeamID() string {
	return sa.teamID
}

// UserID returns the user ID
func (sa *ServiceAccount) UserID() UserID {
	return sa.userID
}

// APIKey returns the API key
func (sa *ServiceAccount) APIKey() string {
	return sa.apiKey
}

// Permissions returns the permissions
func (sa *ServiceAccount) Permissions() []Permission {
	return sa.permissions
}

// CreatedAt returns the creation time
func (sa *ServiceAccount) CreatedAt() time.Time {
	return sa.createdAt
}

// UpdatedAt returns the last update time
func (sa *ServiceAccount) UpdatedAt() time.Time {
	return sa.updatedAt
}

// UpdateAPIKey updates the API key and sets the updated time
func (sa *ServiceAccount) UpdateAPIKey(apiKey string) {
	sa.apiKey = apiKey
	sa.updatedAt = time.Now()
}

// SetCreatedAt sets the created at time (for deserialization)
func (sa *ServiceAccount) SetCreatedAt(t time.Time) {
	sa.createdAt = t
}

// SetUpdatedAt sets the updated at time (for deserialization)
func (sa *ServiceAccount) SetUpdatedAt(t time.Time) {
	sa.updatedAt = t
}

// Validate validates the service account
func (sa *ServiceAccount) Validate() error {
	if sa.teamID == "" {
		return errors.New("team ID cannot be empty")
	}

	if sa.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if sa.apiKey == "" {
		return errors.New("API key cannot be empty")
	}

	if len(sa.permissions) == 0 {
		return errors.New("permissions cannot be empty")
	}

	if sa.createdAt.IsZero() {
		return errors.New("created at time cannot be zero")
	}

	if sa.updatedAt.IsZero() {
		return errors.New("updated at time cannot be zero")
	}

	return nil
}
