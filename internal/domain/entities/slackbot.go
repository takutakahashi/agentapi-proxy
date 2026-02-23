package entities

import (
	"fmt"
	"time"
)

// SlackBotStatus defines the current status of a SlackBot
type SlackBotStatus string

const (
	// SlackBotStatusActive indicates the SlackBot is active and will process events
	SlackBotStatusActive SlackBotStatus = "active"
	// SlackBotStatusPaused indicates the SlackBot is paused
	SlackBotStatusPaused SlackBotStatus = "paused"
)

// SlackBot represents a Slack bot registration entity.
// Each SlackBot corresponds to a Slack App installation (one signing secret, one bot token)
// and gets its own hook URL: /hooks/slack/:id
type SlackBot struct {
	id                 string
	name               string
	userID             string
	scope              ResourceScope
	teamID             string
	status             SlackBotStatus
	signingSecret      string
	botTokenSecretName string                // K8s Secret name for xoxb-... token; empty = use global default
	botTokenSecretKey  string                // Key within the Secret; default: "bot-token"
	allowedEventTypes  []string              // Empty means all event types
	allowedChannelIDs  []string              // Empty means all channels
	sessionConfig      *WebhookSessionConfig // Reuse existing type
	maxSessions        int
	createdAt          time.Time
	updatedAt          time.Time
}

// NewSlackBot creates a new SlackBot entity with defaults
func NewSlackBot(id, name, userID string) *SlackBot {
	now := time.Now()
	return &SlackBot{
		id:          id,
		name:        name,
		userID:      userID,
		status:      SlackBotStatusActive,
		scope:       ScopeUser,
		maxSessions: 10,
		createdAt:   now,
		updatedAt:   now,
	}
}

// ID returns the SlackBot ID (also used as the hook URL path: /hooks/slack/:id)
func (s *SlackBot) ID() string { return s.id }

// Name returns the SlackBot name
func (s *SlackBot) Name() string { return s.name }

// SetName sets the SlackBot name
func (s *SlackBot) SetName(name string) {
	s.name = name
	s.updatedAt = time.Now()
}

// UserID returns the user ID
func (s *SlackBot) UserID() string { return s.userID }

// Scope returns the resource scope (defaults to user)
func (s *SlackBot) Scope() ResourceScope {
	if s.scope == "" {
		return ScopeUser
	}
	return s.scope
}

// SetScope sets the resource scope
func (s *SlackBot) SetScope(scope ResourceScope) {
	s.scope = scope
	s.updatedAt = time.Now()
}

// TeamID returns the team ID
func (s *SlackBot) TeamID() string { return s.teamID }

// SetTeamID sets the team ID
func (s *SlackBot) SetTeamID(teamID string) {
	s.teamID = teamID
	s.updatedAt = time.Now()
}

// Status returns the SlackBot status
func (s *SlackBot) Status() SlackBotStatus { return s.status }

// SetStatus sets the SlackBot status
func (s *SlackBot) SetStatus(status SlackBotStatus) {
	s.status = status
	s.updatedAt = time.Now()
}

// SigningSecret returns the Slack App signing secret
func (s *SlackBot) SigningSecret() string { return s.signingSecret }

// SetSigningSecret sets the Slack App signing secret
func (s *SlackBot) SetSigningSecret(secret string) {
	s.signingSecret = secret
	s.updatedAt = time.Now()
}

// MaskSigningSecret returns the signing secret with only the last 4 characters visible
func (s *SlackBot) MaskSigningSecret() string {
	if len(s.signingSecret) <= 4 {
		return "****"
	}
	return "****" + s.signingSecret[len(s.signingSecret)-4:]
}

// BotTokenSecretName returns the K8s Secret name for the Slack bot token.
// Empty means the global default should be used.
func (s *SlackBot) BotTokenSecretName() string { return s.botTokenSecretName }

// SetBotTokenSecretName sets the K8s Secret name for the Slack bot token
func (s *SlackBot) SetBotTokenSecretName(name string) {
	s.botTokenSecretName = name
	s.updatedAt = time.Now()
}

// BotTokenSecretKey returns the key within the K8s Secret for the bot token.
// Defaults to "bot-token".
func (s *SlackBot) BotTokenSecretKey() string {
	if s.botTokenSecretKey == "" {
		return "bot-token"
	}
	return s.botTokenSecretKey
}

// SetBotTokenSecretKey sets the key within the K8s Secret for the bot token
func (s *SlackBot) SetBotTokenSecretKey(key string) {
	s.botTokenSecretKey = key
	s.updatedAt = time.Now()
}

// AllowedEventTypes returns the list of allowed Slack event types.
// Empty means all event types are allowed.
func (s *SlackBot) AllowedEventTypes() []string { return s.allowedEventTypes }

// SetAllowedEventTypes sets the list of allowed Slack event types
func (s *SlackBot) SetAllowedEventTypes(types []string) {
	s.allowedEventTypes = types
	s.updatedAt = time.Now()
}

// AllowedChannelIDs returns the list of allowed Slack channel IDs.
// Empty means all channels are allowed.
func (s *SlackBot) AllowedChannelIDs() []string { return s.allowedChannelIDs }

// SetAllowedChannelIDs sets the list of allowed Slack channel IDs
func (s *SlackBot) SetAllowedChannelIDs(channelIDs []string) {
	s.allowedChannelIDs = channelIDs
	s.updatedAt = time.Now()
}

// SessionConfig returns the session configuration
func (s *SlackBot) SessionConfig() *WebhookSessionConfig { return s.sessionConfig }

// SetSessionConfig sets the session configuration
func (s *SlackBot) SetSessionConfig(config *WebhookSessionConfig) {
	s.sessionConfig = config
	s.updatedAt = time.Now()
}

// MaxSessions returns the maximum concurrent sessions allowed for this SlackBot.
// Defaults to 10 if not set or 0.
func (s *SlackBot) MaxSessions() int {
	if s.maxSessions <= 0 {
		return 10
	}
	return s.maxSessions
}

// SetMaxSessions sets the maximum concurrent sessions
func (s *SlackBot) SetMaxSessions(max int) {
	s.maxSessions = max
	s.updatedAt = time.Now()
}

// CreatedAt returns the creation time
func (s *SlackBot) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns the last update time
func (s *SlackBot) UpdatedAt() time.Time { return s.updatedAt }

// SetCreatedAt sets the creation time (used during deserialization)
func (s *SlackBot) SetCreatedAt(t time.Time) { s.createdAt = t }

// SetUpdatedAt sets the update time (used during deserialization)
func (s *SlackBot) SetUpdatedAt(t time.Time) { s.updatedAt = t }

// IsEventTypeAllowed returns true if the given event type is allowed
func (s *SlackBot) IsEventTypeAllowed(eventType string) bool {
	if len(s.allowedEventTypes) == 0 {
		return true
	}
	for _, t := range s.allowedEventTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// IsChannelAllowed returns true if the given channel ID is allowed
func (s *SlackBot) IsChannelAllowed(channelID string) bool {
	if len(s.allowedChannelIDs) == 0 {
		return true
	}
	for _, c := range s.allowedChannelIDs {
		if c == channelID {
			return true
		}
	}
	return false
}

// Validate validates the SlackBot configuration
func (s *SlackBot) Validate() error {
	if s.id == "" {
		return ErrInvalidSlackBot{Field: "id", Message: "id is required"}
	}
	if s.name == "" {
		return ErrInvalidSlackBot{Field: "name", Message: "name is required"}
	}
	if s.userID == "" {
		return ErrInvalidSlackBot{Field: "user_id", Message: "user_id is required"}
	}
	if s.signingSecret == "" {
		return ErrInvalidSlackBot{Field: "signing_secret", Message: "signing_secret is required"}
	}
	if s.maxSessions < 0 {
		return ErrInvalidSlackBot{Field: "max_sessions", Message: "max_sessions must be non-negative"}
	}
	return nil
}

// ErrInvalidSlackBot is returned when a SlackBot fails validation
type ErrInvalidSlackBot struct {
	Field   string
	Message string
}

func (e ErrInvalidSlackBot) Error() string {
	return fmt.Sprintf("invalid slackbot %s: %s", e.Field, e.Message)
}

// ErrSlackBotNotFound is returned when a SlackBot is not found
type ErrSlackBotNotFound struct {
	ID string
}

func (e ErrSlackBotNotFound) Error() string {
	return fmt.Sprintf("slackbot not found: %s", e.ID)
}
