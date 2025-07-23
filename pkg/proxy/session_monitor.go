package proxy

import (
	"context"
	"log"
	"sync"
	"time"
)

// SessionStatus represents the status of a session for monitoring
type SessionStatus struct {
	SessionID    string
	UserID       string
	Status       string
	ProcessAlive bool
	Tags         map[string]string
	LastChecked  time.Time
}

// SessionMonitor monitors session status changes and sends notifications
type SessionMonitor struct {
	proxy           *Proxy
	checkInterval   time.Duration
	sessionStatuses map[string]*SessionStatus
	statusMutex     sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewSessionMonitor creates a new session monitor
func NewSessionMonitor(proxy *Proxy, checkInterval time.Duration) *SessionMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionMonitor{
		proxy:           proxy,
		checkInterval:   checkInterval,
		sessionStatuses: make(map[string]*SessionStatus),
		statusMutex:     sync.RWMutex{},
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start begins monitoring session statuses
func (sm *SessionMonitor) Start() {
	log.Printf("Starting session status monitor with check interval: %v", sm.checkInterval)
	go sm.monitorLoop()
}

// Stop stops the session monitor
func (sm *SessionMonitor) Stop() {
	log.Printf("Stopping session status monitor")
	sm.cancel()
}

// monitorLoop is the main monitoring loop
func (sm *SessionMonitor) monitorLoop() {
	ticker := time.NewTicker(sm.checkInterval)
	defer ticker.Stop()

	// Initial check
	sm.checkSessions()

	for {
		select {
		case <-sm.ctx.Done():
			log.Printf("Session monitor stopped")
			return
		case <-ticker.C:
			sm.checkSessions()
		}
	}
}

// checkSessions checks all sessions for status changes
func (sm *SessionMonitor) checkSessions() {
	// Get current sessions from proxy
	currentSessions := sm.getCurrentSessions()

	// Map current sessions by ID for easy lookup
	currentSessionMap := make(map[string]*AgentSession)
	for _, session := range currentSessions {
		currentSessionMap[session.ID] = session
	}

	sm.statusMutex.Lock()
	defer sm.statusMutex.Unlock()

	// Check for new or changed sessions
	for _, session := range currentSessions {
		previousStatus, exists := sm.sessionStatuses[session.ID]

		// Check if process is alive
		processAlive := sm.isProcessAlive(session)

		if !exists {
			// New session detected
			sm.sessionStatuses[session.ID] = &SessionStatus{
				SessionID:    session.ID,
				UserID:       session.UserID,
				Status:       session.Status,
				ProcessAlive: processAlive,
				Tags:         session.Tags,
				LastChecked:  time.Now(),
			}
			log.Printf("Session monitor: New session detected %s (user: %s, status: %s)",
				session.ID, session.UserID, session.Status)
		} else {
			// Existing session - check for changes
			statusChanged := previousStatus.Status != session.Status
			processStateChanged := previousStatus.ProcessAlive != processAlive

			if statusChanged || processStateChanged {
				log.Printf("Session monitor: Status change detected for session %s (user: %s) - status: %s->%s, process: %v->%v",
					session.ID, session.UserID, previousStatus.Status, session.Status,
					previousStatus.ProcessAlive, processAlive)

				// Send notification for status change
				if processStateChanged && !processAlive && previousStatus.ProcessAlive {
					// Process died
					sm.sendProcessTerminatedNotification(session, previousStatus)
				}

				// Update stored status
				previousStatus.Status = session.Status
				previousStatus.ProcessAlive = processAlive
				previousStatus.LastChecked = time.Now()
			}
		}
	}

	// Check for removed sessions (completed or terminated)
	for sessionID, status := range sm.sessionStatuses {
		if _, exists := currentSessionMap[sessionID]; !exists {
			// Session was removed - it completed or was terminated
			log.Printf("Session monitor: Session removed %s (user: %s, was status: %s)",
				sessionID, status.UserID, status.Status)

			// Send completion notification
			sm.sendSessionCompletedNotification(status)

			// Remove from our tracking
			delete(sm.sessionStatuses, sessionID)
		}
	}
}

// getCurrentSessions gets the current active sessions from the proxy
func (sm *SessionMonitor) getCurrentSessions() []*AgentSession {
	sm.proxy.sessionsMutex.RLock()
	defer sm.proxy.sessionsMutex.RUnlock()

	sessions := make([]*AgentSession, 0, len(sm.proxy.sessions))
	for _, session := range sm.proxy.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// isProcessAlive checks if a session's process is still running
func (sm *SessionMonitor) isProcessAlive(session *AgentSession) bool {
	session.processMutex.RLock()
	defer session.processMutex.RUnlock()

	if session.Process == nil {
		return false
	}

	// Check if process is still running
	// Note: ProcessState is nil if the process hasn't exited yet
	return session.Process.ProcessState == nil
}

// sendProcessTerminatedNotification sends a notification when a process terminates unexpectedly
func (sm *SessionMonitor) sendProcessTerminatedNotification(session *AgentSession, previousStatus *SessionStatus) {
	if sm.proxy.notificationSvc == nil {
		return
	}

	title := "エージェントプロセスが予期せず終了しました"
	body := "セッション " + session.ID + " のプロセスが終了しました"
	notificationType := "session_update"
	data := map[string]interface{}{
		"session_id": session.ID,
		"event":      "process_terminated",
		"status":     "process_died",
	}

	if len(session.Tags) > 0 {
		data["tags"] = session.Tags
	}

	err := sm.proxy.notificationSvc.SendNotificationToUser(session.UserID, title, body, notificationType, data)
	if err != nil {
		log.Printf("Failed to send process terminated notification for session %s: %v", session.ID, err)
	}
}

// sendSessionCompletedNotification sends a notification when a session is completed
func (sm *SessionMonitor) sendSessionCompletedNotification(status *SessionStatus) {
	if sm.proxy.notificationSvc == nil {
		return
	}

	// Determine if this was a normal completion or termination
	title := "エージェントタスクが完了しました"
	body := "セッション " + status.SessionID + " のタスクが完了しました"
	eventType := "task_completed"
	statusType := "completed"

	// If the process wasn't alive when removed, it likely failed
	if !status.ProcessAlive {
		title = "エージェントタスクが終了しました"
		body = "セッション " + status.SessionID + " のタスクが終了しました"
		statusType = "terminated"
	}

	notificationType := "session_update"
	data := map[string]interface{}{
		"session_id": status.SessionID,
		"event":      eventType,
		"status":     statusType,
	}

	if len(status.Tags) > 0 {
		data["tags"] = status.Tags
	}

	err := sm.proxy.notificationSvc.SendNotificationToUser(status.UserID, title, body, notificationType, data)
	if err != nil {
		log.Printf("Failed to send session completed notification for session %s: %v", status.SessionID, err)
	}
}
