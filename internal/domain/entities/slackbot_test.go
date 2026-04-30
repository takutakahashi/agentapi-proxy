package entities

import (
	"strings"
	"testing"
	"time"
)

func TestNewSlackBot_Defaults(t *testing.T) {
	bot := NewSlackBot("id-1", "My Bot", "user-1")

	if bot.ID() != "id-1" {
		t.Errorf("ID() = %q, want %q", bot.ID(), "id-1")
	}
	if bot.Name() != "My Bot" {
		t.Errorf("Name() = %q, want %q", bot.Name(), "My Bot")
	}
	if bot.UserID() != "user-1" {
		t.Errorf("UserID() = %q, want %q", bot.UserID(), "user-1")
	}
	if bot.Status() != SlackBotStatusActive {
		t.Errorf("Status() = %q, want %q", bot.Status(), SlackBotStatusActive)
	}
	if bot.Scope() != ScopeUser {
		t.Errorf("Scope() = %q, want %q", bot.Scope(), ScopeUser)
	}
	if bot.MaxSessions() != 10 {
		t.Errorf("MaxSessions() = %d, want 10", bot.MaxSessions())
	}
	if bot.BotTokenSecretKey() != "bot-token" {
		t.Errorf("BotTokenSecretKey() = %q, want %q", bot.BotTokenSecretKey(), "bot-token")
	}
	if bot.CreatedAt().IsZero() {
		t.Error("CreatedAt() should not be zero")
	}
	if bot.UpdatedAt().IsZero() {
		t.Error("UpdatedAt() should not be zero")
	}
}

func TestSlackBot_Validate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*SlackBot)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid bot",
			modify:  func(b *SlackBot) {},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := NewSlackBot("id-1", "My Bot", "user-1")
			tt.modify(bot)
			err := bot.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSlackBot_Validate_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		botName string
		userID  string
		errMsg  string
	}{
		{
			name:    "missing id",
			id:      "",
			botName: "bot",
			userID:  "user",
			errMsg:  "id",
		},
		{
			name:    "missing name",
			id:      "id-1",
			botName: "",
			userID:  "user",
			errMsg:  "name",
		},
		{
			name:    "missing user_id",
			id:      "id-1",
			botName: "bot",
			userID:  "",
			errMsg:  "user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := &SlackBot{
				id:          tt.id,
				name:        tt.botName,
				userID:      tt.userID,
				maxSessions: 10,
				status:      SlackBotStatusActive,
				scope:       ScopeUser,
				createdAt:   time.Now(),
				updatedAt:   time.Now(),
			}
			err := bot.Validate()
			if err == nil {
				t.Errorf("Validate() expected error for missing %q, got nil", tt.errMsg)
				return
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSlackBot_IsEventTypeAllowed(t *testing.T) {
	tests := []struct {
		name              string
		allowedEventTypes []string
		eventType         string
		want              bool
	}{
		{
			name:              "all types allowed (empty list)",
			allowedEventTypes: []string{},
			eventType:         "message",
			want:              true,
		},
		{
			name:              "type in allowed list",
			allowedEventTypes: []string{"message", "app_mention"},
			eventType:         "message",
			want:              true,
		},
		{
			name:              "app_mention in allowed list",
			allowedEventTypes: []string{"message", "app_mention"},
			eventType:         "app_mention",
			want:              true,
		},
		{
			name:              "type not in allowed list",
			allowedEventTypes: []string{"message"},
			eventType:         "reaction_added",
			want:              false,
		},
		{
			name:              "nil allowed list (all types allowed)",
			allowedEventTypes: nil,
			eventType:         "any_type",
			want:              true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := NewSlackBot("id-1", "bot", "user-1")
			bot.SetAllowedEventTypes(tt.allowedEventTypes)
			got := bot.IsEventTypeAllowed(tt.eventType)
			if got != tt.want {
				t.Errorf("IsEventTypeAllowed(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestSlackBot_IsChannelNameAllowed(t *testing.T) {
	tests := []struct {
		name                string
		allowedChannelNames []string
		channelName         string
		want                bool
	}{
		{
			name:                "all channels allowed (empty list)",
			allowedChannelNames: []string{},
			channelName:         "general",
			want:                true,
		},
		{
			name:                "nil allowed list (all channels allowed)",
			allowedChannelNames: nil,
			channelName:         "general",
			want:                true,
		},
		{
			name:                "exact match",
			allowedChannelNames: []string{"dev", "prod"},
			channelName:         "dev",
			want:                true,
		},
		{
			name:                "partial match (prefix)",
			allowedChannelNames: []string{"dev"},
			channelName:         "dev-alerts",
			want:                true,
		},
		{
			name:                "partial match (suffix)",
			allowedChannelNames: []string{"alerts"},
			channelName:         "dev-alerts",
			want:                true,
		},
		{
			name:                "partial match (substring)",
			allowedChannelNames: []string{"back"},
			channelName:         "backend-team",
			want:                true,
		},
		{
			name:                "no match",
			allowedChannelNames: []string{"dev", "prod"},
			channelName:         "general",
			want:                false,
		},
		{
			name:                "second pattern matches",
			allowedChannelNames: []string{"frontend", "backend"},
			channelName:         "backend-alerts",
			want:                true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := NewSlackBot("id-1", "bot", "user-1")
			bot.SetAllowedChannelNames(tt.allowedChannelNames)
			got := bot.IsChannelNameAllowed(tt.channelName)
			if got != tt.want {
				t.Errorf("IsChannelNameAllowed(%q) = %v, want %v", tt.channelName, got, tt.want)
			}
		})
	}
}

func TestSlackBot_IsUserIDAllowed(t *testing.T) {
	tests := []struct {
		name           string
		allowedUserIDs []string
		userID         string
		want           bool
	}{
		{
			name:           "all users allowed (empty list)",
			allowedUserIDs: []string{},
			userID:         "U012AB3CD",
			want:           true,
		},
		{
			name:           "nil allowed list (all users allowed)",
			allowedUserIDs: nil,
			userID:         "U012AB3CD",
			want:           true,
		},
		{
			name:           "user in allowed list",
			allowedUserIDs: []string{"U012AB3CD", "U987XY654"},
			userID:         "U012AB3CD",
			want:           true,
		},
		{
			name:           "second user in allowed list",
			allowedUserIDs: []string{"U012AB3CD", "U987XY654"},
			userID:         "U987XY654",
			want:           true,
		},
		{
			name:           "user not in allowed list",
			allowedUserIDs: []string{"U012AB3CD"},
			userID:         "UNOTALLOWED",
			want:           false,
		},
		{
			name:           "partial match does not count (exact match required)",
			allowedUserIDs: []string{"U012AB"},
			userID:         "U012AB3CD",
			want:           false,
		},
		{
			name:           "empty user ID not in non-empty list",
			allowedUserIDs: []string{"U012AB3CD"},
			userID:         "",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := NewSlackBot("id-1", "bot", "user-1")
			bot.SetAllowedUserIDs(tt.allowedUserIDs)
			got := bot.IsUserIDAllowed(tt.userID)
			if got != tt.want {
				t.Errorf("IsUserIDAllowed(%q) = %v, want %v", tt.userID, got, tt.want)
			}
		})
	}
}

func TestSlackBot_BotTokenSecretKey_Default(t *testing.T) {
	bot := NewSlackBot("id-1", "bot", "user-1")
	// When not set, should return default "bot-token"
	if bot.BotTokenSecretKey() != "bot-token" {
		t.Errorf("BotTokenSecretKey() = %q, want %q", bot.BotTokenSecretKey(), "bot-token")
	}

	// After setting a custom key, should return that key
	bot.SetBotTokenSecretKey("my-key")
	if bot.BotTokenSecretKey() != "my-key" {
		t.Errorf("BotTokenSecretKey() = %q, want %q", bot.BotTokenSecretKey(), "my-key")
	}
}

func TestSlackBot_MaxSessions_Default(t *testing.T) {
	bot := NewSlackBot("id-1", "bot", "user-1")
	// Default should be 10
	if bot.MaxSessions() != 10 {
		t.Errorf("MaxSessions() = %d, want 10", bot.MaxSessions())
	}

	// Setting to 0 should still return default 10
	bot.SetMaxSessions(0)
	if bot.MaxSessions() != 10 {
		t.Errorf("MaxSessions() after SetMaxSessions(0) = %d, want 10", bot.MaxSessions())
	}

	// Setting a positive value should return that value
	bot.SetMaxSessions(5)
	if bot.MaxSessions() != 5 {
		t.Errorf("MaxSessions() after SetMaxSessions(5) = %d, want 5", bot.MaxSessions())
	}
}

func TestSlackBot_Scope_Default(t *testing.T) {
	bot := NewSlackBot("id-1", "bot", "user-1")
	// Default scope should be user
	if bot.Scope() != ScopeUser {
		t.Errorf("Scope() = %q, want %q", bot.Scope(), ScopeUser)
	}

	// Setting to team scope
	bot.SetScope(ScopeTeam)
	if bot.Scope() != ScopeTeam {
		t.Errorf("Scope() after SetScope(ScopeTeam) = %q, want %q", bot.Scope(), ScopeTeam)
	}
}

func TestErrSlackBotNotFound(t *testing.T) {
	err := ErrSlackBotNotFound{ID: "test-id"}
	if !strings.Contains(err.Error(), "test-id") {
		t.Errorf("ErrSlackBotNotFound.Error() = %q, want to contain 'test-id'", err.Error())
	}
}

func TestErrInvalidSlackBot(t *testing.T) {
	err := ErrInvalidSlackBot{Field: "name", Message: "name is required"}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("ErrInvalidSlackBot.Error() = %q, want to contain 'name'", err.Error())
	}
}
