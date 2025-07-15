package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar represents a single environment variable
type EnvVar struct {
	Key   string
	Value string
}

// LoadRoleEnvVars loads environment variables for a specific role
func LoadRoleEnvVars(config *RoleEnvFilesConfig, role string) ([]EnvVar, error) {
	if !config.Enabled {
		return nil, nil
	}

	var envVars []EnvVar

	// Load default.env if enabled
	if config.LoadDefault {
		defaultPath := filepath.Join(config.Path, "default.env")
		if vars, err := loadEnvFile(defaultPath); err == nil {
			envVars = append(envVars, vars...)
			log.Printf("[ENV] Loaded %d environment variables from default.env", len(vars))
		} else if !os.IsNotExist(err) {
			log.Printf("[ENV] Warning: Failed to load default.env: %v", err)
		}
	}

	// Load role-specific env file
	if role != "" {
		rolePath := filepath.Join(config.Path, fmt.Sprintf("%s.env", role))
		if vars, err := loadEnvFile(rolePath); err == nil {
			envVars = append(envVars, vars...)
			log.Printf("[ENV] Loaded %d environment variables for role '%s'", len(vars), role)
		} else if !os.IsNotExist(err) {
			log.Printf("[ENV] Warning: Failed to load env file for role '%s': %v", role, err)
		}
	}

	return envVars, nil
}

// loadEnvFile reads environment variables from a file
func loadEnvFile(filepath string) ([]EnvVar, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("[ENV] Warning: Failed to close file %s: %v", filepath, err)
		}
	}()

	var envVars []EnvVar
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			log.Printf("[ENV] Warning: Invalid format at line %d in %s: %s", lineNum, filepath, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove surrounding quotes if present
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Validate key
		if key == "" || strings.ContainsAny(key, " \t\n") {
			log.Printf("[ENV] Warning: Invalid key at line %d in %s: '%s'", lineNum, filepath, key)
			continue
		}

		envVars = append(envVars, EnvVar{
			Key:   key,
			Value: value,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return envVars, nil
}

// ApplyEnvVars sets environment variables in the current process
// Returns the list of variables that were set
func ApplyEnvVars(envVars []EnvVar) []string {
	var applied []string

	for _, env := range envVars {
		// Check if value already exists
		_, existed := os.LookupEnv(env.Key)

		// Set the new value
		if err := os.Setenv(env.Key, env.Value); err != nil {
			log.Printf("[ENV] Error setting environment variable %s: %v", env.Key, err)
			continue
		}

		// Log the change
		if existed {
			log.Printf("[ENV] Updated %s (previous value existed)", env.Key)
		} else {
			log.Printf("[ENV] Set %s", env.Key)
		}

		applied = append(applied, env.Key)
	}

	return applied
}

// GetRoleFromContext extracts the user's role from the authentication context
// This is a helper function that should be called from the auth package
func GetRoleFromContext(userID string, role string) string {
	if role == "" {
		return "guest"
	}
	return role
}

// LoadTeamEnvVars loads environment variables from a specific file for a team
func LoadTeamEnvVars(envFile string) ([]EnvVar, error) {
	if envFile == "" {
		return nil, nil
	}

	// Check if the file exists
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("env file not found: %s", envFile)
	}

	vars, err := loadEnvFile(envFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load env file %s: %w", envFile, err)
	}

	log.Printf("[ENV] Loaded %d environment variables from team env file: %s", len(vars), envFile)
	return vars, nil
}
