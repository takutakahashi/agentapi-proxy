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
func (s *SlackService) SendDM(slackUserID, title, body, url string) error {
	// Build blocks
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*%s*\n%s", title, body), false, false),
			nil,
			nil,
		),
	}

	if url != "" {
		blocks = append(blocks, slack.NewActionBlock(
			"",
			slack.NewButtonBlockElement(
				"open_url",
				url,
				slack.NewTextBlockObject("plain_text", "開く", false, false),
			).WithURL(url),
		))
	}

	_, _, err := s.client.PostMessage(
		slackUserID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(title+"\n"+body, false),
	)
	if err != nil {
		return fmt.Errorf("failed to send Slack DM: %w", err)
	}

	return nil
}
