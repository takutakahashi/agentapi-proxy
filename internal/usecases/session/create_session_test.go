package session

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"testing"
)

func TestCreateSessionUseCase_Execute(t *testing.T) {
	// Create DI container for testing
	container := di.NewContainer()

	// Create test user
	testUser := entities.NewUser(
		entities.UserID("test_user"),
		entities.UserTypeRegular,
		"testuser",
		nil,
		nil,
		nil,
	)

	// Save test user
	err := container.UserRepo.Save(context.Background(), testUser)
	if err != nil {
		t.Fatal("Failed to save test user:", err)
	}

	// Test creating a session
	req := &CreateSessionRequest{
		UserID:      testUser.ID(),
		Environment: entities.Environment{"TEST": "true"},
		Tags:        entities.Tags{"test": "session"},
	}

	response, err := container.CreateSessionUC.Execute(context.Background(), req)
	if err != nil {
		t.Fatal("Failed to create session:", err)
	}

	if response == nil {
		t.Fatal("Response should not be nil")
	}

	if response.Session == nil {
		t.Fatal("Session should not be nil")
	}

	if response.Session.UserID() != testUser.ID() {
		t.Errorf("Expected user ID %s, got %s", testUser.ID(), response.Session.UserID())
	}

	if response.URL == "" {
		t.Error("Session URL should not be empty")
	}
}
