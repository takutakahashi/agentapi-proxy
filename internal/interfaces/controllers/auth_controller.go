package controllers

import (
	"context"
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"net/http"
)

// AuthController handles HTTP requests for authentication operations
type AuthController struct {
	authenticateUserUC   *auth.AuthenticateUserUseCase
	validateAPIKeyUC     *auth.ValidateAPIKeyUseCase
	githubAuthenticateUC *auth.GitHubAuthenticateUseCase
	validatePermissionUC *auth.ValidatePermissionUseCase
	authPresenter        presenters.AuthPresenter
}

// NewAuthController creates a new AuthController
func NewAuthController(
	authenticateUserUC *auth.AuthenticateUserUseCase,
	validateAPIKeyUC *auth.ValidateAPIKeyUseCase,
	githubAuthenticateUC *auth.GitHubAuthenticateUseCase,
	validatePermissionUC *auth.ValidatePermissionUseCase,
	authPresenter presenters.AuthPresenter,
) *AuthController {
	return &AuthController{
		authenticateUserUC:   authenticateUserUC,
		validateAPIKeyUC:     validateAPIKeyUC,
		githubAuthenticateUC: githubAuthenticateUC,
		validatePermissionUC: validatePermissionUC,
		authPresenter:        authPresenter,
	}
}

// LoginRequest represents the HTTP request for user login
type LoginRequest struct {
	Type     string `json:"type"` // "password", "token", "api_key"
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// GitHubLoginRequest represents the HTTP request for GitHub login
type GitHubLoginRequest struct {
	Token string `json:"token"`
}

// Login handles POST /auth/login
func (c *AuthController) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.authPresenter.PresentError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to credentials
	credentials := &services.Credentials{
		Type:     services.CredentialType(req.Type),
		Username: req.Username,
		Password: req.Password,
		Token:    req.Token,
		APIKey:   req.APIKey,
	}

	// Execute use case
	ucReq := &auth.AuthenticateUserRequest{
		Credentials: credentials,
	}

	response, err := c.authenticateUserUC.Execute(ctx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	// Present response
	c.authPresenter.PresentAuthentication(w, response)
}

// GitHubLogin handles POST /auth/github
func (c *AuthController) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req GitHubLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.authPresenter.PresentError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Execute use case
	ucReq := &auth.GitHubAuthenticateRequest{
		Token: req.Token,
	}

	response, err := c.githubAuthenticateUC.Execute(ctx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(w, "GitHub authentication failed", http.StatusUnauthorized)
		return
	}

	// Present response
	c.authPresenter.PresentGitHubAuthentication(w, response)
}

// ValidateAPIKey handles POST /auth/validate
func (c *AuthController) ValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract API key from Authorization header
	apiKey := extractAPIKeyFromHeader(r)
	if apiKey == "" {
		c.authPresenter.PresentError(w, "API key is required", http.StatusBadRequest)
		return
	}

	// Execute use case
	ucReq := &auth.ValidateAPIKeyRequest{
		APIKey: apiKey,
	}

	response, err := c.validateAPIKeyUC.Execute(ctx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(w, "validation failed", http.StatusInternalServerError)
		return
	}

	// Present response
	c.authPresenter.PresentValidation(w, response)
}

// Logout handles POST /auth/logout
func (c *AuthController) Logout(w http.ResponseWriter, r *http.Request) {
	// In a stateless API with JWT/API keys, logout is typically handled client-side
	// by discarding the token. For server-side token revocation, you would:
	// 1. Extract the API key/token from the request
	// 2. Add it to a blacklist/revocation list
	// 3. Return success response

	c.authPresenter.PresentLogout(w)
}

// extractAPIKeyFromHeader extracts API key from Authorization header
func extractAPIKeyFromHeader(r *http.Request) string {
	// Check Authorization header for "Bearer <token>" or "API-Key <key>"
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Simple implementation - in real applications, parse Bearer/API-Key prefixes
	// For now, assume the entire header value is the API key
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	if len(authHeader) > 8 && authHeader[:8] == "API-Key " {
		return authHeader[8:]
	}

	return authHeader
}

// AuthMiddleware provides authentication middleware
type AuthMiddleware struct {
	validateAPIKeyUC *auth.ValidateAPIKeyUseCase
	authPresenter    presenters.AuthPresenter
}

// NewAuthMiddleware creates a new AuthMiddleware
func NewAuthMiddleware(
	validateAPIKeyUC *auth.ValidateAPIKeyUseCase,
	authPresenter presenters.AuthPresenter,
) *AuthMiddleware {
	return &AuthMiddleware{
		validateAPIKeyUC: validateAPIKeyUC,
		authPresenter:    authPresenter,
	}
}

// Authenticate is a middleware that validates API keys and sets user context
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract API key from request
		apiKey := extractAPIKeyFromHeader(r)
		if apiKey == "" {
			m.authPresenter.PresentError(w, "authentication required", http.StatusUnauthorized)
			return
		}

		// Validate API key
		ucReq := &auth.ValidateAPIKeyRequest{
			APIKey: apiKey,
		}

		response, err := m.validateAPIKeyUC.Execute(r.Context(), ucReq)
		if err != nil || !response.Valid {
			m.authPresenter.PresentError(w, "invalid API key", http.StatusUnauthorized)
			return
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), "user", response.User)
		ctx = context.WithValue(ctx, "userID", response.User.ID())
		ctx = context.WithValue(ctx, "permissions", response.Permissions)

		// Call next handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
