package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Type aliases for backward compatibility
// These types are now defined in internal/domain/entities

// Session represents a running agentapi session
type Session = entities.Session

// SessionFilter defines filter criteria for listing sessions
type SessionFilter = entities.SessionFilter

// SessionManager is an alias to the repositories.SessionManager interface
// for backward compatibility
type SessionManager = repositories.SessionManager
