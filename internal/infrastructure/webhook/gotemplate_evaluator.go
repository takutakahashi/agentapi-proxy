package webhook

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// GoTemplateEvaluator evaluates Go template expressions against JSON payloads
type GoTemplateEvaluator struct{}

// NewGoTemplateEvaluator creates a new GoTemplateEvaluator
func NewGoTemplateEvaluator() *GoTemplateEvaluator {
	return &GoTemplateEvaluator{}
}

// Evaluate evaluates a Go template expression against a payload
// The template should return "true" or "false" as a string
// Returns true if the template evaluates to "true", false otherwise
func (e *GoTemplateEvaluator) Evaluate(payload map[string]interface{}, templateStr string) (bool, error) {
	if templateStr == "" {
		return true, nil
	}

	// Create a new template with custom functions
	tmpl, err := template.New("condition").Funcs(e.FuncMap()).Parse(templateStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute the template with the payload
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return false, fmt.Errorf("failed to execute template: %w", err)
	}

	// Check if the result is "true"
	result := strings.TrimSpace(buf.String())
	return result == "true", nil
}

// FuncMap returns custom template functions
func (e *GoTemplateEvaluator) FuncMap() template.FuncMap {
	return template.FuncMap{
		// String functions
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"toLower":   strings.ToLower,
		"toUpper":   strings.ToUpper,
		"trimSpace": strings.TrimSpace,
		"split":     strings.Split,
		"join":      strings.Join,
		"replace":   strings.ReplaceAll,

		// Type conversion functions
		"toString": func(v interface{}) string {
			return fmt.Sprintf("%v", v)
		},

		// Collection functions
		"len": func(v interface{}) int {
			switch val := v.(type) {
			case string:
				return len(val)
			case []interface{}:
				return len(val)
			case map[string]interface{}:
				return len(val)
			default:
				return 0
			}
		},

		// Utility functions for common patterns
		"in": func(value interface{}, list []interface{}) bool {
			for _, item := range list {
				if value == item {
					return true
				}
			}
			return false
		},

		"matches": func(pattern, value string) bool {
			return strings.Contains(value, pattern)
		},
	}
}
