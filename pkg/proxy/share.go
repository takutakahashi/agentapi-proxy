package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Type aliases for backward compatibility
// These types are now defined in internal/domain/entities

// SessionShare represents a shared session link
type SessionShare = entities.SessionShare

// NewSessionShare creates a new SessionShare
var NewSessionShare = entities.NewSessionShare

// NewSessionShareWithToken creates a SessionShare with a specific token (for loading from storage)
var NewSessionShareWithToken = entities.NewSessionShareWithToken

// ShareRepository defines the interface for share storage
type ShareRepository = repositories.ShareRepository
