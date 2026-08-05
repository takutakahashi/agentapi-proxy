package configrender

import (
	"bytes"
	"fmt"
	"text/template"
)

// RenderTemplate renders a Go template with a payload data map.
func RenderTemplate(tmplStr string, payload map[string]interface{}) (string, error) {
	tmpl, err := template.New("config").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// RenderTemplateMap renders all template values in a map.
func RenderTemplateMap(templates map[string]string, payload map[string]interface{}) (map[string]string, error) {
	result := make(map[string]string, len(templates))
	for key, tmplStr := range templates {
		rendered, err := RenderTemplate(tmplStr, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to render template for key '%s': %w", key, err)
		}
		result[key] = rendered
	}
	return result, nil
}
