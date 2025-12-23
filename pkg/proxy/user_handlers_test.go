package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestGetUserInfo_CacheControlHeader(t *testing.T) {
	e := echo.New()

	h := &UserHandlers{}

	// Create user with GitHub info
	user := entities.NewUser(
		entities.UserID("test-user"),
		entities.UserTypeGitHub,
		"test-user",
	)
	info := entities.NewGitHubUserInfo(123, "testuser", "Test User", "test@example.com", "", "", "")
	user.SetGitHubInfo(info, []entities.GitHubTeamMembership{
		{Organization: "org1", TeamSlug: "team1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/user/info", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("internal_user", user)

	err := h.GetUserInfo(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "max-age=30", rec.Header().Get("Cache-Control"))
}

func TestGetUserInfo_Unauthorized(t *testing.T) {
	e := echo.New()

	h := &UserHandlers{}

	req := httptest.NewRequest(http.MethodGet, "/user/info", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// No user set in context

	err := h.GetUserInfo(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}
