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

// CreateUserProfiles creates a new user profiles collection
func (s *Service) CreateUserProfiles(ctx context.Context, userID, username, email, displayName string, profileName string) (*UserProfiles, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	// Check if user profiles already exist
	exists, err := s.storage.Exists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check profile existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("user profiles already exist for user %s", userID)
	}

	// Create new user profiles
	userProfiles := NewUserProfiles(userID)
	userProfiles.Username = username
	userProfiles.Email = email
	userProfiles.DisplayName = displayName

	// Create default profile if profile name provided
	if profileName != "" {
		defaultProfile := NewProfileConfig(profileName)
		defaultProfile.IsDefault = true
		userProfiles.Profiles = append(userProfiles.Profiles, *defaultProfile)
	}

	// Save user profiles
	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return nil, fmt.Errorf("failed to save user profiles: %w", err)
	}

	return userProfiles, nil
}

// CreateProfile creates a new profile configuration for a user
func (s *Service) CreateProfile(ctx context.Context, userID, profileName string) (*ProfileConfig, error) {
	if userID == "" || profileName == "" {
		return nil, ErrInvalidProfile
	}

	// Load existing user profiles
	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	// Create new profile
	newProfile := NewProfileConfig(profileName)

	// Add profile to user profiles
	if err := userProfiles.AddProfile(newProfile); err != nil {
		return nil, fmt.Errorf("failed to add profile: %w", err)
	}

	// Save updated user profiles
	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return nil, fmt.Errorf("failed to save user profiles: %w", err)
	}

	return newProfile, nil
}

// GetUserProfiles retrieves user profiles by user ID
func (s *Service) GetUserProfiles(ctx context.Context, userID string) (*UserProfiles, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	return userProfiles, nil
}

// GetProfile retrieves a specific profile by user ID and profile name
func (s *Service) GetProfile(ctx context.Context, userID, profileName string) (*ProfileConfig, error) {
	if userID == "" || profileName == "" {
		return nil, ErrInvalidProfile
	}

	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	profile, err := userProfiles.GetProfile(profileName)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}

	return profile, nil
}

// GetDefaultProfile retrieves the default profile for a user
func (s *Service) GetDefaultProfile(ctx context.Context, userID string) (*ProfileConfig, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	defaultProfile := userProfiles.GetDefaultProfile()
	if defaultProfile == nil {
		return nil, fmt.Errorf("no default profile found")
	}

	return defaultProfile, nil
}

// ListProfiles returns all profile names for a user
func (s *Service) ListProfiles(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	return userProfiles.ListProfileNames(), nil
}

// UpdateProfile updates a specific profile by name
func (s *Service) UpdateProfile(ctx context.Context, userID, profileName string, update *ProfileConfigUpdate) (*ProfileConfig, error) {
	if userID == "" || profileName == "" || update == nil {
		return nil, ErrInvalidProfile
	}

	// Load user profiles
	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user profiles: %w", err)
	}

	// Update the specific profile
	if err := userProfiles.UpdateProfile(profileName, update); err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	// Save updated user profiles
	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return nil, fmt.Errorf("failed to save user profiles: %w", err)
	}

	// Return updated profile
	return userProfiles.GetProfile(profileName)
}

// UpdateUserProfiles updates user-level information
func (s *Service) UpdateUserProfiles(ctx context.Context, userID string, update *UserProfilesUpdate) (*UserProfiles, error) {
	if userID == "" || update == nil {
		return nil, ErrInvalidProfile
	}

	// Update the user profiles
	if err := s.storage.Update(ctx, userID, update); err != nil {
		return nil, fmt.Errorf("failed to update user profiles: %w", err)
	}

	// Return updated user profiles
	return s.storage.Load(ctx, userID)
}

// DeleteProfile deletes a specific profile by name
func (s *Service) DeleteProfile(ctx context.Context, userID, profileName string) error {
	if userID == "" || profileName == "" {
		return ErrInvalidProfile
	}

	// Load user profiles
	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to load user profiles: %w", err)
	}

	// Delete the specific profile
	if err := userProfiles.DeleteProfile(profileName); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	// Save updated user profiles
	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return fmt.Errorf("failed to save user profiles: %w", err)
	}

	return nil
}

// DeleteUserProfiles deletes all profiles for a user
func (s *Service) DeleteUserProfiles(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidProfile
	}

	if err := s.storage.Delete(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete user profiles: %w", err)
	}

	return nil
}

// SetDefaultProfile sets a specific profile as default
func (s *Service) SetDefaultProfile(ctx context.Context, userID, profileName string) error {
	if userID == "" || profileName == "" {
		return ErrInvalidProfile
	}

	// Load user profiles
	userProfiles, err := s.storage.Load(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to load user profiles: %w", err)
	}

	// Set the default profile
	if err := userProfiles.SetDefaultProfile(profileName); err != nil {
		return fmt.Errorf("failed to set default profile: %w", err)
	}

	// Save updated user profiles
	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return fmt.Errorf("failed to save user profiles: %w", err)
	}

	return nil
}

// ListAllUsers returns all user IDs (for admin purposes)
func (s *Service) ListAllUsers(ctx context.Context) ([]string, error) {
	return s.storage.List(ctx)
}

// ProfileExists checks if a profile exists
func (s *Service) ProfileExists(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, ErrInvalidProfile
	}

	return s.storage.Exists(ctx, userID)
}

// UpdateLastLogin updates the last login timestamp for a user
func (s *Service) UpdateLastLogin(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidProfile
	}

	now := time.Now()

	// Load existing user profiles to update last login timestamp
	userProfiles, err := s.storage.Load(ctx, userID)
	if err == ErrProfileNotFound {
		// Create new user profiles if they don't exist
		_, err = s.CreateUserProfiles(ctx, userID, "", "", "", "default")
		if err != nil {
			return fmt.Errorf("failed to create user profiles: %w", err)
		}
		userProfiles, err = s.storage.Load(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to load newly created user profiles: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to load user profiles: %w", err)
	}

	userProfiles.LastLoginAt = &now

	if err := s.storage.Save(ctx, userProfiles); err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	return nil
}

// SetPreference sets a specific preference for a user's default profile
func (s *Service) SetPreference(ctx context.Context, userID, key string, value interface{}) error {
	if userID == "" || key == "" {
		return ErrInvalidProfile
	}

	// Get the default profile
	defaultProfile, err := s.GetDefaultProfile(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get default profile: %w", err)
	}

	update := &ProfileConfigUpdate{
		Preferences: map[string]interface{}{
			key: value,
		},
	}

	_, err = s.UpdateProfile(ctx, userID, defaultProfile.Name, update)
	return err
}

// GetPreference gets a specific preference from a user's default profile
func (s *Service) GetPreference(ctx context.Context, userID, key string) (interface{}, error) {
	if userID == "" || key == "" {
		return nil, ErrInvalidProfile
	}

	defaultProfile, err := s.GetDefaultProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	value, exists := defaultProfile.Preferences[key]
	if !exists {
		return nil, fmt.Errorf("preference %s not found", key)
	}

	return value, nil
}

// SetSetting sets a specific setting for a user's default profile
func (s *Service) SetSetting(ctx context.Context, userID, key string, value interface{}) error {
	if userID == "" || key == "" {
		return ErrInvalidProfile
	}

	// Get the default profile
	defaultProfile, err := s.GetDefaultProfile(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get default profile: %w", err)
	}

	update := &ProfileConfigUpdate{
		Settings: map[string]interface{}{
			key: value,
		},
	}

	_, err = s.UpdateProfile(ctx, userID, defaultProfile.Name, update)
	return err
}

// GetSetting gets a specific setting from a user's default profile
func (s *Service) GetSetting(ctx context.Context, userID, key string) (interface{}, error) {
	if userID == "" || key == "" {
		return nil, ErrInvalidProfile
	}

	defaultProfile, err := s.GetDefaultProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	value, exists := defaultProfile.Settings[key]
	if !exists {
		return nil, fmt.Errorf("setting %s not found", key)
	}

	return value, nil
}
