package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultHTTPClientConfig(t *testing.T) {
	config := DefaultHTTPClientConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout to be 30s, got %v", config.Timeout)
	}
}

func TestNewHTTPClient(t *testing.T) {
	config := HTTPClientConfig{
		Timeout: 15 * time.Second,
	}

	client := NewHTTPClient(config)

	if client.Timeout != 15*time.Second {
		t.Errorf("Expected client timeout to be 15s, got %v", client.Timeout)
	}
}

func TestNewDefaultHTTPClient(t *testing.T) {
	client := NewDefaultHTTPClient()

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected client timeout to be 30s, got %v", client.Timeout)
	}
}

func TestHTTPError(t *testing.T) {
	err := HTTPError{
		StatusCode: 404,
		Message:    "Not Found",
		URL:        "https://example.com/test",
	}

	expected := "HTTP 404: Not Found (URL: https://example.com/test)"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestCheckHTTPResponse(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		expectedError bool
	}{
		{"Success 200", 200, false},
		{"Success 201", 201, false},
		{"Success 302", 302, false},
		{"Client Error 400", 400, true},
		{"Client Error 404", 404, true},
		{"Server Error 500", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			// Make request
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			// Check response
			err = CheckHTTPResponse(resp, server.URL)

			if tt.expectedError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if tt.expectedError && err != nil {
				httpErr, ok := err.(HTTPError)
				if !ok {
					t.Errorf("Expected HTTPError, got %T", err)
				} else if httpErr.StatusCode != tt.statusCode {
					t.Errorf("Expected status code %d, got %d", tt.statusCode, httpErr.StatusCode)
				}
			}
		})
	}
}

func TestSafeCloseResponse(t *testing.T) {
	// Test with nil response
	SafeCloseResponse(nil)

	// Test with response that has nil body
	resp := &http.Response{}
	SafeCloseResponse(resp)

	// Test with actual response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("test"))
	}))
	defer server.Close()

	actualResp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	// This should not panic or cause issues
	SafeCloseResponse(actualResp)
}
