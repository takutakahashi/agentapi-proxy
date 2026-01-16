package importexport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Parser handles parsing of import files in various formats
type Parser struct{}

// NewParser creates a new Parser instance
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses the input data and returns TeamResources
// It auto-detects the format based on content type or content
func (p *Parser) Parse(data []byte, contentType string) (*TeamResources, error) {
	format, err := p.detectFormat(data, contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to detect format: %w", err)
	}

	switch format {
	case ExportFormatYAML:
		return p.ParseYAML(data)
	case ExportFormatTOML:
		return p.ParseTOML(data)
	case ExportFormatJSON:
		return p.ParseJSON(data)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// ParseYAML parses YAML data
func (p *Parser) ParseYAML(data []byte) (*TeamResources, error) {
	var resources TeamResources
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Strict mode: reject unknown fields

	if err := decoder.Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &resources, nil
}

// ParseTOML parses TOML data
func (p *Parser) ParseTOML(data []byte) (*TeamResources, error) {
	var resources TeamResources
	if err := toml.Unmarshal(data, &resources); err != nil {
		return nil, fmt.Errorf("failed to parse TOML: %w", err)
	}

	return &resources, nil
}

// ParseJSON parses JSON data
func (p *Parser) ParseJSON(data []byte) (*TeamResources, error) {
	var resources TeamResources
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields() // Strict mode: reject unknown fields

	if err := decoder.Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &resources, nil
}

// detectFormat detects the format from content type or content
func (p *Parser) detectFormat(data []byte, contentType string) (ExportFormat, error) {
	// First try to detect from content type
	if contentType != "" {
		format := p.formatFromContentType(contentType)
		if format != "" {
			return format, nil
		}
	}

	// Fall back to content-based detection
	return p.formatFromContent(data)
}

// formatFromContentType extracts format from content type
func (p *Parser) formatFromContentType(contentType string) ExportFormat {
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	switch {
	case strings.Contains(contentType, "yaml"), strings.Contains(contentType, "yml"):
		return ExportFormatYAML
	case strings.Contains(contentType, "toml"):
		return ExportFormatTOML
	case strings.Contains(contentType, "json"):
		return ExportFormatJSON
	default:
		return ""
	}
}

// formatFromContent detects format from content
func (p *Parser) formatFromContent(data []byte) (ExportFormat, error) {
	// Trim whitespace
	content := bytes.TrimSpace(data)
	if len(content) == 0 {
		return "", fmt.Errorf("empty content")
	}

	// Check for JSON (starts with { or [)
	if content[0] == '{' || content[0] == '[' {
		return ExportFormatJSON, nil
	}

	// Try to detect YAML vs TOML
	// YAML typically starts with --- or has key: value format
	// TOML typically has key = value format
	contentStr := string(content)

	if strings.HasPrefix(contentStr, "---") {
		return ExportFormatYAML, nil
	}

	// Count = vs : to distinguish TOML from YAML
	equalCount := strings.Count(contentStr, "=")
	colonCount := strings.Count(contentStr, ":")

	if equalCount > colonCount {
		return ExportFormatTOML, nil
	}

	// Default to YAML as it's more common and forgiving
	return ExportFormatYAML, nil
}

// Formatter handles formatting of export data
type Formatter struct{}

// NewFormatter creates a new Formatter instance
func NewFormatter() *Formatter {
	return &Formatter{}
}

// Format formats TeamResources to the specified format
func (f *Formatter) Format(resources *TeamResources, format ExportFormat, writer io.Writer) error {
	switch format {
	case ExportFormatYAML:
		return f.FormatYAML(resources, writer)
	case ExportFormatTOML:
		return f.FormatTOML(resources, writer)
	case ExportFormatJSON:
		return f.FormatJSON(resources, writer)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// FormatYAML formats as YAML
func (f *Formatter) FormatYAML(resources *TeamResources, writer io.Writer) error {
	encoder := yaml.NewEncoder(writer)
	encoder.SetIndent(2)

	if err := encoder.Encode(resources); err != nil {
		return fmt.Errorf("failed to format as YAML: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	return nil
}

// FormatTOML formats as TOML
func (f *Formatter) FormatTOML(resources *TeamResources, writer io.Writer) error {
	encoder := toml.NewEncoder(writer)
	if err := encoder.Encode(resources); err != nil {
		return fmt.Errorf("failed to format as TOML: %w", err)
	}

	return nil
}

// FormatJSON formats as JSON
func (f *Formatter) FormatJSON(resources *TeamResources, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(resources); err != nil {
		return fmt.Errorf("failed to format as JSON: %w", err)
	}

	return nil
}

// ContentTypeForFormat returns the MIME type for a given format
func ContentTypeForFormat(format ExportFormat) string {
	switch format {
	case ExportFormatYAML:
		return "application/x-yaml"
	case ExportFormatTOML:
		return "application/toml"
	case ExportFormatJSON:
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// FileExtensionForFormat returns the file extension for a given format
func FileExtensionForFormat(format ExportFormat) string {
	switch format {
	case ExportFormatYAML:
		return ".yaml"
	case ExportFormatTOML:
		return ".toml"
	case ExportFormatJSON:
		return ".json"
	default:
		return ".dat"
	}
}
