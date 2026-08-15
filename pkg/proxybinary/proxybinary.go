// Package proxybinary centralizes selection of the ccplant proxy executable.
package proxybinary

import "strings"

const (
	// EnvName is the project-wide override for the proxy executable path.
	EnvName = "CCPLANT_BINARY_PATH"
	// Default is used when EnvName is unset.
	Default = "ccplant"
)

// Resolve returns a configured executable path or the project default.
func Resolve(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return Default
}

// FromMap resolves the configured executable from an environment map.
func FromMap(env map[string]string) string {
	return Resolve(env[EnvName])
}

// ShellReference resolves EnvName at hook execution time while preserving the default.
func ShellReference() string {
	return `"${CCPLANT_BINARY_PATH:-ccplant}"`
}
