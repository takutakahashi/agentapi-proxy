package proxy

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// UserInfoCacheTTL is the TTL for /user/info API response cache
const UserInfoCacheTTL = 10 * time.Second

// UserHandlers handles user-related endpoints
type UserHandlers struct {
	proxy         *Proxy
	userInfoCache *utils.TTLCache
}

// UserInfoResponse represents the response for /user/info endpoint
type UserInfoResponse struct {
	Username string   `json:"username"`
	Teams    []string `json:"teams"`
}

// NewUserHandlers creates a new UserHandlers instance
func NewUserHandlers(proxy *Proxy) *UserHandlers {
	return &UserHandlers{
		proxy:         proxy,
		userInfoCache: utils.NewTTLCache(UserInfoCacheTTL),
	}
}

// GetUserInfo handles GET /user/info requests
func (h *UserHandlers) GetUserInfo(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	// Use user ID as cache key
	cacheKey := string(user.ID())

	// Check cache first
	if cached, found := h.userInfoCache.Get(cacheKey); found {
		return c.JSON(http.StatusOK, cached.(*UserInfoResponse))
	}

	response := &UserInfoResponse{
		Teams: []string{},
	}

	if githubInfo := user.GitHubInfo(); githubInfo != nil {
		response.Username = githubInfo.Login()
		for _, team := range githubInfo.Teams() {
			teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
			response.Teams = append(response.Teams, teamSlug)
		}
	}

	// Cache the response
	h.userInfoCache.Set(cacheKey, response)

	return c.JSON(http.StatusOK, response)
}
