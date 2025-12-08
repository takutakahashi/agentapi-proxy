package proxy

import (
	"context"
	"sync"
	"time"
)

// kubernetesSession represents a session running in a Kubernetes Deployment
type kubernetesSession struct {
	id             string
	request        *RunServerRequest
	deploymentName string
	serviceName    string
	pvcName        string
	servicePort    int
	namespace      string
	startedAt      time.Time
	status         string
	cancelFunc     context.CancelFunc
	mutex          sync.RWMutex
}

// ID returns the session ID
func (s *kubernetesSession) ID() string {
	return s.id
}

// Port returns the port the session is running on
func (s *kubernetesSession) Port() int {
	return s.servicePort
}

// UserID returns the user ID that owns this session
func (s *kubernetesSession) UserID() string {
	return s.request.UserID
}

// Tags returns the session tags
func (s *kubernetesSession) Tags() map[string]string {
	return s.request.Tags
}

// Status returns the current status of the session
func (s *kubernetesSession) Status() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.status
}

// StartedAt returns when the session was started
func (s *kubernetesSession) StartedAt() time.Time {
	return s.startedAt
}

// Cancel cancels the session context to trigger shutdown
func (s *kubernetesSession) Cancel() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// setStatus updates the session status
func (s *kubernetesSession) setStatus(status string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.status = status
}

// ServiceDNS returns the Kubernetes Service DNS name for this session
func (s *kubernetesSession) ServiceDNS() string {
	return s.serviceName + "." + s.namespace + ".svc.cluster.local"
}
