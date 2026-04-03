package entities

import (
	"encoding/json"
	"time"
)

// Credentials represents a named credential file (e.g., auth.json) stored for a user or team
type Credentials struct {
	name      string
	data      json.RawMessage // Raw JSON content of the credential file
	createdAt time.Time
	updatedAt time.Time
}

// NewCredentials creates a new Credentials entity
func NewCredentials(name string, data json.RawMessage) *Credentials {
	now := time.Now()
	return &Credentials{
		name:      name,
		data:      data,
		createdAt: now,
		updatedAt: now,
	}
}

// Name returns the credential name
func (c *Credentials) Name() string {
	return c.name
}

// SetName sets the credential name
func (c *Credentials) SetName(name string) {
	c.name = name
}

// Data returns the raw JSON data of the credential
func (c *Credentials) Data() json.RawMessage {
	return c.data
}

// SetData sets the raw JSON data
func (c *Credentials) SetData(data json.RawMessage) {
	c.data = data
	c.updatedAt = time.Now()
}

// CreatedAt returns the creation time
func (c *Credentials) CreatedAt() time.Time {
	return c.createdAt
}

// UpdatedAt returns the last update time
func (c *Credentials) UpdatedAt() time.Time {
	return c.updatedAt
}

// SetCreatedAt sets the creation time (for loading from storage)
func (c *Credentials) SetCreatedAt(t time.Time) {
	c.createdAt = t
}

// SetUpdatedAt sets the update time (for loading from storage)
func (c *Credentials) SetUpdatedAt(t time.Time) {
	c.updatedAt = t
}

// Validate validates the Credentials entity
func (c *Credentials) Validate() error {
	if c.name == "" {
		return nil // name is set from URL param or storage metadata
	}
	// Validate JSON is valid
	if len(c.data) > 0 {
		var tmp interface{}
		if err := json.Unmarshal(c.data, &tmp); err != nil {
			return err
		}
	}
	return nil
}
