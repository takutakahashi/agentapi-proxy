package controllers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
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

func (c *AuthController) RegisterRoutes(e *echo.Echo) {
	e.GET("/auth/types", c.GetAuthTypes)
	e.GET("/auth/status", c.GetAuthStatus)
	e.POST("/auth/login", c.Login)
	e.POST("/auth/github", c.GitHubLogin)
	e.POST("/auth/validate", c.ValidateAPIKey)
	e.POST("/auth/logout", c.Logout)
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
func (c *AuthController) Login(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Parse request body
	var req LoginRequest
	if err := ctx.Bind(&req); err != nil {
		c.authPresenter.PresentError(ctx.Response(), "invalid request body", http.StatusBadRequest)
		return nil
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

	response, err := c.authenticateUserUC.Execute(reqCtx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(ctx.Response(), "authentication failed", http.StatusUnauthorized)
		return nil
	}

	// Present response
	c.authPresenter.PresentAuthentication(ctx.Response(), response)
	return nil
}

// GitHubLogin handles POST /auth/github
func (c *AuthController) GitHubLogin(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Parse request body
	var req GitHubLoginRequest
	if err := ctx.Bind(&req); err != nil {
		c.authPresenter.PresentError(ctx.Response(), "invalid request body", http.StatusBadRequest)
		return nil
	}

	// Execute use case
	ucReq := &auth.GitHubAuthenticateRequest{
		Token: req.Token,
	}

	response, err := c.githubAuthenticateUC.Execute(reqCtx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(ctx.Response(), "GitHub authentication failed", http.StatusUnauthorized)
		return nil
	}

	// Present response
	c.authPresenter.PresentGitHubAuthentication(ctx.Response(), response)
	return nil
}

// ValidateAPIKey handles POST /auth/validate
func (c *AuthController) ValidateAPIKey(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract API key from Authorization header
	apiKey := extractAPIKeyFromHeader(ctx.Request())
	if apiKey == "" {
		c.authPresenter.PresentError(ctx.Response(), "API key is required", http.StatusBadRequest)
		return nil
	}

	// Execute use case
	ucReq := &auth.ValidateAPIKeyRequest{
		APIKey: apiKey,
	}

	response, err := c.validateAPIKeyUC.Execute(reqCtx, ucReq)
	if err != nil {
		c.authPresenter.PresentError(ctx.Response(), "validation failed", http.StatusInternalServerError)
		return nil
	}

	// Present response
	c.authPresenter.PresentValidation(ctx.Response(), response)
	return nil
}

// Logout handles POST /auth/logout
func (c *AuthController) Logout(ctx echo.Context) error {
	// In a stateless API with JWT/API keys, logout is typically handled client-side
	// by discarding the token. For server-side token revocation, you would:
	// 1. Extract the API key/token from the request
	// 2. Add it to a blacklist/revocation list
	// 3. Return success response

	c.authPresenter.PresentLogout(ctx.Response())
	return nil
}

type AuthTypesResponse struct {
	Types []string `json:"types"`
}

// GetAuthTypes handles GET /auth/types
func (c *AuthController) GetAuthTypes(ctx echo.Context) error {
	response := AuthTypesResponse{
		Types: []string{"api_key", "github"},
	}

	return ctx.JSON(http.StatusOK, response)
}

type AuthStatusResponse struct {
	Authenticated bool    `json:"authenticated"`
	UserID        *string `json:"user_id"`
	Method        *string `json:"method"`
}

// GetAuthStatus handles GET /auth/status
func (c *AuthController) GetAuthStatus(ctx echo.Context) error {
	// Extract user info from context if available
	reqCtx := ctx.Request().Context()
	userID, hasUserID := reqCtx.Value("userID").(string)

	response := AuthStatusResponse{
		Authenticated: hasUserID,
	}

	if hasUserID {
		response.UserID = &userID
		method := "api_key"
		response.Method = &method
	}

	return ctx.JSON(http.StatusOK, response)
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
func (m *AuthMiddleware) Authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		// Extract API key from request
		apiKey := extractAPIKeyFromHeader(ctx.Request())
		if apiKey == "" {
			m.authPresenter.PresentError(ctx.Response(), "authentication required", http.StatusUnauthorized)
			return nil
		}

		// Validate API key
		ucReq := &auth.ValidateAPIKeyRequest{
			APIKey: apiKey,
		}

		response, err := m.validateAPIKeyUC.Execute(ctx.Request().Context(), ucReq)
		if err != nil || !response.Valid {
			m.authPresenter.PresentError(ctx.Response(), "invalid API key", http.StatusUnauthorized)
			return nil
		}

		// Define context keys to avoid collisions
		type contextKey string
		const (
			userKey        contextKey = "user"
			userIDKey      contextKey = "userID"
			permissionsKey contextKey = "permissions"
		)

		// Add user to context
		reqCtx := context.WithValue(ctx.Request().Context(), userKey, response.User)
		reqCtx = context.WithValue(reqCtx, userIDKey, response.User.ID())
		reqCtx = context.WithValue(reqCtx, permissionsKey, response.Permissions)

		// Set updated context
		ctx.SetRequest(ctx.Request().WithContext(reqCtx))

		// Call next handler
		return next(ctx)
	}
}
