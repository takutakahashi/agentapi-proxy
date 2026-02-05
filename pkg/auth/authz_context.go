package auth

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// AuthorizationContext contains pre-resolved authorization information
// This is populated by the Auth Middleware and consumed by handlers
type AuthorizationContext struct {
	// User information
	User *entities.User

	// Personal scope permissions
	PersonalScope PersonalScopeAuth

	// Team scope permissions
	TeamScope TeamScopeAuth
}

// PersonalScopeAuth contains authorization info for personal (user) scope
type PersonalScopeAuth struct {
	// UserID of the authenticated user
	UserID string

	// CanCreate indicates if the user can create personal-scoped resources
	CanCreate bool

	// CanRead indicates if the user can read their own personal-scoped resources
	CanRead bool

	// CanUpdate indicates if the user can update their own personal-scoped resources
	CanUpdate bool

	// CanDelete indicates if the user can delete their own personal-scoped resources
	CanDelete bool
}

// TeamScopeAuth contains authorization info for team scope
type TeamScopeAuth struct {
	// Teams is a list of team IDs the user belongs to (format: "org/team-slug")
	Teams []string

	// TeamPermissions maps team IDs to their permissions
	TeamPermissions map[string]TeamPermissions

	// IsAdmin indicates if the user is an admin (can access all teams)
	IsAdmin bool
}

// TeamPermissions represents permissions for a specific team
type TeamPermissions struct {
	// TeamID in format "org/team-slug"
	TeamID string

	// CanCreate indicates if the user can create resources in this team
	CanCreate bool

	// CanRead indicates if the user can read resources in this team
	CanRead bool

	// CanUpdate indicates if the user can update resources in this team
	CanUpdate bool

	// CanDelete indicates if the user can delete resources in this team
	CanDelete bool
}

// CanAccessTeam checks if the user can access the specified team
func (a *AuthorizationContext) CanAccessTeam(teamID string) bool {
	if a.TeamScope.IsAdmin {
		return true
	}

	for _, team := range a.TeamScope.Teams {
		if team == teamID {
			return true
		}
	}
	return false
}

// CanCreateInTeam checks if the user can create resources in the specified team
func (a *AuthorizationContext) CanCreateInTeam(teamID string) bool {
	if a.TeamScope.IsAdmin {
		return true
	}

	if perms, ok := a.TeamScope.TeamPermissions[teamID]; ok {
		return perms.CanCreate
	}
	return false
}

// CanReadInTeam checks if the user can read resources in the specified team
func (a *AuthorizationContext) CanReadInTeam(teamID string) bool {
	if a.TeamScope.IsAdmin {
		return true
	}

	if perms, ok := a.TeamScope.TeamPermissions[teamID]; ok {
		return perms.CanRead
	}
	return false
}

// CanAccessResource checks if the user can access a resource based on scope
func (a *AuthorizationContext) CanAccessResource(ownerUserID string, scope string, teamID string) bool {
	// Admin can access everything
	if a.TeamScope.IsAdmin {
		return true
	}

	// Team-scoped resources
	if scope == "team" && teamID != "" {
		return a.CanAccessTeam(teamID)
	}

	// User-scoped resources - check if the user is the owner
	return a.PersonalScope.UserID == ownerUserID
}

// CanCreateResource checks if the user can create a resource with the given scope
func (a *AuthorizationContext) CanCreateResource(scope string, teamID string) bool {
	// Admin can create everywhere
	if a.TeamScope.IsAdmin {
		return true
	}

	// Team-scoped creation
	if scope == "team" && teamID != "" {
		return a.CanCreateInTeam(teamID)
	}

	// Personal-scoped creation
	return a.PersonalScope.CanCreate
}

// CanModifyResource checks if the user can modify (update/delete) a resource
func (a *AuthorizationContext) CanModifyResource(ownerUserID string, scope string, teamID string) bool {
	// For now, modification follows the same logic as access
	// In the future, we might have stricter modification rules
	return a.CanAccessResource(ownerUserID, scope, teamID)
}
