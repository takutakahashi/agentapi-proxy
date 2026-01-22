package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// KubernetesSession represents a session running in a Kubernetes Deployment
type KubernetesSession struct {
	id             string
	request        *entities.RunServerRequest
	deploymentName string
	serviceName    string
	pvcName        string
	servicePort    int
	namespace      string
	startedAt      time.Time
	updatedAt      time.Time
	status         string
	cancelFunc     context.CancelFunc
	mutex          sync.RWMutex
	description    string // Preserved description from Secret (not truncated by label limits)
}

// NewKubernetesSession creates a new KubernetesSession
func NewKubernetesSession(
	id string,
	request *entities.RunServerRequest,
	deploymentName, serviceName, pvcName, namespace string,
	servicePort int,
	cancelFunc context.CancelFunc,
) *KubernetesSession {
	now := time.Now()
	return &KubernetesSession{
		id:             id,
		request:        request,
		deploymentName: deploymentName,
		serviceName:    serviceName,
		pvcName:        pvcName,
		servicePort:    servicePort,
		namespace:      namespace,
		startedAt:      now,
		updatedAt:      now,
		status:         "creating",
		cancelFunc:     cancelFunc,
	}
}

// ID returns the session ID
func (s *KubernetesSession) ID() string {
	return s.id
}

// Addr returns the address (host:port) the session is running on
// For Kubernetes sessions, this returns the Service DNS name with port
func (s *KubernetesSession) Addr() string {
	return fmt.Sprintf("%s:%d", s.ServiceDNS(), s.servicePort)
}

// UserID returns the user ID that owns this session
func (s *KubernetesSession) UserID() string {
	return s.request.UserID
}

// Scope returns the resource scope ("user" or "team")
func (s *KubernetesSession) Scope() entities.ResourceScope {
	if s.request.Scope == "" {
		return entities.ScopeUser
	}
	return s.request.Scope
}

// TeamID returns the team ID when Scope is "team"
func (s *KubernetesSession) TeamID() string {
	return s.request.TeamID
}

// Tags returns the session tags
func (s *KubernetesSession) Tags() map[string]string {
	return s.request.Tags
}

// Status returns the current status of the session
func (s *KubernetesSession) Status() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.status
}

// StartedAt returns when the session was started
func (s *KubernetesSession) StartedAt() time.Time {
	return s.startedAt
}

// Description returns the session description (cached initial message)
func (s *KubernetesSession) Description() string {
	// Return cached description if available
	if s.description != "" {
		return s.description
	}
	// Fall back to InitialMessage
	if s.request != nil && s.request.InitialMessage != "" {
		return s.request.InitialMessage
	}
	return ""
}

// UpdatedAt returns when the session was last updated
func (s *KubernetesSession) UpdatedAt() time.Time {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.updatedAt
}

// Cancel cancels the session context to trigger shutdown
func (s *KubernetesSession) Cancel() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// SetStatus updates the session status
func (s *KubernetesSession) SetStatus(status string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.status = status
}

// SetStartedAt sets the session start time (used for restored sessions)
func (s *KubernetesSession) SetStartedAt(t time.Time) {
	s.startedAt = t
}

// SetDescription sets the session description (used for restored sessions from Secret)
func (s *KubernetesSession) SetDescription(desc string) {
	s.description = desc
}

// SetUpdatedAt sets the last updated time (used for restored sessions)
func (s *KubernetesSession) SetUpdatedAt(t time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updatedAt = t
}

// TouchUpdatedAt updates the updatedAt timestamp to now
func (s *KubernetesSession) TouchUpdatedAt() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updatedAt = time.Now()
}

// ServiceDNS returns the Kubernetes Service DNS name for this session
func (s *KubernetesSession) ServiceDNS() string {
	return s.serviceName + "." + s.namespace + ".svc.cluster.local"
}

// DeploymentName returns the Kubernetes Deployment name
func (s *KubernetesSession) DeploymentName() string {
	return s.deploymentName
}

// ServiceName returns the Kubernetes Service name
func (s *KubernetesSession) ServiceName() string {
	return s.serviceName
}

// PVCName returns the Kubernetes PVC name
func (s *KubernetesSession) PVCName() string {
	return s.pvcName
}

// Namespace returns the Kubernetes namespace
func (s *KubernetesSession) Namespace() string {
	return s.namespace
}

// ServicePort returns the service port
func (s *KubernetesSession) ServicePort() int {
	return s.servicePort
}

// Request returns the run server request
func (s *KubernetesSession) Request() *entities.RunServerRequest {
	return s.request
}

// Ensure KubernetesSession implements entities.Session
var _ entities.Session = (*KubernetesSession)(nil)
