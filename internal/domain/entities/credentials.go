package entities

import (
	"encoding/json"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Credentials represents a named credential stored for a user or team.
// A single Credentials value may represent either:
//   - a single-file upload (FileType is set, Data holds the raw JSON)
//   - the aggregate view of all files for a user (Files is set, Data is nil)
type Credentials struct {
	name      string
	fileType  string                        // e.g. "codex_auth" or "claude_credentials"
	data      json.RawMessage               // raw JSON content of the uploaded file (nil for aggregates)
	files     []sessionsettings.ManagedFile // all managed files (populated by FindByName)
	createdAt time.Time
	updatedAt time.Time
}

// NewCredentials creates a new Credentials entity for a single-file upload.
// Pass nil for data when creating an aggregate returned by FindByName.
func NewCredentials(name string, data json.RawMessage) *Credentials {
	now := time.Now()
	return &Credentials{
		name:      name,
		data:      data,
		createdAt: now,
		updatedAt: now,
	}
}

// Name returns the credential name.
func (c *Credentials) Name() string { return c.name }

// SetName sets the credential name.
func (c *Credentials) SetName(name string) { c.name = name }

// FileType returns the file type (e.g. "codex_auth").
func (c *Credentials) FileType() string { return c.fileType }

// SetFileType sets the file type.
func (c *Credentials) SetFileType(ft string) { c.fileType = ft }

// Data returns the raw JSON content of this credential file.
// Returns nil for aggregate Credentials returned by FindByName.
func (c *Credentials) Data() json.RawMessage { return c.data }

// SetData replaces the raw JSON content and updates UpdatedAt.
func (c *Credentials) SetData(data json.RawMessage) {
	c.data = data
	c.updatedAt = time.Now()
}

// Files returns all managed files stored in this credential Secret.
// Populated only by FindByName; empty for single-file uploads.
func (c *Credentials) Files() []sessionsettings.ManagedFile { return c.files }

// SetFiles sets the managed files list.
func (c *Credentials) SetFiles(files []sessionsettings.ManagedFile) { c.files = files }

// HasData returns true if any credential data is present (either raw data or stored files).
func (c *Credentials) HasData() bool {
	return len(c.data) > 0 || len(c.files) > 0
}

// CreatedAt returns the creation time.
func (c *Credentials) CreatedAt() time.Time { return c.createdAt }

// UpdatedAt returns the last update time.
func (c *Credentials) UpdatedAt() time.Time { return c.updatedAt }

// SetCreatedAt sets the creation time (used when loading from storage).
func (c *Credentials) SetCreatedAt(t time.Time) { c.createdAt = t }

// SetUpdatedAt sets the update time (used when loading from storage).
func (c *Credentials) SetUpdatedAt(t time.Time) { c.updatedAt = t }

// Validate validates the Credentials entity.
func (c *Credentials) Validate() error {
	if len(c.data) > 0 {
		var tmp interface{}
		if err := json.Unmarshal(c.data, &tmp); err != nil {
			return err
		}
	}
	return nil
}
