package di

import (
	"testing"
)

func TestNewContainer(t *testing.T) {
	container := NewContainer()

	// Test repositories are initialized
	if container.SessionRepo == nil {
		t.Error("SessionRepo should not be nil")
	}

	if container.UserRepo == nil {
		t.Error("UserRepo should not be nil")
	}

	if container.NotificationRepo == nil {
		t.Error("NotificationRepo should not be nil")
	}

	// Test services are initialized
	if container.AgentService == nil {
		t.Error("AgentService should not be nil")
	}

	if container.AuthService == nil {
		t.Error("AuthService should not be nil")
	}

	if container.NotificationService == nil {
		t.Error("NotificationService should not be nil")
	}

	// Test use cases are initialized
	if container.CreateSessionUC == nil {
		t.Error("CreateSessionUC should not be nil")
	}

	if container.DeleteSessionUC == nil {
		t.Error("DeleteSessionUC should not be nil")
	}

	if container.AuthenticateUserUC == nil {
		t.Error("AuthenticateUserUC should not be nil")
	}

	// Test presenters are initialized
	if container.SessionPresenter == nil {
		t.Error("SessionPresenter should not be nil")
	}

	if container.AuthPresenter == nil {
		t.Error("AuthPresenter should not be nil")
	}

	// Test controllers are initialized
	if container.SessionController == nil {
		t.Error("SessionController should not be nil")
	}

	if container.AuthController == nil {
		t.Error("AuthController should not be nil")
	}

	if container.AuthMiddleware == nil {
		t.Error("AuthMiddleware should not be nil")
	}
}
