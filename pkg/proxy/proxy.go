package proxy

import (
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// Proxy is an alias to internal app.Server for backward compatibility
type Proxy = app.Server

// CustomHandler is an alias to internal app.CustomHandler for backward compatibility
type CustomHandler = app.CustomHandler

// Router is an alias to internal app.Router for backward compatibility
type Router = app.Router

// NewProxy creates a new proxy (server) instance
func NewProxy(cfg *config.Config, verbose bool) *Proxy {
	return app.NewServer(cfg, verbose)
}

// ExtractRepositoryInfo extracts repository information from tags.
// This is a wrapper around the internal implementation for backward compatibility.
func ExtractRepositoryInfo(tags map[string]string, cloneDir string) *entities.RepositoryInfo {
	return app.ExtractRepositoryInfo(tags, cloneDir)
}

// The following types and functions are provided for backward compatibility
// with external code that may depend on them.

// OAuthSession is an alias to internal app.OAuthSession for backward compatibility
type OAuthSession = app.OAuthSession

// OAuthLoginRequest is an alias to internal app.OAuthLoginRequest for backward compatibility
type OAuthLoginRequest = app.OAuthLoginRequest

// OAuthLoginResponse is an alias to internal app.OAuthLoginResponse for backward compatibility
type OAuthLoginResponse = app.OAuthLoginResponse

// OAuthCallbackRequest is an alias to internal app.OAuthCallbackRequest for backward compatibility
type OAuthCallbackRequest = app.OAuthCallbackRequest

// OAuthTokenResponse is an alias to internal app.OAuthTokenResponse for backward compatibility
type OAuthTokenResponse = app.OAuthTokenResponse

// OAuthSessionResponse is an alias to internal app.OAuthSessionResponse for backward compatibility
type OAuthSessionResponse = app.OAuthSessionResponse

// NewRouter creates a new Router instance
// This is a wrapper around the internal implementation for backward compatibility.
func NewRouter(e *echo.Echo, proxy *Proxy) *Router {
	return app.NewRouter(e, proxy)
}
