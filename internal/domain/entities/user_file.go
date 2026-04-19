package entities

import (
	"errors"
	"time"
)

// ErrUserFilePathRequired is returned when a UserFile has an empty path.
var ErrUserFilePathRequired = errors.New("user file path is required")

// UserFile represents a user-managed file to be placed inside agent sessions.
// Files are stored in the agentapi-user-files-{userID} Kubernetes Secret and
// embedded into SessionSettings.Files at session creation time so that the
// provisioner writes them to their target paths (mode 0600).
type UserFile struct {
	id          string
	name        string
	path        string // destination path inside the container
	content     string // file content (plain text or base64)
	permissions string // e.g. "0600" (informational; provisioner uses 0600 by default)
	createdAt   time.Time
	updatedAt   time.Time
}

// NewUserFile creates a new UserFile entity.
func NewUserFile(id, name, path, content, permissions string) *UserFile {
	now := time.Now()
	return &UserFile{
		id:          id,
		name:        name,
		path:        path,
		content:     content,
		permissions: permissions,
		createdAt:   now,
		updatedAt:   now,
	}
}

// ID returns the file's unique identifier.
func (f *UserFile) ID() string { return f.id }

// Name returns the human-readable display name.
func (f *UserFile) Name() string { return f.name }

// SetName sets the display name.
func (f *UserFile) SetName(name string) { f.name = name }

// Path returns the destination path inside the container.
func (f *UserFile) Path() string { return f.path }

// SetPath sets the destination path.
func (f *UserFile) SetPath(path string) { f.path = path }

// Content returns the file content.
func (f *UserFile) Content() string { return f.content }

// SetContent replaces the file content and updates UpdatedAt.
func (f *UserFile) SetContent(content string) {
	f.content = content
	f.updatedAt = time.Now()
}

// Permissions returns the permissions string (e.g. "0600").
func (f *UserFile) Permissions() string { return f.permissions }

// SetPermissions sets the permissions string.
func (f *UserFile) SetPermissions(p string) { f.permissions = p }

// CreatedAt returns the creation timestamp.
func (f *UserFile) CreatedAt() time.Time { return f.createdAt }

// UpdatedAt returns the last update timestamp.
func (f *UserFile) UpdatedAt() time.Time { return f.updatedAt }

// SetCreatedAt sets the creation timestamp (used when loading from storage).
func (f *UserFile) SetCreatedAt(t time.Time) { f.createdAt = t }

// SetUpdatedAt sets the update timestamp (used when loading from storage).
func (f *UserFile) SetUpdatedAt(t time.Time) { f.updatedAt = t }

// Validate returns an error if the entity is invalid.
func (f *UserFile) Validate() error {
	if f.path == "" {
		return ErrUserFilePathRequired
	}
	return nil
}
