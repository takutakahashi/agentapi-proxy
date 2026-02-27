package auth

import "context"

// TeamMappingRepository defines the interface for persistent user-team mapping cache.
// Implementations persist the mapping of GitHub usernames to their team memberships
// so that the data survives pod restarts and reduces GitHub API calls.
type TeamMappingRepository interface {
	// Get retrieves the team memberships for a given username.
	// Returns the memberships, true if found, and any error.
	Get(ctx context.Context, username string) ([]GitHubTeamMembership, bool, error)

	// Set stores the team memberships for a given username.
	Set(ctx context.Context, username string, teams []GitHubTeamMembership) error
}
