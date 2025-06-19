package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type SessionLog struct {
	SessionID     string    `json:"session_id"`
	StartedAt     time.Time `json:"started_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	Repository    string    `json:"repository"`
	MessageCount  int       `json:"message_count"`
}

type Logger struct {
	logDir string
}

func NewLogger() *Logger {
	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = "./logs"
	}
	
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("Failed to create log directory %s: %v", logDir, err)
	}
	
	return &Logger{
		logDir: logDir,
	}
}

func (l *Logger) LogSessionStart(sessionID string, repository string) error {
	sessionLog := SessionLog{
		SessionID:    sessionID,
		StartedAt:    time.Now(),
		Repository:   repository,
		MessageCount: 0,
	}
	
	return l.writeSessionLog(sessionLog)
}

func (l *Logger) LogSessionEnd(sessionID string, messageCount int) error {
	sessionLog, err := l.readSessionLog(sessionID)
	if err != nil {
		// If we can't read the existing log, create a new one
		now := time.Now()
		sessionLog = SessionLog{
			SessionID:    sessionID,
			StartedAt:    now,
			DeletedAt:    &now,
			Repository:   "",
			MessageCount: messageCount,
		}
	} else {
		now := time.Now()
		sessionLog.DeletedAt = &now
		sessionLog.MessageCount = messageCount
	}
	
	return l.writeSessionLog(sessionLog)
}

func (l *Logger) readSessionLog(sessionID string) (SessionLog, error) {
	var sessionLog SessionLog
	
	filePath := filepath.Join(l.logDir, fmt.Sprintf("%s.json", sessionID))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return sessionLog, err
	}
	
	err = json.Unmarshal(data, &sessionLog)
	return sessionLog, err
}

func (l *Logger) writeSessionLog(sessionLog SessionLog) error {
	filePath := filepath.Join(l.logDir, fmt.Sprintf("%s.json", sessionLog.SessionID))
	
	data, err := json.MarshalIndent(sessionLog, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session log: %v", err)
	}
	
	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write session log to %s: %v", filePath, err)
	}
	
	log.Printf("Session log written: %s", filePath)
	return nil
}