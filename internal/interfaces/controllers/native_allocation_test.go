package controllers

import (
	"testing"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestAllocationMetadataRedactsProvisioningData(t *testing.T) {
	original := &sessionallocation.AllocationRequest{
		SessionID: "session-1",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Env: map[string]string{"SECRET": "value"},
		},
		Request: &entities.RunServerRequest{
			UserID:            "user-1",
			Scope:             entities.ScopeUser,
			AgentType:         "codex-acp",
			Tags:              map[string]string{"allocator.os": "linux"},
			Environment:       map[string]string{"SECRET": "value"},
			GithubToken:       "secret-token",
			InitialMessage:    "private prompt",
			ProvisionSettings: &sessionsettings.SessionSettings{},
		},
	}

	got := allocationMetadata(original)
	if got.ProvisionSettings != nil || got.Request.ProvisionSettings != nil {
		t.Fatal("provision settings were exposed to native allocator")
	}
	if got.Request.Environment != nil || got.Request.GithubToken != "" || got.Request.InitialMessage != "" {
		t.Fatalf("sensitive request data was exposed: %#v", got.Request)
	}
	if got.Request.UserID != "user-1" || got.Request.AgentType != "codex-acp" || got.Request.Tags["allocator.os"] != "linux" {
		t.Fatalf("required allocation metadata was not retained: %#v", got.Request)
	}
	if original.ProvisionSettings == nil || original.Request.GithubToken == "" {
		t.Fatal("redaction mutated the stored allocation")
	}
}
