package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
)

func TestHealthCheck(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxyServer := proxy.NewProxy(cfg, true)
	defer func() {
		if err := proxyServer.Shutdown(5 * time.Second); err != nil {
			t.Logf("Failed to shutdown proxy: %v", err)
		}
	}()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxyServer.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	body := w.Body.String()
	expected := "{\"status\":\"ok\"}\n"
	if body != expected {
		t.Errorf("Expected body '%s' (len=%d), got '%s' (len=%d)", expected, len(expected), body, len(body))
	}
}

func TestNotificationEndpoints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxyServer := proxy.NewProxy(cfg, true)
	defer func() {
		if err := proxyServer.Shutdown(5 * time.Second); err != nil {
			t.Logf("Failed to shutdown proxy: %v", err)
		}
	}()

	// Test notification webhook endpoint exists
	req := httptest.NewRequest("POST", "/notifications/webhook", nil)
	w := httptest.NewRecorder()

	proxyServer.GetEcho().ServeHTTP(w, req)

	// Should not return 404 (not found), but might return other errors due to missing data
	if w.Code == http.StatusNotFound {
		t.Errorf("Notification webhook endpoint should exist")
	}
}

func TestCORSHeaders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxyServer := proxy.NewProxy(cfg, true)
	defer func() {
		if err := proxyServer.Shutdown(5 * time.Second); err != nil {
			t.Logf("Failed to shutdown proxy: %v", err)
		}
	}()

	// Test CORS for health endpoint
	req := httptest.NewRequest("OPTIONS", "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	proxyServer.GetEcho().ServeHTTP(w, req)

	// Check that CORS headers are set correctly
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS headers to be set")
	}
}