package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Proxy represents the HTTP proxy server
type Proxy struct {
	config  *Config
	echo    *echo.Echo
	verbose bool
}

// NewProxy creates a new proxy instance
func NewProxy(config *Config, verbose bool) *Proxy {
	e := echo.New()
	
	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)
	
	// Add recovery middleware
	e.Use(middleware.Recover())
	
	p := &Proxy{
		config:  config,
		echo:    e,
		verbose: verbose,
	}

	// Add logging middleware if verbose
	if verbose {
		e.Use(p.loggingMiddleware())
	}

	p.setupRoutes()
	return p
}

// loggingMiddleware returns Echo middleware for request logging
func (p *Proxy) loggingMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Request: %s %s from %s", req.Method, req.URL.Path, req.RemoteAddr)
			return next(c)
		}
	})
}

// setupRoutes configures the router with all defined routes
func (p *Proxy) setupRoutes() {
	for pattern, backend := range p.config.Routes {
		p.addRoute(pattern, backend)
	}

	// Add default handler for unmatched routes - Echo will handle this automatically
	// if no routes match, but we can add a catch-all route
	p.echo.Any("/*", p.defaultHandler)
}

// addRoute adds a single route to the router
func (p *Proxy) addRoute(pattern, backend string) {
	// Convert gorilla/mux pattern {param} to Echo pattern :param
	echoPattern := strings.ReplaceAll(pattern, "{", ":")
	echoPattern = strings.ReplaceAll(echoPattern, "}", "")
	
	if p.verbose {
		log.Printf("Adding route: %s -> %s (Echo pattern: %s)", pattern, backend, echoPattern)
	}

	handler := p.createProxyHandler(backend)
	p.echo.Any(echoPattern, handler)
}

// createProxyHandler creates a reverse proxy handler for the given backend
func (p *Proxy) createProxyHandler(backendURL string) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Parse the backend URL
		target, err := url.Parse(backendURL)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid backend URL: %v", err))
		}

		// Get request and response from Echo context
		req := c.Request()
		w := c.Response()

		// Create reverse proxy
		proxy := httputil.NewSingleHostReverseProxy(target)
		
		// Customize the director to preserve the original path
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Preserve the original Host header from the Echo context
			originalHost := c.Request().Host
			if originalHost == "" {
				originalHost = c.Request().Header.Get("Host")
			}
			req.Header.Set("X-Forwarded-Host", originalHost)
			req.Header.Set("X-Forwarded-Proto", "http")
			if req.TLS != nil {
				req.Header.Set("X-Forwarded-Proto", "https")
			}
		}

		// Custom error handler
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("Proxy error for %s: %v", r.URL.Path, err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		}

		if p.verbose {
			log.Printf("Proxying %s %s to %s", req.Method, req.URL.Path, target)
		}

		proxy.ServeHTTP(w, req)
		return nil
	}
}

// defaultHandler handles requests that don't match any configured routes
func (p *Proxy) defaultHandler(c echo.Context) error {
	if p.config.DefaultBackend != "" {
		handler := p.createProxyHandler(p.config.DefaultBackend)
		return handler(c)
	}

	// No default backend configured, return 404
	req := c.Request()
	if p.verbose {
		log.Printf("No route found for %s %s", req.Method, req.URL.Path)
	}
	return echo.NewHTTPError(http.StatusNotFound, "Not Found")
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}