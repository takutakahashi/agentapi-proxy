package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeEnvIntoScheduleJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		envMap  map[string]string
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:   "empty envMap returns unchanged data",
			input:  `{"name":"test"}`,
			envMap: map[string]string{},
			want: map[string]interface{}{
				"name": "test",
			},
		},
		{
			name:  "nil envMap returns unchanged data",
			input: `{"name":"test"}`,
			want: map[string]interface{}{
				"name": "test",
			},
		},
		{
			name:  "adds session_config.environment when absent",
			input: `{"name":"test","cron_expr":"0 9 * * 1-5"}`,
			envMap: map[string]string{
				"MY_VAR": "hello",
				"DEBUG":  "true",
			},
			want: map[string]interface{}{
				"name":      "test",
				"cron_expr": "0 9 * * 1-5",
				"session_config": map[string]interface{}{
					"environment": map[string]interface{}{
						"MY_VAR": "hello",
						"DEBUG":  "true",
					},
				},
			},
		},
		{
			name: "merges into existing session_config.environment",
			input: `{
				"name": "test",
				"session_config": {
					"environment": {"EXISTING": "val", "MY_VAR": "old"}
				}
			}`,
			envMap: map[string]string{
				"MY_VAR": "new",
				"ADDED":  "yes",
			},
			want: map[string]interface{}{
				"name": "test",
				"session_config": map[string]interface{}{
					"environment": map[string]interface{}{
						"EXISTING": "val",
						"MY_VAR":   "new", // overridden by CLI flag
						"ADDED":    "yes",
					},
				},
			},
		},
		{
			name: "creates environment when session_config exists but environment does not",
			input: `{
				"name": "test",
				"session_config": {
					"tags": {"repo": "org/repo"}
				}
			}`,
			envMap: map[string]string{
				"FOO": "bar",
			},
			want: map[string]interface{}{
				"name": "test",
				"session_config": map[string]interface{}{
					"tags": map[string]interface{}{"repo": "org/repo"},
					"environment": map[string]interface{}{
						"FOO": "bar",
					},
				},
			},
		},
		{
			name:    "invalid JSON returns error",
			input:   `{invalid json}`,
			envMap:  map[string]string{"KEY": "val"},
			wantErr: true,
		},
		{
			name:  "value with equals sign is preserved",
			input: `{"name":"test"}`,
			envMap: map[string]string{
				"BASE64": "abc=def=",
			},
			want: map[string]interface{}{
				"name": "test",
				"session_config": map[string]interface{}{
					"environment": map[string]interface{}{
						"BASE64": "abc=def=",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeEnvIntoScheduleJSON([]byte(tt.input), tt.envMap)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.want == nil {
				// No structural check needed for unchanged-data cases
				return
			}

			var gotMap map[string]interface{}
			require.NoError(t, json.Unmarshal(got, &gotMap))
			assert.Equal(t, tt.want, gotMap)
		})
	}
}

func TestScheduleCreateCmdFlags(t *testing.T) {
	// --file flag
	fileFlag := scheduleCreateCmd.Flags().Lookup("file")
	assert.NotNil(t, fileFlag, "--file flag should be registered on schedule create")
	assert.Equal(t, "f", fileFlag.Shorthand)

	// --env flag
	envFlag := scheduleCreateCmd.Flags().Lookup("env")
	assert.NotNil(t, envFlag, "--env flag should be registered on schedule create")
}

func TestScheduleApplyCmdFlags(t *testing.T) {
	// --file flag
	fileFlag := scheduleApplyCmd.Flags().Lookup("file")
	assert.NotNil(t, fileFlag, "--file flag should be registered on schedule apply")
	assert.Equal(t, "f", fileFlag.Shorthand)

	// --env flag
	envFlag := scheduleApplyCmd.Flags().Lookup("env")
	assert.NotNil(t, envFlag, "--env flag should be registered on schedule apply")
}
