package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewProxy(t *testing.T) {
	config := &Config{
		DefaultBackend: "http://localhost:3000",
		Routes: map[string]string{
			"/api/{org}/{repo}": "http://localhost:3001",
		},
	}

	proxy := NewProxy(config, false)
	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	if proxy.config != config {
		t.Error("Proxy config not set correctly")
	}

	if proxy.echo == nil {
		t.Error("Echo instance not initialized")
	}
}

func TestProxyRouting(t *testing.T) {
	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	config := &Config{
		DefaultBackend: backend.URL,
		Routes: map[string]string{
			"/api/{org}/{repo}": backend.URL,
			"/health":           backend.URL,
		},
	}

	proxy := NewProxy(config, true)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "API route with org and repo",
			path:           "/api/myorg/myrepo",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Health endpoint",
			path:           "/health",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Unmatched route should use default backend",
			path:           "/some/other/path",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			proxy.GetEcho().ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestProxyWithoutDefaultBackend(t *testing.T) {
	config := &Config{
		Routes: map[string]string{
			"/health": "http://localhost:3001",
		},
	}

	proxy := NewProxy(config, false)

	// Test unmatched route without default backend
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestProxyErrorHandling(t *testing.T) {
	config := &Config{
		Routes: map[string]string{
			"/api/{org}/{repo}": "http://invalid-backend:99999",
		},
	}

	proxy := NewProxy(config, false)

	req := httptest.NewRequest("GET", "/api/test/repo", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d for invalid backend, got %d", http.StatusBadGateway, w.Code)
	}
}