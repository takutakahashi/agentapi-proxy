package proxy

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/profile"
)

// ProfileHandlers handles HTTP requests for profile management
type ProfileHandlers struct {
	service *profile.Service
}

// NewProfileHandlers creates a new profile handlers instance
func NewProfileHandlers(service *profile.Service) *ProfileHandlers {
	return &ProfileHandlers{
		service: service,
	}
}

// CreateProfileRequest represents the request body for creating a profile
type CreateProfileRequest struct {
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// UpdateProfileRequest represents the request body for updating a profile
type UpdateProfileRequest struct {
	Username    string                 `json:"username,omitempty"`
	Email       string                 `json:"email,omitempty"`
	DisplayName string                 `json:"display_name,omitempty"`
	Preferences map[string]interface{} `json:"preferences,omitempty"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
}

// SetPreferenceRequest represents the request body for setting a preference
type SetPreferenceRequest struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// SetSettingRequest represents the request body for setting a setting
type SetSettingRequest struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// GetProfile handles GET /profile - get current user's profile
func (h *ProfileHandlers) GetProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userProfile, err := h.service.GetProfile(c.Request().Context(), user.UserID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get profile")
	}

	return c.JSON(http.StatusOK, userProfile)
}

// CreateProfile handles POST /profile - create current user's profile
func (h *ProfileHandlers) CreateProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	profile, err := h.service.CreateProfile(c.Request().Context(), user.UserID, req.Username, req.Email, req.DisplayName)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return echo.NewHTTPError(http.StatusConflict, "Profile already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create profile")
	}

	return c.JSON(http.StatusCreated, profile)
}

// UpdateProfile handles PUT /profile - update current user's profile
func (h *ProfileHandlers) UpdateProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	update := &profile.ProfileUpdate{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Preferences: req.Preferences,
		Settings:    req.Settings,
		Metadata:    req.Metadata,
	}

	updatedProfile, err := h.service.UpdateProfile(c.Request().Context(), user.UserID, update)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update profile")
	}

	return c.JSON(http.StatusOK, updatedProfile)
}

// DeleteProfile handles DELETE /profile - delete current user's profile
func (h *ProfileHandlers) DeleteProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	err := h.service.DeleteProfile(c.Request().Context(), user.UserID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete profile")
	}

	return c.NoContent(http.StatusNoContent)
}

// SetPreference handles POST /profile/preference - set a preference
func (h *ProfileHandlers) SetPreference(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req SetPreferenceRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Preference key is required")
	}

	err := h.service.SetPreference(c.Request().Context(), user.UserID, req.Key, req.Value)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to set preference")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Preference set successfully",
		"key":     req.Key,
		"value":   req.Value,
	})
}

// GetPreference handles GET /profile/preference/:key - get a preference
func (h *ProfileHandlers) GetPreference(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	key := c.Param("key")
	if key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Preference key is required")
	}

	value, err := h.service.GetPreference(c.Request().Context(), user.UserID, key)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Preference not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get preference")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": value,
	})
}

// SetSetting handles POST /profile/setting - set a setting
func (h *ProfileHandlers) SetSetting(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req SetSettingRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Setting key is required")
	}

	err := h.service.SetSetting(c.Request().Context(), user.UserID, req.Key, req.Value)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to set setting")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Setting set successfully",
		"key":     req.Key,
		"value":   req.Value,
	})
}

// GetSetting handles GET /profile/setting/:key - get a setting
func (h *ProfileHandlers) GetSetting(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	key := c.Param("key")
	if key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Setting key is required")
	}

	value, err := h.service.GetSetting(c.Request().Context(), user.UserID, key)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Setting not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get setting")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": value,
	})
}

// Admin endpoints

// ListProfiles handles GET /profiles - list all profiles (admin only)
func (h *ProfileHandlers) ListProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userIDs, err := h.service.ListProfiles(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list profiles")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"profiles": userIDs,
		"count":    len(userIDs),
	})
}

// GetUserProfile handles GET /profiles/:userID - get specific user's profile (admin only)
func (h *ProfileHandlers) GetUserProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	userProfile, err := h.service.GetProfile(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get profile")
	}

	return c.JSON(http.StatusOK, userProfile)
}

// DeleteUserProfile handles DELETE /profiles/:userID - delete specific user's profile (admin only)
func (h *ProfileHandlers) DeleteUserProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	err := h.service.DeleteProfile(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete profile")
	}

	return c.NoContent(http.StatusNoContent)
}
