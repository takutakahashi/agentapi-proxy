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
	// Get current session snapshots from proxy
	currentSnapshots := sm.getCurrentSessionSnapshots()

	sm.statusMutex.Lock()
	defer sm.statusMutex.Unlock()

	// Check for new or changed sessions
	for sessionID, snapshot := range currentSnapshots {
		previousStatus, exists := sm.sessionStatuses[sessionID]

		if !exists {
			// New session detected
			sm.sessionStatuses[sessionID] = &SessionStatus{
				SessionID:    snapshot.ID,
				UserID:       snapshot.UserID,
				Status:       snapshot.Status,
				ProcessAlive: snapshot.ProcessAlive,
				Tags:         snapshot.Tags,
				LastChecked:  time.Now(),
			}
			log.Printf("Session monitor: New session detected %s (user: %s, status: %s)",
				snapshot.ID, snapshot.UserID, snapshot.Status)
		} else {
			// Existing session - check for changes
			statusChanged := previousStatus.Status != snapshot.Status
			processStateChanged := previousStatus.ProcessAlive != snapshot.ProcessAlive

			if statusChanged || processStateChanged {
				log.Printf("Session monitor: Status change detected for session %s (user: %s) - status: %s->%s, process: %v->%v",
					snapshot.ID, snapshot.UserID, previousStatus.Status, snapshot.Status,
					previousStatus.ProcessAlive, snapshot.ProcessAlive)

				// Send notification for status change
				if processStateChanged && !snapshot.ProcessAlive && previousStatus.ProcessAlive {
					// Process died
					sm.sendProcessTerminatedNotificationFromSnapshot(snapshot, previousStatus)
				}

				// Update stored status
				previousStatus.Status = snapshot.Status
				previousStatus.ProcessAlive = snapshot.ProcessAlive
				previousStatus.Tags = snapshot.Tags
				previousStatus.LastChecked = time.Now()
			}
		}
	}

	// Check for removed sessions (completed or terminated)
	for sessionID, status := range sm.sessionStatuses {
		if _, exists := currentSnapshots[sessionID]; !exists {
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

// SessionSnapshot represents a snapshot of session data for monitoring
type SessionSnapshot struct {
	ID           string
	UserID       string
	Status       string
	ProcessAlive bool
	Tags         map[string]string
}

// getCurrentSessionSnapshots gets snapshots of current active sessions from the proxy
func (sm *SessionMonitor) getCurrentSessionSnapshots() map[string]*SessionSnapshot {
	sm.proxy.sessionsMutex.RLock()
	defer sm.proxy.sessionsMutex.RUnlock()

	snapshots := make(map[string]*SessionSnapshot)
	for _, session := range sm.proxy.sessions {
		// Create a snapshot with only necessary data
		snapshot := &SessionSnapshot{
			ID:     session.ID,
			UserID: session.UserID,
			Status: session.Status,
			Tags:   make(map[string]string),
		}

		// Safely copy tags
		for k, v := range session.Tags {
			snapshot.Tags[k] = v
		}

		// Check process status with proper locking
		session.processMutex.RLock()
		snapshot.ProcessAlive = session.Process != nil && session.Process.ProcessState == nil
		session.processMutex.RUnlock()

		snapshots[session.ID] = snapshot
	}
	return snapshots
}

// sendProcessTerminatedNotificationFromSnapshot sends a notification when a process terminates unexpectedly
func (sm *SessionMonitor) sendProcessTerminatedNotificationFromSnapshot(snapshot *SessionSnapshot, previousStatus *SessionStatus) {
	if sm.proxy.notificationSvc == nil {
		return
	}

	title := "エージェントプロセスが予期せず終了しました"
	body := "セッション " + snapshot.ID + " のプロセスが終了しました"
	notificationType := "session_update"
	data := map[string]interface{}{
		"session_id": snapshot.ID,
		"event":      "process_terminated",
		"status":     "process_died",
	}

	if len(snapshot.Tags) > 0 {
		data["tags"] = snapshot.Tags
	}

	err := sm.proxy.notificationSvc.SendNotificationToUser(snapshot.UserID, title, body, notificationType, data)
	if err != nil {
		log.Printf("Failed to send process terminated notification for session %s: %v", snapshot.ID, err)
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
