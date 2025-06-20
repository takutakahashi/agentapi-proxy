package profile

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Service handles profile business logic
type Service struct {
	storage Storage
}

// NewService creates a new profile service
func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

// CreateProfile creates a new profile
func (s *Service) CreateProfile(userID string, req *CreateProfileRequest) (*Profile, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("profile name is required")
	}

	profile := &Profile{
		ID:                uuid.New().String(),
		UserID:            userID,
		Name:              req.Name,
		Description:       req.Description,
		Environment:       req.Environment,
		RepositoryHistory: req.RepositoryHistory,
		SystemPrompt:      req.SystemPrompt,
		MessageTemplates:  req.MessageTemplates,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if profile.Environment == nil {
		profile.Environment = make(map[string]string)
	}
	if profile.RepositoryHistory == nil {
		profile.RepositoryHistory = []RepositoryEntry{}
	}
	if profile.MessageTemplates == nil {
		profile.MessageTemplates = []MessageTemplate{}
	}

	if err := s.storage.Save(profile); err != nil {
		return nil, fmt.Errorf("failed to save profile: %w", err)
	}

	return profile, nil
}

// GetProfile retrieves a profile by ID
func (s *Service) GetProfile(profileID string) (*Profile, error) {
	return s.storage.Load(profileID)
}

// GetUserProfiles retrieves all profiles for a specific user
func (s *Service) GetUserProfiles(userID string) ([]*Profile, error) {
	return s.storage.LoadByUserID(userID)
}

// UpdateProfile updates an existing profile
func (s *Service) UpdateProfile(profileID string, req *UpdateProfileRequest) (*Profile, error) {
	profile, err := s.storage.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile: %w", err)
	}

	// Update fields if provided
	if req.Name != nil {
		profile.Name = *req.Name
	}
	if req.Description != nil {
		profile.Description = *req.Description
	}
	if req.Environment != nil {
		profile.Environment = req.Environment
	}
	if req.RepositoryHistory != nil {
		profile.RepositoryHistory = req.RepositoryHistory
	}
	if req.SystemPrompt != nil {
		profile.SystemPrompt = *req.SystemPrompt
	}
	if req.MessageTemplates != nil {
		profile.MessageTemplates = req.MessageTemplates
	}

	if err := s.storage.Update(profile); err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	return profile, nil
}

// DeleteProfile deletes a profile
func (s *Service) DeleteProfile(profileID string) error {
	return s.storage.Delete(profileID)
}

// AddRepositoryEntry adds a repository entry to a profile's history
func (s *Service) AddRepositoryEntry(profileID string, entry RepositoryEntry) error {
	profile, err := s.storage.Load(profileID)
	if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	// Generate ID if not provided
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	entry.AccessedAt = time.Now()

	// Check if repository already exists in history
	for i, existing := range profile.RepositoryHistory {
		if existing.URL == entry.URL {
			// Update existing entry
			profile.RepositoryHistory[i] = entry
			return s.storage.Update(profile)
		}
	}

	// Add new entry
	profile.RepositoryHistory = append(profile.RepositoryHistory, entry)
	return s.storage.Update(profile)
}

// AddMessageTemplate adds a message template to a profile
func (s *Service) AddMessageTemplate(profileID string, template MessageTemplate) error {
	profile, err := s.storage.Load(profileID)
	if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	// Generate ID if not provided
	if template.ID == "" {
		template.ID = uuid.New().String()
	}
	template.CreatedAt = time.Now()

	profile.MessageTemplates = append(profile.MessageTemplates, template)
	return s.storage.Update(profile)
}

// UpdateLastUsed updates the last used timestamp for a profile
func (s *Service) UpdateLastUsed(profileID string) error {
	profile, err := s.storage.Load(profileID)
	if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	now := time.Now()
	profile.LastUsedAt = &now
	return s.storage.Update(profile)
}

// MergeEnvironmentVariables merges profile environment variables with provided ones
// Provided variables take precedence over profile variables
func (s *Service) MergeEnvironmentVariables(profileID string, providedEnv map[string]string) (map[string]string, error) {
	profile, err := s.storage.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile: %w", err)
	}

	merged := make(map[string]string)

	// Start with profile environment variables
	for key, value := range profile.Environment {
		merged[key] = value
	}

	// Override with provided environment variables
	for key, value := range providedEnv {
		merged[key] = value
	}

	return merged, nil
}