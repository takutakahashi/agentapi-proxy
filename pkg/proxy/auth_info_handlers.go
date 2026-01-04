package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// Type aliases for backward compatibility
// These types are now defined in internal/interfaces/controllers

// AuthTypesResponse is an alias to the internal implementation
type AuthTypesResponse = controllers.AuthTypesResponse

// AuthType is an alias to the internal implementation
type AuthType = controllers.AuthType

// AuthStatusResponse is an alias to the internal implementation
type AuthStatusResponse = controllers.AuthStatusResponse

// AuthInfoHandlers is an alias to the internal implementation
type AuthInfoHandlers = controllers.AuthInfoController

// NewAuthInfoHandlers creates a new AuthInfoController
var NewAuthInfoHandlers = controllers.NewAuthInfoController
