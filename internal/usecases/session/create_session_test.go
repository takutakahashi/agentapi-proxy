package session

import (
	"context"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra_services "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"testing"
)

// SimpleProxyService is a simple implementation of ProxyService for testing
type SimpleProxyService struct{}

func (s *SimpleProxyService) RouteRequest(ctx context.Context, sessionID entities.SessionID, request *services.HTTPRequest) (*services.HTTPResponse, error) {
	return &services.HTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte("Hello from session " + string(sessionID)),
	}, nil
}

func (s *SimpleProxyService) IsSessionReachable(ctx context.Context, sessionID entities.SessionID, port entities.Port) (bool, error) {
	return true, nil
}

func (s *SimpleProxyService) GetSessionURL(ctx context.Context, sessionID entities.SessionID, port entities.Port) (string, error) {
	return fmt.Sprintf("http://localhost:%d", port), nil
}

func TestCreateSessionUseCase_Execute(t *testing.T) {
	// Create repositories and services directly
	sessionRepo := repositories.NewMemorySessionRepository()
	userRepo := repositories.NewMemoryUserRepository()
	agentService := infra_services.NewLocalAgentService()
	
	// Create a simple proxy service for testing
	proxyService := &SimpleProxyService{}
	
	// Create use case
	createSessionUC := NewCreateSessionUseCase(sessionRepo, userRepo, agentService, proxyService)

	// Create test user
	testUser := entities.NewUser(
		entities.UserID("test_user"),
		entities.UserTypeRegular,
		"testuser",
	)

	// Save test user
	err := userRepo.Save(context.Background(), testUser)
	if err != nil {
		t.Fatal("Failed to save test user:", err)
	}

	// Test creating a session
	req := &CreateSessionRequest{
		UserID:      testUser.ID(),
		Environment: entities.Environment{"TEST": "true"},
		Tags:        entities.Tags{"test": "session"},
	}

	response, err := createSessionUC.Execute(context.Background(), req)
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
