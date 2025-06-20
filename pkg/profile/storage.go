package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Storage defines the interface for profile persistence
type Storage interface {
	// Save persists a profile to storage
	Save(profile *Profile) error

	// Load retrieves a profile by ID
	Load(profileID string) (*Profile, error)

	// LoadByUserID retrieves all profiles for a specific user
	LoadByUserID(userID string) ([]*Profile, error)

	// LoadAll retrieves all profiles
	LoadAll() ([]*Profile, error)

	// Delete removes a profile from storage
	Delete(profileID string) error

	// Update updates an existing profile
	Update(profile *Profile) error

	// Close cleans up any resources
	Close() error
}

// FileStorage implements profile storage using JSON files
type FileStorage struct {
	filePath string
	mutex    sync.RWMutex
	profiles map[string]*Profile
}

// NewFileStorage creates a new file-based profile storage
func NewFileStorage(filePath string) (*FileStorage, error) {
	fs := &FileStorage{
		filePath: filePath,
		profiles: make(map[string]*Profile),
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Load existing profiles
	if err := fs.loadFromFile(); err != nil {
		return nil, fmt.Errorf("failed to load profiles from file: %w", err)
	}

	return fs, nil
}

// Save persists a profile to storage
func (fs *FileStorage) Save(profile *Profile) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	if profile.ID == "" {
		profile.ID = uuid.New().String()
	}
	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()

	fs.profiles[profile.ID] = profile
	return fs.saveToFile()
}

// Load retrieves a profile by ID
func (fs *FileStorage) Load(profileID string) (*Profile, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	profile, exists := fs.profiles[profileID]
	if !exists {
		return nil, fmt.Errorf("profile not found: %s", profileID)
	}

	// Create a copy to avoid data races
	profileCopy := *profile
	return &profileCopy, nil
}

// LoadByUserID retrieves all profiles for a specific user
func (fs *FileStorage) LoadByUserID(userID string) ([]*Profile, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	var userProfiles []*Profile
	for _, profile := range fs.profiles {
		if profile.UserID == userID {
			profileCopy := *profile
			userProfiles = append(userProfiles, &profileCopy)
		}
	}

	return userProfiles, nil
}

// LoadAll retrieves all profiles
func (fs *FileStorage) LoadAll() ([]*Profile, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	var allProfiles []*Profile
	for _, profile := range fs.profiles {
		profileCopy := *profile
		allProfiles = append(allProfiles, &profileCopy)
	}

	return allProfiles, nil
}

// Delete removes a profile from storage
func (fs *FileStorage) Delete(profileID string) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	if _, exists := fs.profiles[profileID]; !exists {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	delete(fs.profiles, profileID)
	return fs.saveToFile()
}

// Update updates an existing profile
func (fs *FileStorage) Update(profile *Profile) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	if _, exists := fs.profiles[profile.ID]; !exists {
		return fmt.Errorf("profile not found: %s", profile.ID)
	}

	profile.UpdatedAt = time.Now()
	fs.profiles[profile.ID] = profile
	return fs.saveToFile()
}

// Close cleans up any resources
func (fs *FileStorage) Close() error {
	return nil
}

// loadFromFile loads profiles from the JSON file
func (fs *FileStorage) loadFromFile() error {
	if _, err := os.Stat(fs.filePath); os.IsNotExist(err) {
		// File doesn't exist, start with empty profiles
		return nil
	}

	data, err := os.ReadFile(fs.filePath)
	if err != nil {
		return fmt.Errorf("failed to read profiles file: %w", err)
	}

	if len(data) == 0 {
		// Empty file, start with empty profiles
		return nil
	}

	var profileList []*Profile
	if err := json.Unmarshal(data, &profileList); err != nil {
		return fmt.Errorf("failed to unmarshal profiles: %w", err)
	}

	for _, profile := range profileList {
		fs.profiles[profile.ID] = profile
	}

	return nil
}

// saveToFile saves profiles to the JSON file
func (fs *FileStorage) saveToFile() error {
	var profileList []*Profile
	for _, profile := range fs.profiles {
		profileList = append(profileList, profile)
	}

	data, err := json.MarshalIndent(profileList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}

	if err := os.WriteFile(fs.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write profiles file: %w", err)
	}

	return nil
}

// MemoryStorage implements in-memory profile storage for testing
type MemoryStorage struct {
	profiles map[string]*Profile
	mutex    sync.RWMutex
}

// NewMemoryStorage creates a new in-memory profile storage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		profiles: make(map[string]*Profile),
	}
}

// Save persists a profile to memory
func (ms *MemoryStorage) Save(profile *Profile) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if profile.ID == "" {
		profile.ID = uuid.New().String()
	}
	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()

	ms.profiles[profile.ID] = profile
	return nil
}

// Load retrieves a profile by ID
func (ms *MemoryStorage) Load(profileID string) (*Profile, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	profile, exists := ms.profiles[profileID]
	if !exists {
		return nil, fmt.Errorf("profile not found: %s", profileID)
	}

	profileCopy := *profile
	return &profileCopy, nil
}

// LoadByUserID retrieves all profiles for a specific user
func (ms *MemoryStorage) LoadByUserID(userID string) ([]*Profile, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	var userProfiles []*Profile
	for _, profile := range ms.profiles {
		if profile.UserID == userID {
			profileCopy := *profile
			userProfiles = append(userProfiles, &profileCopy)
		}
	}

	return userProfiles, nil
}

// LoadAll retrieves all profiles
func (ms *MemoryStorage) LoadAll() ([]*Profile, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	var allProfiles []*Profile
	for _, profile := range ms.profiles {
		profileCopy := *profile
		allProfiles = append(allProfiles, &profileCopy)
	}

	return allProfiles, nil
}

// Delete removes a profile from memory
func (ms *MemoryStorage) Delete(profileID string) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if _, exists := ms.profiles[profileID]; !exists {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	delete(ms.profiles, profileID)
	return nil
}

// Update updates an existing profile
func (ms *MemoryStorage) Update(profile *Profile) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if _, exists := ms.profiles[profile.ID]; !exists {
		return fmt.Errorf("profile not found: %s", profile.ID)
	}

	profile.UpdatedAt = time.Now()
	ms.profiles[profile.ID] = profile
	return nil
}

// Close cleans up any resources
func (ms *MemoryStorage) Close() error {
	return nil
}