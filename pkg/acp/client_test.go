package acp

import "testing"

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
