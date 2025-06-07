package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// Proxy represents the HTTP proxy server
type Proxy struct {
	config  *Config
	router  *mux.Router
	verbose bool
}

// NewProxy creates a new proxy instance
func NewProxy(config *Config, verbose bool) *Proxy {
	p := &Proxy{
		config:  config,
		router:  mux.NewRouter(),
		verbose: verbose,
	}

	p.setupRoutes()
	return p
}

// ServeHTTP implements the http.Handler interface
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.verbose {
		log.Printf("Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	}

	p.router.ServeHTTP(w, r)
}

// setupRoutes configures the router with all defined routes
func (p *Proxy) setupRoutes() {
	for pattern, backend := range p.config.Routes {
		p.addRoute(pattern, backend)
	}

	// Add default handler for unmatched routes
	p.router.PathPrefix("/").HandlerFunc(p.defaultHandler)
}

// addRoute adds a single route to the router
func (p *Proxy) addRoute(pattern, backend string) {
	if p.verbose {
		log.Printf("Adding route: %s -> %s", pattern, backend)
	}

	handler := p.createProxyHandler(backend)
	p.router.Handle(pattern, handler)
}

// createProxyHandler creates a reverse proxy handler for the given backend
func (p *Proxy) createProxyHandler(backendURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse the backend URL
		target, err := url.Parse(backendURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid backend URL: %v", err), http.StatusInternalServerError)
			return
		}

		// Create reverse proxy
		proxy := httputil.NewSingleHostReverseProxy(target)
		
		// Customize the director to preserve the original path
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
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
			log.Printf("Proxying %s %s to %s", r.Method, r.URL.Path, target)
		}

		proxy.ServeHTTP(w, r)
	}
}

// defaultHandler handles requests that don't match any configured routes
func (p *Proxy) defaultHandler(w http.ResponseWriter, r *http.Request) {
	if p.config.DefaultBackend != "" {
		handler := p.createProxyHandler(p.config.DefaultBackend)
		handler(w, r)
		return
	}

	// No default backend configured, return 404
	if p.verbose {
		log.Printf("No route found for %s %s", r.Method, r.URL.Path)
	}
	http.NotFound(w, r)
}