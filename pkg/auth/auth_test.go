package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestAuthMiddleware_Disabled(t *testing.T) {
	// Create config with auth disabled
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := AuthMiddleware(cfg, nil)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthMiddleware_MissingAPIKey(t *testing.T) {
	// Create config with auth enabled
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Static: &config.StaticAuthConfig{
				Enabled:    true,
				HeaderName: "X-API-Key",
			},
		},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := AuthMiddleware(cfg, nil)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

func TestAuthMiddleware_InvalidAPIKey(t *testing.T) {
	// Create config with auth enabled and valid API keys
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Static: &config.StaticAuthConfig{
				Enabled:    true,
				HeaderName: "X-API-Key",
				APIKeys: []config.APIKey{
					{
						Key:         "valid-key",
						UserID:      "user1",
						Role:        "user",
						Permissions: []string{"session:create"},
					},
				},
			},
		},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "invalid-key")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := AuthMiddleware(cfg, nil)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

func TestAuthMiddleware_ValidAPIKey(t *testing.T) {
	// Create config with auth enabled and valid API keys
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Enabled: true,
			Static: &config.StaticAuthConfig{
				Enabled:    true,
				HeaderName: "X-API-Key",
				APIKeys: []config.APIKey{
					{
						Key:         "valid-key",
						UserID:      "user1",
						Role:        "user",
						Permissions: []string{"session:create"},
					},
				},
			},
		},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := AuthMiddleware(cfg, nil)
	handler := func(c echo.Context) error {
		user := GetUserFromContext(c)
		assert.NotNil(t, user)
		assert.Equal(t, "user1", user.UserID)
		assert.Equal(t, "user", user.Role)
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_HasPermission(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with required permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session:create", "session:delete"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:create")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_MissingPermission(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context without required permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session:list"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:create")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

func TestRequirePermission_WildcardPermission(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with wildcard permission
	userCtx := &UserContext{
		UserID:      "admin",
		Role:        "admin",
		Permissions: []string{"*"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:create")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUserOwnsSession_Admin(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set admin user context
	userCtx := &UserContext{
		UserID: "admin",
		Role:   "admin",
	}
	c.Set("user", userCtx)

	// Admin should have access to any session
	assert.True(t, UserOwnsSession(c, "other-user"))
}

func TestUserOwnsSession_OwnSession(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set regular user context
	userCtx := &UserContext{
		UserID: "user1",
		Role:   "user",
	}
	c.Set("user", userCtx)

	// User should have access to their own session
	assert.True(t, UserOwnsSession(c, "user1"))
}

func TestUserOwnsSession_OtherSession(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set regular user context
	userCtx := &UserContext{
		UserID: "user1",
		Role:   "user",
	}
	c.Set("user", userCtx)

	// User should not have access to other user's session
	assert.False(t, UserOwnsSession(c, "user2"))
}

func TestUserOwnsSession_SessionAllList(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:list permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:list"},
	}
	c.Set("user", userCtx)

	// User with session_all:list should have access to any session
	assert.True(t, UserOwnsSession(c, "user2"))
}

func TestUserOwnsSession_SessionAllAccess(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:access permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:access"},
	}
	c.Set("user", userCtx)

	// User with session_all:access should have access to any session
	assert.True(t, UserOwnsSession(c, "user2"))
}

func TestRequirePermission_SessionAllCreate(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:create permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:create"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:create")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_SessionAllCreateWithoutSessionCreate(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:create permission (but not session:create)
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:create"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:create")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_SessionAllList(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:list permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:list"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:list")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_SessionAllDelete(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:delete permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:delete"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:delete")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequirePermission_SessionAllAccess(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user context with session_all:access permission
	userCtx := &UserContext{
		UserID:      "user1",
		Role:        "user",
		Permissions: []string{"session_all:access"},
	}
	c.Set("user", userCtx)

	middleware := RequirePermission("session:access")
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	}

	err := middleware(handler)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}
