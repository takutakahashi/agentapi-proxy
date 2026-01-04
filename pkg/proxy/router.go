package proxy

import (
	"github.com/takutakahashi/agentapi-proxy/internal/app"
)

// HandlerRegistry is an alias to internal app.HandlerRegistry for backward compatibility
type HandlerRegistry = app.HandlerRegistry

// Note: Router and CustomHandler types are already defined as aliases in proxy.go
// Router = app.Router
// CustomHandler = app.CustomHandler

// The NewRouter function is also defined in proxy.go for backward compatibility
