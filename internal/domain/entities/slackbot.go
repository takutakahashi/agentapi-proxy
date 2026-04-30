package entities

import (
	"fmt"
	"strings"
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
// Each SlackBot corresponds to a Slack App installation (one bot token).
// In Socket Mode, events are received via WebSocket rather than HTTP webhook.
type SlackBot struct {
	id                     string
	name                   string
	userID                 string
	scope                  ResourceScope
	teamID                 string
	teams                  []string // GitHub team slugs (e.g., ["org/team-a"]) resolved at create/update time
	status                 SlackBotStatus
	botTokenSecretName     string                // K8s Secret name for xoxb-... token; empty = use global default
	botTokenSecretKey      string                // Key within the Secret; default: "bot-token"
	appTokenSecretKey      string                // Key within botTokenSecretName Secret for xapp-... token; default: "app-token"
	allowedEventTypes      []string              // Empty means all event types
	allowedChannelNames    []string              // Empty means all channels; prefix match on resolved channel name
	sessionConfig          *WebhookSessionConfig // Reuse existing type
	maxSessions            int
	notifyOnSessionCreated *bool // nil means true (default: notify)
	allowBotMessages       *bool // nil means false (default: ignore bot messages)
	createdAt              time.Time
	updatedAt              time.Time

	// Transient write-only fields - stored directly in K8s Secret, never serialized
	botToken string // xoxb-... token value (write-only)
	appToken string // xapp-... token value (write-only)
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

// Teams returns the GitHub team slugs stored at bot creation/update time.
// These are used to merge team-level settings (MCP servers, env vars, etc.)
// into sessions created by this bot.
func (s *SlackBot) Teams() []string {
	result := make([]string, len(s.teams))
	copy(result, s.teams)
	return result
}

// SetTeams stores the GitHub team slugs for settings merging.
// This should be called with the bot owner's current team memberships.
func (s *SlackBot) SetTeams(teams []string) {
	s.teams = teams
	s.updatedAt = time.Now()
}

// Status returns the SlackBot status
func (s *SlackBot) Status() SlackBotStatus { return s.status }

// SetStatus sets the SlackBot status
func (s *SlackBot) SetStatus(status SlackBotStatus) {
	s.status = status
	s.updatedAt = time.Now()
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

// AppTokenSecretKey returns the key within the K8s Secret for the App-level token (xapp-...).
// The App-level token is stored in the same Secret as the bot token.
// Defaults to "app-token".
func (s *SlackBot) AppTokenSecretKey() string {
	if s.appTokenSecretKey == "" {
		return "app-token"
	}
	return s.appTokenSecretKey
}

// SetAppTokenSecretKey sets the key within the K8s Secret for the App-level token
func (s *SlackBot) SetAppTokenSecretKey(key string) {
	s.appTokenSecretKey = key
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

// AllowedChannelNames returns the list of allowed Slack channel name patterns.
// Empty means all channels are allowed. Matching is prefix (前方一致).
func (s *SlackBot) AllowedChannelNames() []string { return s.allowedChannelNames }

// SetAllowedChannelNames sets the list of allowed Slack channel name patterns
func (s *SlackBot) SetAllowedChannelNames(names []string) {
	s.allowedChannelNames = names
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

// NotifyOnSessionCreated returns whether the SlackBot should post a notification
// to Slack when a new session is created. Defaults to true if not explicitly set.
func (s *SlackBot) NotifyOnSessionCreated() bool {
	if s.notifyOnSessionCreated == nil {
		return true
	}
	return *s.notifyOnSessionCreated
}

// SetNotifyOnSessionCreated sets whether the SlackBot should post a notification
// to Slack when a new session is created.
func (s *SlackBot) SetNotifyOnSessionCreated(v *bool) {
	s.notifyOnSessionCreated = v
	s.updatedAt = time.Now()
}

// RawNotifyOnSessionCreated returns the raw pointer for serialization.
// nil means the field was never explicitly set (treated as true).
func (s *SlackBot) RawNotifyOnSessionCreated() *bool {
	return s.notifyOnSessionCreated
}

// AllowBotMessages returns whether the SlackBot should process messages from bots.
// Defaults to false (bot messages are ignored to prevent recursive session creation).
func (s *SlackBot) AllowBotMessages() bool {
	if s.allowBotMessages == nil {
		return false
	}
	return *s.allowBotMessages
}

// SetAllowBotMessages sets whether the SlackBot should process messages from bots.
func (s *SlackBot) SetAllowBotMessages(v *bool) {
	s.allowBotMessages = v
	s.updatedAt = time.Now()
}

// RawAllowBotMessages returns the raw pointer for serialization.
// nil means the field was never explicitly set (treated as false).
func (s *SlackBot) RawAllowBotMessages() *bool {
	return s.allowBotMessages
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

// IsChannelNameAllowed returns true if the given channel name matches any of the allowed patterns.
// Matching is prefix (前方一致): a pattern is considered matched if the channel name starts with it.
// If no patterns are configured, all channels are allowed.
func (s *SlackBot) IsChannelNameAllowed(channelName string) bool {
	if len(s.allowedChannelNames) == 0 {
		return true
	}
	for _, pattern := range s.allowedChannelNames {
		if strings.HasPrefix(channelName, pattern) {
			return true
		}
	}
	return false
}

// LongestMatchingChannelPatternLength returns the length of the longest channel name pattern
// that matches channelName by prefix (前方一致). Returns 0 if no pattern matches or if
// allowedChannelNames is empty. This is used for longest-match bot selection when multiple
// bots have patterns that match the same channel.
func (s *SlackBot) LongestMatchingChannelPatternLength(channelName string) int {
	maxLen := 0
	for _, pattern := range s.allowedChannelNames {
		if strings.HasPrefix(channelName, pattern) && len(pattern) > maxLen {
			maxLen = len(pattern)
		}
	}
	return maxLen
}

// BotToken returns the transient bot token value (write-only, never returned in API)
func (s *SlackBot) BotToken() string { return s.botToken }

// SetBotToken sets the transient bot token value
func (s *SlackBot) SetBotToken(token string) {
	s.botToken = token
	s.updatedAt = time.Now()
}

// AppToken returns the transient app token value (write-only, never returned in API)
func (s *SlackBot) AppToken() string { return s.appToken }

// SetAppToken sets the transient app token value
func (s *SlackBot) SetAppToken(token string) {
	s.appToken = token
	s.updatedAt = time.Now()
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
