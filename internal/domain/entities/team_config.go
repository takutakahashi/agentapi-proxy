package entities

import (
	"errors"
)

// TeamConfig represents a team configuration domain entity
type TeamConfig struct {
	teamID         string
	serviceAccount *ServiceAccount
	envVars        map[string]string
}

// NewTeamConfig creates a new team configuration
func NewTeamConfig(teamID string, serviceAccount *ServiceAccount, envVars map[string]string) *TeamConfig {
	if envVars == nil {
		envVars = make(map[string]string)
	}
	return &TeamConfig{
		teamID:         teamID,
		serviceAccount: serviceAccount,
		envVars:        envVars,
	}
}

// TeamID returns the team ID
func (tc *TeamConfig) TeamID() string {
	return tc.teamID
}

// ServiceAccount returns the service account
func (tc *TeamConfig) ServiceAccount() *ServiceAccount {
	return tc.serviceAccount
}

// EnvVars returns the environment variables
func (tc *TeamConfig) EnvVars() map[string]string {
	return tc.envVars
}

// SetServiceAccount sets the service account
func (tc *TeamConfig) SetServiceAccount(sa *ServiceAccount) {
	tc.serviceAccount = sa
}

// SetEnvVars sets the environment variables
func (tc *TeamConfig) SetEnvVars(envVars map[string]string) {
	tc.envVars = envVars
}

// AddEnvVar adds an environment variable
func (tc *TeamConfig) AddEnvVar(key, value string) {
	if tc.envVars == nil {
		tc.envVars = make(map[string]string)
	}
	tc.envVars[key] = value
}

// RemoveEnvVar removes an environment variable
func (tc *TeamConfig) RemoveEnvVar(key string) {
	delete(tc.envVars, key)
}

// Validate validates the team configuration
func (tc *TeamConfig) Validate() error {
	if tc.teamID == "" {
		return errors.New("team ID cannot be empty")
	}

	// Service account is optional, but if present, it must be valid
	if tc.serviceAccount != nil {
		if err := tc.serviceAccount.Validate(); err != nil {
			return err
		}
	}

	return nil
}
