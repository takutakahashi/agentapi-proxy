package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// Type aliases for backward compatibility
// These types are now defined in internal/interfaces/controllers

// NotificationHandlers is an alias to the internal implementation
type NotificationHandlers = controllers.NotificationHandlers

// NewNotificationHandlers creates new notification handlers
var NewNotificationHandlers = controllers.NewNotificationHandlers
