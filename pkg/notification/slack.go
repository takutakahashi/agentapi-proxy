package notification

import (
	"fmt"
	"os"

	"github.com/slack-go/slack"
)

// SlackService handles sending Slack DM notifications
type SlackService struct {
	client *slack.Client
}

// NewSlackService creates a new Slack DM service using SLACK_BOT_TOKEN env var
func NewSlackService() (*SlackService, error) {
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN is required")
	}
	return &SlackService{
		client: slack.New(token),
	}, nil
}

// SendDM sends a DM to the specified Slack user ID
// initialMessage is an optional initial message to display as a linked quote
func (s *SlackService) SendDM(slackUserID, title, body, url, initialMessage string) error {
	// Open a DM channel with the user first.
	// This is required because PostMessage with a user ID may return channel_not_found
	// if the bot has not previously interacted with the user.
	channel, _, _, err := s.client.OpenConversation(&slack.OpenConversationParameters{
		Users: []string{slackUserID},
	})
	if err != nil {
		return fmt.Errorf("failed to open DM channel with user %s: %w", slackUserID, err)
	}

	// Build content text
	contentText := fmt.Sprintf("*%s*\n%s", title, body)

	// Build blocks
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", contentText, false, false),
			nil,
			nil,
		),
	}

	if url != "" {
		// Use the initial message as button label so the user can tell which
		// task/conversation triggered this notification.
		// Fall back to "開く" if no initial message is provided.
		buttonLabel := "開く"
		if initialMessage != "" {
			truncated := initialMessage
			runes := []rune(truncated)
			if len(runes) > 40 {
				truncated = string(runes[:40]) + "..."
			}
			buttonLabel = truncated
		}
		blocks = append(blocks, slack.NewActionBlock(
			"",
			slack.NewButtonBlockElement(
				"open_url",
				url,
				slack.NewTextBlockObject("plain_text", buttonLabel, false, false),
			).WithURL(url),
		))
	}

	_, _, err = s.client.PostMessage(
		channel.ID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(title+"\n"+body, false),
	)
	if err != nil {
		return fmt.Errorf("failed to send Slack DM: %w", err)
	}

	return nil
}
