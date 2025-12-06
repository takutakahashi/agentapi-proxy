package presenters

import (
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"net/http"
)

// AuthPresenter defines the interface for presenting authentication data
type AuthPresenter interface {
	PresentAuthentication(w http.ResponseWriter, response *auth.AuthenticateUserResponse)
	PresentGitHubAuthentication(w http.ResponseWriter, response *auth.GitHubAuthenticateResponse)
	PresentValidation(w http.ResponseWriter, response *auth.ValidateAPIKeyResponse)
	PresentLogout(w http.ResponseWriter)
	PresentError(w http.ResponseWriter, message string, statusCode int)
}

// HTTPAuthPresenter implements AuthPresenter for HTTP responses
type HTTPAuthPresenter struct{}

// NewHTTPAuthPresenter creates a new HTTPAuthPresenter
func NewHTTPAuthPresenter() *HTTPAuthPresenter {
	return &HTTPAuthPresenter{}
}

// AuthenticationResponse represents the response for user authentication
type AuthenticationResponse struct {
	User        *UserResponse `json:"user"`
	APIKey      string        `json:"api_key"`
	Permissions []string      `json:"permissions"`
	ExpiresAt   *string       `json:"expires_at,omitempty"`
}

// GitHubAuthenticationResponse represents the response for GitHub authentication
type GitHubAuthenticationResponse struct {
	User        *UserResponse       `json:"user"`
	APIKey      string              `json:"api_key"`
	Permissions []string            `json:"permissions"`
	GitHubInfo  *GitHubInfoResponse `json:"github_info"`
	ExpiresAt   *string             `json:"expires_at,omitempty"`
}

// ValidationResponse represents the response for API key validation
type ValidationResponse struct {
	Valid       bool          `json:"valid"`
	User        *UserResponse `json:"user,omitempty"`
	Permissions []string      `json:"permissions,omitempty"`
}

// UserResponse represents user information in HTTP responses
type UserResponse struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Username    string   `json:"username"`
	Email       *string  `json:"email,omitempty"`
	DisplayName *string  `json:"display_name,omitempty"`
	AvatarURL   *string  `json:"avatar_url,omitempty"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	LastUsedAt  *string  `json:"last_used_at,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

// GitHubInfoResponse represents GitHub user information in HTTP responses
type GitHubInfoResponse struct {
	ID        int64                           `json:"id"`
	Login     string                          `json:"login"`
	Name      *string                         `json:"name,omitempty"`
	Email     *string                         `json:"email,omitempty"`
	AvatarURL string                          `json:"avatar_url"`
	Company   *string                         `json:"company,omitempty"`
	Location  *string                         `json:"location,omitempty"`
	Teams     []*GitHubTeamMembershipResponse `json:"teams,omitempty"`
}

// GitHubTeamMembershipResponse represents GitHub team membership in HTTP responses
type GitHubTeamMembershipResponse struct {
	TeamID   int    `json:"team_id"`
	TeamName string `json:"team_name"`
	OrgName  string `json:"org_name"`
	Role     string `json:"role"`
}

// LogoutResponse represents the response for logout
type LogoutResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PresentAuthentication presents an authentication response
func (p *HTTPAuthPresenter) PresentAuthentication(w http.ResponseWriter, response *auth.AuthenticateUserResponse) {
	userResp := p.convertUserToResponse(response.User)
	permissions := p.convertPermissionsToStrings(response.Permissions)

	authResp := &AuthenticationResponse{
		User:        userResp,
		APIKey:      response.APIKey.Key,
		Permissions: permissions,
	}

	if response.APIKey.ExpiresAt != nil {
		authResp.ExpiresAt = response.APIKey.ExpiresAt
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(authResp)
}

// PresentGitHubAuthentication presents a GitHub authentication response
func (p *HTTPAuthPresenter) PresentGitHubAuthentication(w http.ResponseWriter, response *auth.GitHubAuthenticateResponse) {
	userResp := p.convertUserToResponse(response.User)
	permissions := p.convertPermissionsToStrings(response.Permissions)
	githubInfo := p.convertGitHubInfoToResponse(response.GitHubInfo)

	authResp := &GitHubAuthenticationResponse{
		User:        userResp,
		APIKey:      response.APIKey.Key,
		Permissions: permissions,
		GitHubInfo:  githubInfo,
	}

	if response.APIKey.ExpiresAt != nil {
		authResp.ExpiresAt = response.APIKey.ExpiresAt
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(authResp)
}

// PresentValidation presents an API key validation response
func (p *HTTPAuthPresenter) PresentValidation(w http.ResponseWriter, response *auth.ValidateAPIKeyResponse) {
	validationResp := &ValidationResponse{
		Valid: response.Valid,
	}

	if response.Valid && response.User != nil {
		validationResp.User = p.convertUserToResponse(response.User)
		validationResp.Permissions = p.convertPermissionsToStrings(response.Permissions)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(validationResp)
}

// PresentLogout presents a logout response
func (p *HTTPAuthPresenter) PresentLogout(w http.ResponseWriter) {
	logoutResp := &LogoutResponse{
		Success: true,
		Message: "Successfully logged out",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(logoutResp)
}

// PresentError presents an error response
func (p *HTTPAuthPresenter) PresentError(w http.ResponseWriter, message string, statusCode int) {
	errorResp := &entities.ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errorResp)
}

// convertUserToResponse converts a domain user to HTTP response format
func (p *HTTPAuthPresenter) convertUserToResponse(user *entities.User) *UserResponse {
	resp := &UserResponse{
		ID:          string(user.ID()),
		Type:        string(user.Type()),
		Username:    user.Username(),
		Status:      string(user.Status()),
		Roles:       p.convertRolesToStrings(user.Roles()),
		Permissions: p.convertPermissionsToStrings(user.Permissions()),
		CreatedAt:   user.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}

	if email := user.Email(); email != nil {
		resp.Email = email
	}

	if displayName := user.DisplayName(); displayName != nil {
		resp.DisplayName = displayName
	}

	if avatarURL := user.AvatarURL(); avatarURL != nil {
		resp.AvatarURL = avatarURL
	}

	if lastUsedAt := user.LastUsedAt(); lastUsedAt != nil {
		lastUsedStr := lastUsedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastUsedAt = &lastUsedStr
	}

	return resp
}

// convertGitHubInfoToResponse converts GitHub user info to HTTP response format
func (p *HTTPAuthPresenter) convertGitHubInfoToResponse(githubInfo *entities.GitHubUserInfo) *GitHubInfoResponse {
	if githubInfo == nil {
		return nil
	}

	resp := &GitHubInfoResponse{
		ID:        githubInfo.ID(),
		Login:     githubInfo.Login(),
		AvatarURL: githubInfo.AvatarURL(),
	}

	if name := githubInfo.Name(); name != "" {
		resp.Name = &name
	}

	if email := githubInfo.Email(); email != "" {
		resp.Email = &email
	}

	if company := githubInfo.Company(); company != "" {
		resp.Company = &company
	}

	if location := githubInfo.Location(); location != "" {
		resp.Location = &location
	}

	// Convert teams if available
	// Note: This assumes the User entity has access to GitHub team info
	// In a real implementation, you'd need to pass teams separately or store them in the user

	return resp
}

// convertRolesToStrings converts domain roles to string slice
func (p *HTTPAuthPresenter) convertRolesToStrings(roles []entities.Role) []string {
	result := make([]string, len(roles))
	for i, role := range roles {
		result[i] = string(role)
	}
	return result
}

// convertPermissionsToStrings converts domain permissions to string slice
func (p *HTTPAuthPresenter) convertPermissionsToStrings(permissions []entities.Permission) []string {
	result := make([]string, len(permissions))
	for i, permission := range permissions {
		result[i] = string(permission)
	}
	return result
}
