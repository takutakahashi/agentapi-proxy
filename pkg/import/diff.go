package importexport

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"
)

// generateDiff generates a unified diff between two objects with secrets masked
func generateDiff(oldObj, newObj interface{}, resourceName string) (*string, error) {
	// Marshal to YAML for comparison
	oldYAML, err := yaml.Marshal(oldObj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %w", err)
	}

	newYAML, err := yaml.Marshal(newObj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %w", err)
	}

	// Mask secrets before comparison
	oldYAMLMasked := maskSecretsInYAML(string(oldYAML))
	newYAMLMasked := maskSecretsInYAML(string(newYAML))

	// Generate unified diff
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldYAMLMasked),
		B:        difflib.SplitLines(newYAMLMasked),
		FromFile: fmt.Sprintf("existing/%s", resourceName),
		ToFile:   fmt.Sprintf("new/%s", resourceName),
		Context:  3,
	}

	diffText, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return nil, fmt.Errorf("failed to generate diff: %w", err)
	}

	if diffText == "" {
		return nil, nil // No changes
	}

	return &diffText, nil
}

// maskSecretsInYAML masks sensitive fields in YAML content
func maskSecretsInYAML(yamlContent string) string {
	// Define secret field patterns to mask
	secretPatterns := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{
			pattern:     regexp.MustCompile(`(?m)^(\s*secret:\s+).*$`),
			replacement: `${1}***MASKED***`,
		},
		{
			pattern:     regexp.MustCompile(`(?m)^(\s*github_token:\s+).*$`),
			replacement: `${1}***MASKED***`,
		},
		{
			pattern:     regexp.MustCompile(`(?m)^(\s*access_key_id:\s+).*$`),
			replacement: `${1}***MASKED***`,
		},
		{
			pattern:     regexp.MustCompile(`(?m)^(\s*secret_access_key:\s+).*$`),
			replacement: `${1}***MASKED***`,
		},
		{
			pattern:     regexp.MustCompile(`(?m)^(\s*claude_code_oauth_token:\s+).*$`),
			replacement: `${1}***MASKED***`,
		},
		{
			// Mask env values (but keep keys visible)
			pattern:     regexp.MustCompile(`(?m)^(\s+)([A-Z_][A-Z0-9_]*:\s+).*$`),
			replacement: `${1}${2}***MASKED***`,
		},
	}

	masked := yamlContent
	for _, sp := range secretPatterns {
		masked = sp.pattern.ReplaceAllString(masked, sp.replacement)
	}

	// Mask encrypted metadata sections (not needed for comparison)
	// These sections contain algorithm, key_id, encrypted_at, version
	// We'll simply remove lines that are part of encrypted metadata
	lines := strings.Split(masked, "\n")
	var filteredLines []string
	skipUntilNextKey := false
	encryptedKeys := map[string]bool{
		"secret_encrypted:":                  true,
		"github_token_encrypted:":            true,
		"access_key_id_encrypted:":           true,
		"secret_access_key_encrypted:":       true,
		"claude_code_oauth_token_encrypted:": true,
		"env_encrypted:":                     true,
		"headers_encrypted:":                 true,
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is an encrypted key
		isEncryptedKey := false
		for key := range encryptedKeys {
			if strings.HasPrefix(trimmed, key) {
				isEncryptedKey = true
				skipUntilNextKey = true
				break
			}
		}

		if isEncryptedKey {
			continue
		}

		// If we're skipping, check if this line starts a new key (not indented as much)
		if skipUntilNextKey {
			// If line is not heavily indented and contains a colon, it's likely a new key
			if !strings.HasPrefix(line, "    ") && strings.Contains(trimmed, ":") {
				skipUntilNextKey = false
				filteredLines = append(filteredLines, line)
			}
			continue
		}

		filteredLines = append(filteredLines, line)
	}

	masked = strings.Join(filteredLines, "\n")

	// Clean up extra blank lines
	masked = regexp.MustCompile(`\n\n+`).ReplaceAllString(masked, "\n\n")

	return strings.TrimSpace(masked)
}
