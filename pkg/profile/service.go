package profile

import (
	"context"
	"fmt"
	"time"
)

// Service provides profile management operations
type Service struct {
	storage Storage
}

// NewService creates a new profile service
func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

// CreateProfile creates a new profile for a user
func (s *Service) CreateProfile(ctx context.Context, userID, username, email, displayName string) (*Profile, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	// Check if profile already exists
	exists, err := s.storage.Exists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check profile existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("profile already exists for user %s", userID)
	}

	// Create new profile
	profile := NewProfile(userID)
	profile.Username = username
	profile.Email = email
	profile.DisplayName = displayName

	// Save profile
	if err := s.storage.Save(ctx, profile); err != nil {
		return nil, fmt.Errorf("failed to save profile: %w", err)
	}

	return profile, nil
}

// GetProfile retrieves a profile by user ID
func (s *Service) GetProfile(ctx context.Context, userID string) (*Profile, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	profile, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile: %w", err)
	}

	return profile, nil
}

// UpdateProfile updates a profile
func (s *Service) UpdateProfile(ctx context.Context, userID string, update *ProfileUpdate) (*Profile, error) {
	if userID == "" || update == nil {
		return nil, ErrInvalidProfile
	}

	// Update the profile
	if err := s.storage.Update(ctx, userID, update); err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	// Return updated profile
	return s.storage.Load(ctx, userID)
}

// DeleteProfile deletes a profile
func (s *Service) DeleteProfile(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidProfile
	}

	if err := s.storage.Delete(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	return nil
}

// ProfileExists checks if a profile exists
func (s *Service) ProfileExists(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, ErrInvalidProfile
	}

	return s.storage.Exists(ctx, userID)
}

// ListProfiles returns all profile IDs
func (s *Service) ListProfiles(ctx context.Context) ([]string, error) {
	return s.storage.List(ctx)
}

// UpdateLastLogin updates the last login timestamp for a user
func (s *Service) UpdateLastLogin(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidProfile
	}

	now := time.Now()

	// Load existing profile to update last login timestamp
	profile, err := s.storage.Load(ctx, userID)
	if err == ErrProfileNotFound {
		// Create a new profile if it doesn't exist
		_, err = s.CreateProfile(ctx, userID, "", "", "")
		if err != nil {
			return fmt.Errorf("failed to create profile: %w", err)
		}
		profile, err = s.storage.Load(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to load newly created profile: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	profile.LastLoginAt = &now

	if err := s.storage.Save(ctx, profile); err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	return nil
}

// SetPreference sets a specific preference for a user
func (s *Service) SetPreference(ctx context.Context, userID, key string, value interface{}) error {
	if userID == "" || key == "" {
		return ErrInvalidProfile
	}

	update := &ProfileUpdate{
		Preferences: map[string]interface{}{
			key: value,
		},
	}

	_, err := s.UpdateProfile(ctx, userID, update)
	return err
}

// GetPreference gets a specific preference for a user
func (s *Service) GetPreference(ctx context.Context, userID, key string) (interface{}, error) {
	if userID == "" || key == "" {
		return nil, ErrInvalidProfile
	}

	profile, err := s.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	value, exists := profile.Preferences[key]
	if !exists {
		return nil, fmt.Errorf("preference %s not found", key)
	}

	return value, nil
}

// SetSetting sets a specific setting for a user
func (s *Service) SetSetting(ctx context.Context, userID, key string, value interface{}) error {
	if userID == "" || key == "" {
		return ErrInvalidProfile
	}

	update := &ProfileUpdate{
		Settings: map[string]interface{}{
			key: value,
		},
	}

	_, err := s.UpdateProfile(ctx, userID, update)
	return err
}

// GetSetting gets a specific setting for a user
func (s *Service) GetSetting(ctx context.Context, userID, key string) (interface{}, error) {
	if userID == "" || key == "" {
		return nil, ErrInvalidProfile
	}

	profile, err := s.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	value, exists := profile.Settings[key]
	if !exists {
		return nil, fmt.Errorf("setting %s not found", key)
	}

	return value, nil
}
