package configrender

import (
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// RenderSessionParams renders all template fields in session params.
func RenderSessionParams(sessionConfig *entities.WebhookSessionConfig, payload map[string]interface{}) (*entities.SessionParams, error) {
	if sessionConfig == nil || sessionConfig.Params() == nil {
		return nil, nil
	}

	params := sessionConfig.Params()
	result := &entities.SessionParams{
		Oneshot: params.Oneshot,
	}

	fields := []struct {
		src  string
		dest *string
		name string
	}{
		{params.Message, &result.Message, "params.message"},
		{params.GithubToken, &result.GithubToken, "params.github_token"},
		{params.AgentType, &result.AgentType, "params.agent_type"},
	}

	for _, f := range fields {
		if f.src == "" {
			continue
		}
		rendered, err := RenderTemplate(f.src, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to render template for %s: %w", f.name, err)
		}
		*f.dest = rendered
	}

	return result, nil
}
