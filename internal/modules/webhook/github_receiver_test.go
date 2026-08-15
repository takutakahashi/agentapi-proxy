package webhook

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestBuildGitHubTagsIncludesSenderCredentialSource(t *testing.T) {
	webhook := entities.NewWebhook("webhook-1", "GitHub", "owner", entities.WebhookTypeGitHub)
	trigger := entities.NewWebhookTrigger("trigger-1", "Pull request")

	tags := buildGitHubTags(webhook, &trigger, "pull_request", &GitHubPayload{
		Sender: &GitHubUser{Login: " octocat "},
	})

	if got := tags["github_sender"]; got != "octocat" {
		t.Fatalf("github_sender = %q, want octocat", got)
	}
	if got := tags["credential_source"]; got != "github_sender" {
		t.Fatalf("credential_source = %q, want github_sender", got)
	}
}
