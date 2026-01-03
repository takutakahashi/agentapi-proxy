package entities

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// UserID represents a unique user identifier
type UserID string

// UserType represents the type of user authentication
type UserType string

const (
	UserTypeAPIKey  UserType = "api_key"
	UserTypeGitHub  UserType = "github"
	UserTypeAWS     UserType = "aws"
	UserTypeRegular UserType = "regular"
	UserTypeAdmin   UserType = "admin"
)

// Permission represents a user permission
type Permission string

const (
	PermissionSessionCreate Permission = "session:create"
	PermissionSessionRead   Permission = "session:read"
	PermissionSessionUpdate Permission = "session:update"
	PermissionSessionDelete Permission = "session:delete"
	PermissionAdmin         Permission = "admin"
)

// UserStatus represents the status of a user
type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusInactive  UserStatus = "inactive"
	UserStatusSuspended UserStatus = "suspended"
)

// Role represents a user role
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleUser      Role = "user"
	RoleMember    Role = "member"
	RoleDeveloper Role = "developer"
	RoleReadOnly  Role = "readonly"
)

// User represents a user domain entity
type User struct {
	id          UserID
	userType    UserType
	username    string
	email       *string
	displayName *string
	avatarURL   *string
	status      UserStatus
	roles       []Role
	permissions []Permission
	envFile     string
	createdAt   time.Time
	lastUsedAt  *time.Time
	githubInfo  *GitHubUserInfo
	awsInfo     *AWSUserInfo
}

// GitHubUserInfo contains GitHub-specific user information
type GitHubUserInfo struct {
	login         string
	id            int64
	name          string
	email         string
	avatarURL     string
	company       string
	location      string
	organizations []GitHubOrganization
	teams         []GitHubTeamMembership
}

// Login returns the GitHub login
func (g *GitHubUserInfo) Login() string {
	return g.login
}

// ID returns the GitHub ID
func (g *GitHubUserInfo) ID() int64 {
	return g.id
}

// Name returns the GitHub name
func (g *GitHubUserInfo) Name() string {
	return g.name
}

// Email returns the GitHub email
func (g *GitHubUserInfo) Email() string {
	return g.email
}

// AvatarURL returns the GitHub avatar URL
func (g *GitHubUserInfo) AvatarURL() string {
	return g.avatarURL
}

// Company returns the GitHub company
func (g *GitHubUserInfo) Company() string {
	return g.company
}

// Location returns the GitHub location
func (g *GitHubUserInfo) Location() string {
	return g.location
}

// Organizations returns the GitHub organizations
func (g *GitHubUserInfo) Organizations() []GitHubOrganization {
	orgs := make([]GitHubOrganization, len(g.organizations))
	copy(orgs, g.organizations)
	return orgs
}

// Teams returns the GitHub teams
func (g *GitHubUserInfo) Teams() []GitHubTeamMembership {
	teams := make([]GitHubTeamMembership, len(g.teams))
	copy(teams, g.teams)
	return teams
}

// NewGitHubUserInfo creates a new GitHubUserInfo
func NewGitHubUserInfo(id int64, login, name, email, avatarURL, company, location string) *GitHubUserInfo {
	return &GitHubUserInfo{
		id:        id,
		login:     login,
		name:      name,
		email:     email,
		avatarURL: avatarURL,
		company:   company,
		location:  location,
	}
}

// GitHubOrganization represents a GitHub organization
type GitHubOrganization struct {
	Login string
	ID    int64
}

// GitHubTeamMembership represents GitHub team membership
type GitHubTeamMembership struct {
	Organization string
	TeamSlug     string
	TeamName     string
	Role         string
}

// NewUser creates a new user
func NewUser(id UserID, userType UserType, username string) *User {
	now := time.Now()
	return &User{
		id:          id,
		userType:    userType,
		username:    username,
		status:      UserStatusActive,
		roles:       []Role{RoleUser},
		permissions: []Permission{PermissionSessionCreate, PermissionSessionRead},
		createdAt:   now,
		lastUsedAt:  &now,
	}
}

// NewGitHubUser creates a new GitHub user
func NewGitHubUser(id UserID, username, email string, githubInfo *GitHubUserInfo) *User {
	now := time.Now()
	return &User{
		id:          id,
		userType:    UserTypeGitHub,
		username:    username,
		email:       &email,
		status:      UserStatusActive,
		roles:       []Role{RoleUser},
		permissions: []Permission{PermissionSessionCreate, PermissionSessionRead},
		createdAt:   now,
		lastUsedAt:  &now,
		githubInfo:  githubInfo,
	}
}

// NewAWSUser creates a new AWS IAM user
func NewAWSUser(id UserID, username string, awsInfo *AWSUserInfo) *User {
	now := time.Now()
	return &User{
		id:          id,
		userType:    UserTypeAWS,
		username:    username,
		status:      UserStatusActive,
		roles:       []Role{RoleUser},
		permissions: []Permission{PermissionSessionCreate, PermissionSessionRead},
		createdAt:   now,
		lastUsedAt:  &now,
		awsInfo:     awsInfo,
	}
}

// ID returns the user ID
func (u *User) ID() UserID {
	return u.id
}

// UserType returns the user type
func (u *User) UserType() UserType {
	return u.userType
}

// Type returns the user type (alias for UserType)
func (u *User) Type() UserType {
	return u.userType
}

// Username returns the username
func (u *User) Username() string {
	return u.username
}

// Email returns the user email
func (u *User) Email() *string {
	return u.email
}

// DisplayName returns the user display name
func (u *User) DisplayName() *string {
	return u.displayName
}

// AvatarURL returns the user avatar URL
func (u *User) AvatarURL() *string {
	return u.avatarURL
}

// Roles returns the user roles
func (u *User) Roles() []Role {
	roles := make([]Role, len(u.roles))
	copy(roles, u.roles)
	return roles
}

// Permissions returns a copy of user permissions
func (u *User) Permissions() []Permission {
	permissions := make([]Permission, len(u.permissions))
	copy(permissions, u.permissions)
	return permissions
}

// EnvFile returns the environment file path
func (u *User) EnvFile() string {
	return u.envFile
}

// CreatedAt returns when the user was created
func (u *User) CreatedAt() time.Time {
	return u.createdAt
}

// LastUsedAt returns when the user was last used
func (u *User) LastUsedAt() *time.Time {
	return u.lastUsedAt
}

// Status returns the user status
func (u *User) Status() UserStatus {
	return u.status
}

// IsActive returns true if the user is active
func (u *User) IsActive() bool {
	return u.status == UserStatusActive
}

// GitHubInfo returns GitHub-specific information
func (u *User) GitHubInfo() *GitHubUserInfo {
	return u.githubInfo
}

// AWSInfo returns AWS IAM-specific information
func (u *User) AWSInfo() *AWSUserInfo {
	return u.awsInfo
}

// SetEmail sets the user email
func (u *User) SetEmail(email string) {
	u.email = &email
}

// SetRoles sets the user roles
func (u *User) SetRoles(roles []Role) error {
	validRoles := []Role{RoleAdmin, RoleUser, RoleMember, RoleDeveloper, RoleReadOnly}
	for _, role := range roles {
		valid := false
		for _, validRole := range validRoles {
			if role == validRole {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid role: %s", role)
		}
	}
	u.roles = make([]Role, len(roles))
	copy(u.roles, roles)
	return nil
}

// SetPermissions sets the user permissions
func (u *User) SetPermissions(permissions []Permission) {
	u.permissions = make([]Permission, len(permissions))
	copy(u.permissions, permissions)
}

// AddPermission adds a permission to the user
func (u *User) AddPermission(permission Permission) {
	// Check if permission already exists
	for _, p := range u.permissions {
		if p == permission {
			return
		}
	}
	u.permissions = append(u.permissions, permission)
}

// RemovePermission removes a permission from the user
func (u *User) RemovePermission(permission Permission) {
	for i, p := range u.permissions {
		if p == permission {
			u.permissions = append(u.permissions[:i], u.permissions[i+1:]...)
			return
		}
	}
}

// SetEnvFile sets the environment file path
func (u *User) SetEnvFile(envFile string) {
	u.envFile = envFile
}

// UpdateLastUsed updates the last used timestamp
func (u *User) UpdateLastUsed() {
	now := time.Now()
	u.lastUsedAt = &now
}

// Deactivate deactivates the user
func (u *User) Deactivate() {
	u.status = UserStatusInactive
}

// Activate activates the user
func (u *User) Activate() {
	u.status = UserStatusActive
}

// HasPermission checks if the user has a specific permission
func (u *User) HasPermission(permission Permission) bool {
	// Admin has all permissions
	for _, role := range u.roles {
		if role == RoleAdmin {
			return true
		}
	}

	// Check for wildcard permission
	for _, p := range u.permissions {
		if p == "*" {
			return true
		}
		if p == permission {
			return true
		}
	}

	return false
}

// HasAnyPermission checks if the user has any of the specified permissions
func (u *User) HasAnyPermission(permissions ...Permission) bool {
	for _, permission := range permissions {
		if u.HasPermission(permission) {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the user is an admin
func (u *User) IsAdmin() bool {
	for _, role := range u.roles {
		if role == RoleAdmin {
			return true
		}
	}
	return false
}

// CanAccessSession checks if the user can access a specific session
func (u *User) CanAccessSession(sessionUserID UserID) bool {
	// Admin can access all sessions
	if u.IsAdmin() {
		return true
	}

	// Users can only access their own sessions
	return u.id == sessionUserID
}

// IsMemberOfTeam checks if the user is a member of the specified team
// teamID should be in the format "org/team-slug"
func (u *User) IsMemberOfTeam(teamID string) bool {
	if u.githubInfo == nil {
		return false
	}
	for _, team := range u.githubInfo.Teams() {
		fullTeamID := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		if fullTeamID == teamID {
			return true
		}
	}
	return false
}

// CanAccessResource checks if the user can access a resource based on its scope
// For user-scoped resources, only the owner or admin can access
// For team-scoped resources, any team member or admin can access
func (u *User) CanAccessResource(ownerUserID UserID, scope string, teamID string) bool {
	// Admin can access all resources
	if u.IsAdmin() {
		return true
	}

	// Team-scoped: check team membership
	if scope == "team" && teamID != "" {
		return u.IsMemberOfTeam(teamID)
	}

	// Default (user-scoped): only owner can access
	return u.id == ownerUserID
}

// UpdateGitHubInfo updates GitHub-specific information
func (u *User) UpdateGitHubInfo(githubInfo *GitHubUserInfo) error {
	if u.userType != UserTypeGitHub {
		return errors.New("cannot update GitHub info for non-GitHub user")
	}
	u.githubInfo = githubInfo
	return nil
}

// SetGitHubInfo sets GitHub information for the user
func (u *User) SetGitHubInfo(info *GitHubUserInfo, teams []GitHubTeamMembership) {
	u.githubInfo = info
	if info != nil {
		u.githubInfo.teams = teams
	}
}

// SetAWSInfo sets AWS IAM information for the user
func (u *User) SetAWSInfo(info *AWSUserInfo) {
	u.awsInfo = info
}

// GetDisplayName returns a display-friendly name
func (u *User) GetDisplayName() string {
	if u.githubInfo != nil && u.githubInfo.Name() != "" {
		return u.githubInfo.Name()
	}
	if u.username != "" {
		return u.username
	}
	return string(u.id)
}

// Validate ensures the user is in a valid state
func (u *User) Validate() error {
	if u.id == "" {
		return errors.New("user ID cannot be empty")
	}

	if u.username == "" {
		return errors.New("username cannot be empty")
	}

	if u.userType == "" {
		return errors.New("user type cannot be empty")
	}

	// Validate user type
	validTypes := []UserType{UserTypeAPIKey, UserTypeGitHub, UserTypeAWS}
	typeValid := false
	for _, validType := range validTypes {
		if u.userType == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid user type: %s", u.userType)
	}

	// Validate roles
	validRoles := []Role{RoleAdmin, RoleUser, RoleMember, RoleDeveloper, RoleReadOnly}
	for _, role := range u.roles {
		roleValid := false
		for _, validRole := range validRoles {
			if role == validRole {
				roleValid = true
				break
			}
		}
		if !roleValid {
			return fmt.Errorf("invalid role: %s", role)
		}
	}

	// Validate email format if provided
	if u.email != nil && !strings.Contains(*u.email, "@") {
		return fmt.Errorf("invalid email format: %s", *u.email)
	}

	if u.createdAt.IsZero() {
		return errors.New("created at time cannot be zero")
	}

	return nil
}
