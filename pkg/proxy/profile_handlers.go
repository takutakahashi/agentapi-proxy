package proxy

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/profile"
)

// ProfileHandlers handles HTTP requests for profile management with multiple named profiles
type ProfileHandlers struct {
	service *profile.Service
}

// NewProfileHandlers creates a new profile handlers instance
func NewProfileHandlers(service *profile.Service) *ProfileHandlers {
	return &ProfileHandlers{
		service: service,
	}
}

// Request/Response structures
type CreateUserProfilesRequest struct {
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
}

type CreateProfileRequest struct {
	Name            string                   `json:"name"`
	Description     string                   `json:"description,omitempty"`
	APIEndpoint     string                   `json:"api_endpoint,omitempty"`
	ClaudeJSON      map[string]interface{}   `json:"claude_json,omitempty"`
	PromptTemplates []profile.PromptTemplate `json:"prompt_templates,omitempty"`
	Preferences     map[string]interface{}   `json:"preferences,omitempty"`
	Settings        map[string]interface{}   `json:"settings,omitempty"`
	Metadata        map[string]string        `json:"metadata,omitempty"`
}

type UpdateProfileRequest struct {
	Description     string                   `json:"description,omitempty"`
	APIEndpoint     string                   `json:"api_endpoint,omitempty"`
	ClaudeJSON      map[string]interface{}   `json:"claude_json,omitempty"`
	PromptTemplates []profile.PromptTemplate `json:"prompt_templates,omitempty"`
	Preferences     map[string]interface{}   `json:"preferences,omitempty"`
	Settings        map[string]interface{}   `json:"settings,omitempty"`
	Metadata        map[string]string        `json:"metadata,omitempty"`
}

type UpdateUserProfilesRequest struct {
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
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

// User Profiles Management

// GetUserProfiles handles GET /profiles - get current user's all profiles
func (h *ProfileHandlers) GetUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userProfiles, err := h.service.GetUserProfiles(c.Request().Context(), user.UserID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get user profiles")
	}

	return c.JSON(http.StatusOK, userProfiles)
}

// CreateUserProfiles handles POST /profiles - create user profiles collection
func (h *ProfileHandlers) CreateUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateUserProfilesRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	profileName := req.ProfileName
	if profileName == "" {
		profileName = "default"
	}

	userProfiles, err := h.service.CreateUserProfiles(c.Request().Context(), user.UserID, req.Username, req.Email, req.DisplayName, profileName)
	if err != nil {
		if strings.Contains(err.Error(), "already exist") {
			return echo.NewHTTPError(http.StatusConflict, "User profiles already exist")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create user profiles")
	}

	return c.JSON(http.StatusCreated, userProfiles)
}

// UpdateUserProfiles handles PUT /profiles - update user-level information
func (h *ProfileHandlers) UpdateUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req UpdateUserProfilesRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	update := &profile.UserProfilesUpdate{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
	}

	updatedUserProfiles, err := h.service.UpdateUserProfiles(c.Request().Context(), user.UserID, update)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update user profiles")
	}

	return c.JSON(http.StatusOK, updatedUserProfiles)
}

// DeleteUserProfiles handles DELETE /profiles - delete all user profiles
func (h *ProfileHandlers) DeleteUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	err := h.service.DeleteUserProfiles(c.Request().Context(), user.UserID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete user profiles")
	}

	return c.NoContent(http.StatusNoContent)
}

// Profile Management

// ListProfiles handles GET /profiles/list - list all profile names for current user
func (h *ProfileHandlers) ListProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileNames, err := h.service.ListProfiles(c.Request().Context(), user.UserID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list profiles")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"profiles": profileNames,
		"count":    len(profileNames),
	})
}

// GetProfile handles GET /profiles/:name - get specific profile by name
func (h *ProfileHandlers) GetProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileName := c.Param("name")
	if profileName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile name is required")
	}

	profileConfig, err := h.service.GetProfile(c.Request().Context(), user.UserID, profileName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get profile")
	}

	return c.JSON(http.StatusOK, profileConfig)
}

// GetDefaultProfile handles GET /profiles/default - get default profile
func (h *ProfileHandlers) GetDefaultProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	defaultProfile, err := h.service.GetDefaultProfile(c.Request().Context(), user.UserID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Default profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get default profile")
	}

	return c.JSON(http.StatusOK, defaultProfile)
}

// CreateProfile handles POST /profiles/:name - create new profile
func (h *ProfileHandlers) CreateProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileName := c.Param("name")
	if profileName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile name is required")
	}

	var req CreateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Use name from URL parameter
	req.Name = profileName

	profileConfig, err := h.service.CreateProfile(c.Request().Context(), user.UserID, req.Name)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return echo.NewHTTPError(http.StatusConflict, "Profile already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create profile")
	}

	// Update the profile with provided data
	if req.Description != "" || req.APIEndpoint != "" || req.ClaudeJSON != nil ||
		req.PromptTemplates != nil || req.Preferences != nil || req.Settings != nil || req.Metadata != nil {
		update := &profile.ProfileConfigUpdate{
			Description:     req.Description,
			APIEndpoint:     req.APIEndpoint,
			ClaudeJSON:      req.ClaudeJSON,
			PromptTemplates: req.PromptTemplates,
			Preferences:     req.Preferences,
			Settings:        req.Settings,
			Metadata:        req.Metadata,
		}

		profileConfig, err = h.service.UpdateProfile(c.Request().Context(), user.UserID, req.Name, update)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update profile after creation")
		}
	}

	return c.JSON(http.StatusCreated, profileConfig)
}

// UpdateProfile handles PUT /profiles/:name - update profile by name
func (h *ProfileHandlers) UpdateProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileName := c.Param("name")
	if profileName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile name is required")
	}

	var req UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	update := &profile.ProfileConfigUpdate{
		Description:     req.Description,
		APIEndpoint:     req.APIEndpoint,
		ClaudeJSON:      req.ClaudeJSON,
		PromptTemplates: req.PromptTemplates,
		Preferences:     req.Preferences,
		Settings:        req.Settings,
		Metadata:        req.Metadata,
	}

	updatedProfile, err := h.service.UpdateProfile(c.Request().Context(), user.UserID, profileName, update)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update profile")
	}

	return c.JSON(http.StatusOK, updatedProfile)
}

// DeleteProfile handles DELETE /profiles/:name - delete profile by name
func (h *ProfileHandlers) DeleteProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileName := c.Param("name")
	if profileName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile name is required")
	}

	err := h.service.DeleteProfile(c.Request().Context(), user.UserID, profileName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete profile")
	}

	return c.NoContent(http.StatusNoContent)
}

// SetDefaultProfile handles POST /profiles/:name/default - set profile as default
func (h *ProfileHandlers) SetDefaultProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	profileName := c.Param("name")
	if profileName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile name is required")
	}

	err := h.service.SetDefaultProfile(c.Request().Context(), user.UserID, profileName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to set default profile")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Default profile set successfully",
		"profile": profileName,
	})
}

// Admin endpoints (same as before but adapted for new structure)

// AdminListAllUserProfiles handles GET /admin/profiles - list all user profiles (admin only)
func (h *ProfileHandlers) AdminListAllUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userIDs, err := h.service.ListAllUsers(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list all user profiles")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"users": userIDs,
		"count": len(userIDs),
	})
}

// AdminGetUserProfiles handles GET /admin/profiles/:userID - get specific user's profiles (admin only)
func (h *ProfileHandlers) AdminGetUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	userProfiles, err := h.service.GetUserProfiles(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get user profiles")
	}

	return c.JSON(http.StatusOK, userProfiles)
}

// AdminDeleteUserProfiles handles DELETE /admin/profiles/:userID - delete specific user's profiles (admin only)
func (h *ProfileHandlers) AdminDeleteUserProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	err := h.service.DeleteUserProfiles(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "User profiles not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete user profiles")
	}

	return c.NoContent(http.StatusNoContent)
}

// Backward compatibility methods for old API

// SetPreference handles POST /profile/preference - set a preference (backward compatibility)
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

// GetPreference handles GET /profile/preference/:key - get a preference (backward compatibility)
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

// SetSetting handles POST /profile/setting - set a setting (backward compatibility)
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

// GetSetting handles GET /profile/setting/:key - get a setting (backward compatibility)
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

// GetUserProfile handles GET /profiles/:userID - get specific user's profile (admin only, backward compatibility)
func (h *ProfileHandlers) GetUserProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	userProfiles, err := h.service.GetUserProfiles(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get profile")
	}

	return c.JSON(http.StatusOK, userProfiles)
}

// DeleteUserProfile handles DELETE /profiles/:userID - delete specific user's profile (admin only, backward compatibility)
func (h *ProfileHandlers) DeleteUserProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := c.Param("userID")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "User ID is required")
	}

	err := h.service.DeleteUserProfiles(c.Request().Context(), userID)
	if err != nil {
		if err == profile.ErrProfileNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete profile")
	}

	return c.NoContent(http.StatusNoContent)
}
