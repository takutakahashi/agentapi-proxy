package services

import (
	"context"
	"errors"
)

var (
	ErrNoPortAvailable = errors.New("no port available")
	ErrPortInUse       = errors.New("port is in use")
	ErrInvalidPort     = errors.New("invalid port")
)

// PortManager handles port allocation and management
type PortManager interface {
	// GetAvailablePort finds and reserves an available port
	GetAvailablePort(ctx context.Context) (int, error)

	// IsPortAvailable checks if a specific port is available
	IsPortAvailable(ctx context.Context, port int) (bool, error)

	// ReleasePort releases a previously allocated port
	ReleasePort(ctx context.Context, port int) error
}
