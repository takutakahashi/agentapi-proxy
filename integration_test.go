package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
)

func TestIntegrationFullProxy(t *testing.T) {
	// Create multiple backend servers to simulate different services
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "backend1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Response from backend 1"))
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "backend2")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Response from backend 2"))
	}))
	defer backend2.Close()

	cfg := &config.Config{
		DefaultBackend: backend1.URL,
		Routes: map[string]string{
			"/api/{org}/{repo}":        backend1.URL,
			"/api/{org}/{repo}/issues": backend2.URL,
			"/health":                  backend1.URL,
		},
	}

	proxyServer := proxy.NewProxy(cfg, true)

	tests := []struct {
		name             string
		method           string
		path             string
		expectedStatus   int
		expectedBackend  string
		expectedResponse string
	}{
		{
			name:             "API route to backend 1",
			method:           "GET",
			path:             "/api/myorg/myrepo",
			expectedStatus:   http.StatusOK,
			expectedBackend:  "backend1",
			expectedResponse: "Response from backend 1",
		},
		{
			name:             "Issues route to backend 2",
			method:           "GET",
			path:             "/api/myorg/myrepo/issues",
			expectedStatus:   http.StatusOK,
			expectedBackend:  "backend2",
			expectedResponse: "Response from backend 2",
		},
		{
			name:             "Health check",
			method:           "GET",
			path:             "/health",
			expectedStatus:   http.StatusOK,
			expectedBackend:  "backend1",
			expectedResponse: "Response from backend 1",
		},
		{
			name:             "Default route",
			method:           "GET",
			path:             "/some/unknown/path",
			expectedStatus:   http.StatusOK,
			expectedBackend:  "backend1",
			expectedResponse: "Response from backend 1",
		},
		{
			name:           "POST request",
			method:         "POST",
			path:           "/api/test/repo",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			proxyServer.GetEcho().ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedBackend != "" {
				backend := w.Header().Get("X-Backend")
				if backend != tt.expectedBackend {
					t.Errorf("Expected backend %s, got %s", tt.expectedBackend, backend)
				}
			}

			if tt.expectedResponse != "" {
				body := w.Body.String()
				if !strings.Contains(body, tt.expectedResponse) {
					t.Errorf("Expected response to contain %s, got %s", tt.expectedResponse, body)
				}
			}
		})
	}
}

func TestProxyHeaders(t *testing.T) {
	// Backend that returns request headers for inspection
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the forwarded headers
		w.Header().Set("X-Forwarded-Host-Echo", r.Header.Get("X-Forwarded-Host"))
		w.Header().Set("X-Forwarded-Proto-Echo", r.Header.Get("X-Forwarded-Proto"))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		Routes: map[string]string{
			"/test": backend.URL,
		},
	}

	proxyServer := proxy.NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "example.com"
	w := httptest.NewRecorder()

	proxyServer.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check if forwarded headers are set correctly
	forwardedHost := w.Header().Get("X-Forwarded-Host-Echo")
	if forwardedHost != "example.com" {
		t.Errorf("Expected X-Forwarded-Host to be 'example.com', got '%s'", forwardedHost)
	}

	forwardedProto := w.Header().Get("X-Forwarded-Proto-Echo")
	if forwardedProto != "http" {
		t.Errorf("Expected X-Forwarded-Proto to be 'http', got '%s'", forwardedProto)
	}
}

func TestConcurrentRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	cfg := &config.Config{
		Routes: map[string]string{
			"/api/{org}/{repo}": backend.URL,
		},
	}

	proxyServer := proxy.NewProxy(cfg, false)

	// Test concurrent requests
	const numRequests = 10
	results := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := httptest.NewRequest("GET", "/api/org/repo", nil)
			w := httptest.NewRecorder()
			proxyServer.GetEcho().ServeHTTP(w, req)
			results <- w.Code
		}(i)
	}

	// Collect results
	for i := 0; i < numRequests; i++ {
		select {
		case status := <-results:
			if status != http.StatusOK {
				t.Errorf("Request %d failed with status %d", i, status)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Request %d timed out", i)
		}
	}
}
