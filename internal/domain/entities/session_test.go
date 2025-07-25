package entities

import (
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	sessionID := SessionID("test-session")
	userID := UserID("test-user")
	port := Port(8080)
	env := Environment{"KEY": "value"}
	tags := Tags{"tag": "value"}
	repo, _ := NewRepository(RepositoryURL("https://github.com/owner/repo.git"))

	session := NewSession(sessionID, userID, port, env, tags, repo)

	if session.ID() != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID())
	}
	if session.UserID() != userID {
		t.Errorf("Expected user ID %s, got %s", userID, session.UserID())
	}
	if session.Port() != port {
		t.Errorf("Expected port %d, got %d", port, session.Port())
	}
	if session.Status() != SessionStatusStarting {
		t.Errorf("Expected status %s, got %s", SessionStatusStarting, session.Status())
	}
	if session.Repository() != repo {
		t.Error("Expected repository to match")
	}
}

func TestSession_Start(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	err := session.Start(processInfo)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if session.Status() != SessionStatusActive {
		t.Errorf("Expected status %s, got %s", SessionStatusActive, session.Status())
	}
	if session.ProcessInfo() != processInfo {
		t.Error("Expected process info to match")
	}
}

func TestSession_StartWithoutProcessInfo(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)

	err := session.Start(nil)
	if err == nil {
		t.Error("Expected error when starting without process info")
	}
}

func TestSession_StartFromWrongStatus(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	// Start the session first
	_ = session.Start(processInfo)

	// Try to start again
	err := session.Start(processInfo)
	if err == nil {
		t.Error("Expected error when starting from active status")
	}
}

func TestSession_Terminate(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	_ = session.Start(processInfo)

	err := session.Terminate()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if session.Status() != SessionStatusTerminating {
		t.Errorf("Expected status %s, got %s", SessionStatusTerminating, session.Status())
	}
}

func TestSession_TerminateFromWrongStatus(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)

	err := session.Terminate()
	if err == nil {
		t.Error("Expected error when terminating from starting status")
	}
}

func TestSession_MarkStopped(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	_ = session.Start(processInfo)
	_ = session.Terminate()

	err := session.MarkStopped()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if session.Status() != SessionStatusStopped {
		t.Errorf("Expected status %s, got %s", SessionStatusStopped, session.Status())
	}
	if session.ProcessInfo() != nil {
		t.Error("Expected process info to be nil after stopping")
	}
}

func TestSession_MarkFailed(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)

	session.MarkFailed("test error")

	if session.Status() != SessionStatusFailed {
		t.Errorf("Expected status %s, got %s", SessionStatusFailed, session.Status())
	}
}

func TestSession_IsActive(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	if session.IsActive() {
		t.Error("Expected session not to be active initially")
	}

	_ = session.Start(processInfo)
	if !session.IsActive() {
		t.Error("Expected session to be active after starting")
	}

	_ = session.Terminate()
	if session.IsActive() {
		t.Error("Expected session not to be active after terminating")
	}
}

func TestSession_IsHealthy(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	if session.IsHealthy() {
		t.Error("Expected session not to be healthy initially")
	}

	_ = session.Start(processInfo)
	if !session.IsHealthy() {
		t.Error("Expected session to be healthy after starting")
	}
}

func TestSession_CanBeTerminated(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})

	if session.CanBeTerminated() {
		t.Error("Expected session not to be terminable initially")
	}

	_ = session.Start(processInfo)
	if !session.CanBeTerminated() {
		t.Error("Expected session to be terminable when active")
	}

	session.MarkFailed("test error")
	if !session.CanBeTerminated() {
		t.Error("Expected session to be terminable when failed")
	}
}

func TestSession_UpdateEnvironment(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	newEnv := Environment{"NEW_KEY": "new_value"}

	err := session.UpdateEnvironment(newEnv)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	env := session.Environment()
	if env["NEW_KEY"] != "new_value" {
		t.Error("Expected environment to be updated")
	}
}

func TestSession_UpdateEnvironmentWhenActive(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	processInfo := NewProcessInfo(1234, time.Now())
	processInfo.SetCommand("test", []string{})
	_ = session.Start(processInfo)

	newEnv := Environment{"NEW_KEY": "new_value"}
	err := session.UpdateEnvironment(newEnv)
	if err == nil {
		t.Error("Expected error when updating environment of active session")
	}
}

func TestSession_UpdateTags(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)
	newTags := Tags{"new_tag": "new_value"}

	session.UpdateTags(newTags)

	tags := session.Tags()
	if tags["new_tag"] != "new_value" {
		t.Error("Expected tags to be updated")
	}
}

func TestSession_Duration(t *testing.T) {
	session := NewSession("test", "user", 8080, nil, nil, nil)

	time.Sleep(10 * time.Millisecond)
	duration := session.Duration()

	if duration < 10*time.Millisecond {
		t.Error("Expected duration to be at least 10ms")
	}
}

func TestSession_Validate(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		wantErr bool
	}{
		{
			name:    "valid session",
			session: NewSession("test", "user", 8080, nil, nil, nil),
			wantErr: false,
		},
		{
			name:    "empty session ID",
			session: NewSession("", "user", 8080, nil, nil, nil),
			wantErr: true,
		},
		{
			name:    "empty user ID",
			session: NewSession("test", "", 8080, nil, nil, nil),
			wantErr: true,
		},
		{
			name:    "invalid port (zero)",
			session: NewSession("test", "user", 0, nil, nil, nil),
			wantErr: true,
		},
		{
			name:    "invalid port (too high)",
			session: NewSession("test", "user", 70000, nil, nil, nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Session.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSession_EnvironmentImmutability(t *testing.T) {
	originalEnv := Environment{"KEY": "value"}
	session := NewSession("test", "user", 8080, originalEnv, nil, nil)

	// Get environment and modify it
	env := session.Environment()
	env["NEW_KEY"] = "new_value"

	// Original environment should not be modified
	sessionEnv := session.Environment()
	if _, exists := sessionEnv["NEW_KEY"]; exists {
		t.Error("Expected session environment to be immutable")
	}
}

func TestSession_TagsImmutability(t *testing.T) {
	originalTags := Tags{"tag": "value"}
	session := NewSession("test", "user", 8080, nil, originalTags, nil)

	// Get tags and modify them
	tags := session.Tags()
	tags["new_tag"] = "new_value"

	// Original tags should not be modified
	sessionTags := session.Tags()
	if _, exists := sessionTags["new_tag"]; exists {
		t.Error("Expected session tags to be immutable")
	}
}
