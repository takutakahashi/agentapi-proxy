package webhook

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionConfigToResponseIncludesCredentialSource(t *testing.T) {
	config := entities.NewWebhookSessionConfig()
	config.SetParams(&entities.SessionParams{
		CredentialSource: "triggered_user",
	})

	response := (&WebhookController{}).sessionConfigToResponse(config)

	if response.Params == nil {
		t.Fatal("response params must not be nil")
	}
	if got := response.Params.CredentialSource; got != "triggered_user" {
		t.Fatalf("credential source = %q, want triggered_user", got)
	}
}
