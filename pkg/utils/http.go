package utils

import (
	"fmt"
	"net/http"
	"time"
)

// HTTPClientConfig holds configuration for HTTP client creation
type HTTPClientConfig struct {
	Timeout time.Duration
}

// DefaultHTTPClientConfig returns default HTTP client configuration
func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout: 30 * time.Second,
	}
}

// NewHTTPClient creates a new HTTP client with the given configuration
func NewHTTPClient(config HTTPClientConfig) *http.Client {
	return &http.Client{
		Timeout: config.Timeout,
	}
}

// NewDefaultHTTPClient creates a new HTTP client with default configuration
func NewDefaultHTTPClient() *http.Client {
	return NewHTTPClient(DefaultHTTPClientConfig())
}

// HTTPError represents an HTTP error with status code and message
type HTTPError struct {
	StatusCode int
	Message    string
	URL        string
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s (URL: %s)", e.StatusCode, e.Message, e.URL)
}

// CheckHTTPResponse checks if HTTP response indicates an error
func CheckHTTPResponse(resp *http.Response, url string) error {
	if resp.StatusCode >= 400 {
		return HTTPError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
			URL:        url,
		}
	}
	return nil
}

// SafeCloseResponse safely closes HTTP response body with error logging
func SafeCloseResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		if err := resp.Body.Close(); err != nil {
			// Note: Using fmt.Printf since we're creating a utility package
			// In a real application, this should use a structured logger
			fmt.Printf("Warning: failed to close HTTP response body: %v\n", err)
		}
	}
}
