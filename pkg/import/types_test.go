package importexport

import (
	"testing"
)

func TestImportMode(t *testing.T) {
	tests := []struct {
		mode     ImportMode
		expected string
	}{
		{ImportModeCreate, "create"},
		{ImportModeUpdate, "update"},
		{ImportModeUpsert, "upsert"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if string(tt.mode) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.mode))
			}
		})
	}
}

func TestExportFormat(t *testing.T) {
	tests := []struct {
		format   ExportFormat
		expected string
	}{
		{ExportFormatYAML, "yaml"},
		{ExportFormatTOML, "toml"},
		{ExportFormatJSON, "json"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			if string(tt.format) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.format))
			}
		})
	}
}

func TestImportOptions_Defaults(t *testing.T) {
	opts := ImportOptions{}

	if opts.DryRun {
		t.Error("Expected DryRun to be false by default")
	}
	if opts.AllowPartial {
		t.Error("Expected AllowPartial to be false by default")
	}
	if opts.RegenerateAll {
		t.Error("Expected RegenerateAll to be false by default")
	}
}

func TestExportOptions_Defaults(t *testing.T) {
	opts := ExportOptions{}

	if opts.IncludeSecrets {
		t.Error("Expected IncludeSecrets to be false by default")
	}
	if len(opts.StatusFilter) != 0 {
		t.Error("Expected StatusFilter to be empty by default")
	}
	if len(opts.IncludeTypes) != 0 {
		t.Error("Expected IncludeTypes to be empty by default")
	}
}
