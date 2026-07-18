package controllers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionProfileResponseOmitsEnvironment(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{"SECRET": "sensitive-value"})
	cfg.SetTags(map[string]string{"agent": "codex"})
	profile.SetConfig(cfg)

	controller := NewSessionProfileController(nil)
	body, err := json.Marshal(controller.toResponse(profile))
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, json.Unmarshal(body, &response))
	responseConfig, ok := response["config"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, responseConfig, "environment")
	require.Equal(t, map[string]any{"agent": "codex"}, responseConfig["tags"])
}
