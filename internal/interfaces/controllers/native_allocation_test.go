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

func TestContainsAllocatorSelector(t *testing.T) {
	if !containsAllocatorSelector(map[string]string{"allocator.os": "linux"}) {
		t.Fatal("allocator.* tag was not detected")
	}
	if containsAllocatorSelector(map[string]string{"repository": "owner/repo"}) {
		t.Fatal("ordinary session tag was treated as allocator selector")
	}
}

func TestRemoveImplicitAllocatorCapabilities(t *testing.T) {
	params := &entities.SessionParams{
		Sandbox: &entities.SandboxParams{Enabled: true},
		Docker:  &entities.DockerParams{Enabled: true},
	}
	removeImplicitAllocatorCapabilities(params, false, false)
	if params.Sandbox != nil || params.Docker != nil {
		t.Fatalf("profile capabilities were retained: %#v", params)
	}

	explicit := &entities.SessionParams{
		Sandbox: &entities.SandboxParams{Enabled: true},
		Docker:  &entities.DockerParams{Enabled: true},
	}
	removeImplicitAllocatorCapabilities(explicit, true, true)
	if explicit.Sandbox == nil || explicit.Docker == nil {
		t.Fatalf("explicit capabilities were removed: %#v", explicit)
	}
}
