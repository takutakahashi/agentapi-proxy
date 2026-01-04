package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Type aliases for backward compatibility
// These types are now defined in internal/domain/entities

// ResourceScope defines the scope of a resource (session, schedule, etc.)
type ResourceScope = entities.ResourceScope

const (
	// ScopeUser indicates the resource is owned by a specific user
	ScopeUser = entities.ScopeUser
	// ScopeTeam indicates the resource is owned by a team
	ScopeTeam = entities.ScopeTeam
)

// SessionParams represents session parameters for agentapi server
type SessionParams = entities.SessionParams

// StartRequest represents the request body for starting a new agentapi server
type StartRequest = entities.StartRequest

// RepositoryInfo contains repository information extracted from tags
type RepositoryInfo = entities.RepositoryInfo

// RunServerRequest contains parameters needed to run an agentapi server
type RunServerRequest = entities.RunServerRequest
