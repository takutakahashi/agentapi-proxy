package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestWebhookInfoDoesNotExposeSecret(t *testing.T) {
	webhook := entities.NewWebhook("webhook-1", "test webhook", "user-1", entities.WebhookTypeGitHub)
	webhook.SetSecret("super-secret-value")

	data, err := json.Marshal(toWebhookInfo(webhook))
	if err != nil {
		t.Fatalf("marshal webhook info: %v", err)
	}

	body := string(data)
	if strings.Contains(body, "super-secret-value") || strings.Contains(body, `"secret"`) {
		t.Fatalf("webhook info exposed secret: %s", body)
	}
}

func TestSlackBotInfoDoesNotExposeTokenValues(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "test bot", "user-1")
	bot.SetBotToken("xoxb-secret")
	bot.SetAppToken("xapp-secret")

	data, err := json.Marshal(toSlackBotInfo(bot))
	if err != nil {
		t.Fatalf("marshal slackbot info: %v", err)
	}

	body := string(data)
	if strings.Contains(body, "xoxb-secret") || strings.Contains(body, "xapp-secret") {
		t.Fatalf("slackbot info exposed token value: %s", body)
	}
}
