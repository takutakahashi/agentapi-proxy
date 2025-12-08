package proxy

import (
	"testing"
	"time"
)

func TestSanitizeLabelFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		fn       func(string) string
	}{
		{
			name:     "sanitize key - normal",
			input:    "valid-key",
			expected: "valid-key",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize key - special chars",
			input:    "key/with/slashes",
			expected: "key-with-slashes",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize key - email",
			input:    "user@example.com",
			expected: "user-example.com",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize value - normal",
			input:    "valid-value",
			expected: "valid-value",
			fn:       sanitizeLabelValue,
		},
		{
			name:     "sanitize value - special chars",
			input:    "value/with/slashes",
			expected: "value-with-slashes",
			fn:       sanitizeLabelValue,
		},
		{
			name:     "sanitize value - long string",
			input:    "this-is-a-very-long-string-that-exceeds-the-kubernetes-label-value-limit-of-63-characters",
			expected: "this-is-a-very-long-string-that-exceeds-the-kubernetes-label-va",
			fn:       sanitizeLabelValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestKubernetesSession_Methods(t *testing.T) {
	// Test that kubernetesSession implements Session interface
	var _ Session = &kubernetesSession{}

	session := &kubernetesSession{
		id:          "test-session",
		serviceName: "test-svc",
		namespace:   "test-ns",
		servicePort: 9000,
		startedAt:   time.Now(),
		status:      "active",
		request: &RunServerRequest{
			UserID: "test-user",
			Tags:   map[string]string{"key": "value"},
		},
	}

	// Test ID
	if session.ID() != "test-session" {
		t.Errorf("Expected ID 'test-session', got %s", session.ID())
	}

	// Test Addr
	expectedAddr := "test-svc.test-ns.svc.cluster.local:9000"
	if session.Addr() != expectedAddr {
		t.Errorf("Expected Addr %s, got %s", expectedAddr, session.Addr())
	}

	// Test UserID
	if session.UserID() != "test-user" {
		t.Errorf("Expected UserID 'test-user', got %s", session.UserID())
	}

	// Test Tags
	if session.Tags()["key"] != "value" {
		t.Errorf("Expected tag 'key'='value', got %s", session.Tags()["key"])
	}

	// Test Status
	if session.Status() != "active" {
		t.Errorf("Expected status 'active', got %s", session.Status())
	}

	// Test setStatus
	session.setStatus("stopped")
	if session.Status() != "stopped" {
		t.Errorf("Expected status 'stopped', got %s", session.Status())
	}

	// Test ServiceDNS
	expectedDNS := "test-svc.test-ns.svc.cluster.local"
	if session.ServiceDNS() != expectedDNS {
		t.Errorf("Expected ServiceDNS %s, got %s", expectedDNS, session.ServiceDNS())
	}
}
