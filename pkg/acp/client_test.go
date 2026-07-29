package acp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptResultPreservesTokenUsage(t *testing.T) {
	var result PromptResult
	err := json.Unmarshal([]byte(`{
		"stopReason": "end_turn",
		"usage": {
			"totalTokens": 5321,
			"inputTokens": 913,
			"cachedReadTokens": 4096,
			"outputTokens": 312,
			"thoughtTokens": 96
		},
		"_meta": {"quota": {"remaining": 42}}
	}`), &result)
	require.NoError(t, err)
	require.Equal(t, StopReasonEndTurn, result.StopReason)
	require.NotNil(t, result.Usage)
	require.EqualValues(t, 5321, result.Usage.TotalTokens)
	require.EqualValues(t, 913, result.Usage.InputTokens)
	require.EqualValues(t, 4096, *result.Usage.CachedReadTokens)
	require.EqualValues(t, 312, result.Usage.OutputTokens)
	require.EqualValues(t, 96, *result.Usage.ThoughtTokens)
	require.Contains(t, result.Meta, "quota")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"stopReason": "end_turn",
		"usage": {
			"totalTokens": 5321,
			"inputTokens": 913,
			"cachedReadTokens": 4096,
			"outputTokens": 312,
			"thoughtTokens": 96
		},
		"_meta": {"quota": {"remaining": 42}}
	}`, string(encoded))
}

func TestExtractModelFromConfigOptions(t *testing.T) {
	tests := []struct {
		name    string
		options []ConfigOption
		want    string
	}{
		{
			name: "string model default",
			options: []ConfigOption{
				{Key: "model", Default: "gpt-5.1-codex"},
			},
			want: "gpt-5.1-codex",
		},
		{
			name: "model id object default",
			options: []ConfigOption{
				{Key: "model", Default: map[string]interface{}{"id": "claude-sonnet-4.5"}},
			},
			want: "claude-sonnet-4.5",
		},
		{
			name: "description fallback",
			options: []ConfigOption{
				{Key: "provider_default", Description: "Current model", Default: "o4-mini"},
			},
			want: "o4-mini",
		},
		{
			name: "current value preferred over default",
			options: []ConfigOption{
				{Key: "model", CurrentValue: "gpt-5.1-codex", Default: "gpt-4.1"},
			},
			want: "gpt-5.1-codex",
		},
		{
			name: "v1 model category with id",
			options: []ConfigOption{
				{
					ID:           "model",
					Category:     "model",
					Type:         "select",
					CurrentValue: "gpt-5.1-codex",
					Options: []interface{}{
						map[string]interface{}{"value": "gpt-5.1-codex", "name": "GPT-5.1 Codex"},
					},
				},
			},
			want: "gpt-5.1-codex",
		},
		{
			name: "non model option ignored",
			options: []ConfigOption{
				{Key: "cwd", Default: "/workspace"},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractModelFromConfigOptions(tt.options); got != tt.want {
				t.Fatalf("ExtractModelFromConfigOptions() = %q, want %q", got, tt.want)
			}
		})
	}
}
