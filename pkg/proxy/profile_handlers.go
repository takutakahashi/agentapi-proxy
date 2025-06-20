package proxy

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/profile"
)

// createProfile handles POST /profiles requests to create a new profile
func (p *Proxy) createProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req profile.CreateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	createdProfile, err := p.profileService.CreateProfile(user.UserID, &req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusCreated, &profile.ProfileResponse{
		Profile: createdProfile,
	})
}

// listProfiles handles GET /profiles requests to list user's profiles
func (p *Proxy) listProfiles(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profiles, err := p.profileService.GetUserProfiles(user.UserID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, &profile.ProfileListResponse{
		Profiles: profilePtrToValue(profiles),
		Total:    len(profiles),
	})
}

// getProfile handles GET /profiles/:profileId requests to get a specific profile
func (p *Proxy) getProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profileID := c.Param("profileId")
	if profileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	profileData, err := p.profileService.GetProfile(profileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}

	// Check if user owns this profile
	if profileData.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only access your own profiles")
	}

	return c.JSON(http.StatusOK, &profile.ProfileResponse{
		Profile: profileData,
	})
}

// updateProfile handles PUT /profiles/:profileId requests to update a profile
func (p *Proxy) updateProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profileID := c.Param("profileId")
	if profileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	// Check if user owns this profile
	existingProfile, err := p.profileService.GetProfile(profileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}
	if existingProfile.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only update your own profiles")
	}

	var req profile.UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	updatedProfile, err := p.profileService.UpdateProfile(profileID, &req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, &profile.ProfileResponse{
		Profile: updatedProfile,
	})
}

// deleteProfile handles DELETE /profiles/:profileId requests to delete a profile
func (p *Proxy) deleteProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profileID := c.Param("profileId")
	if profileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	// Check if user owns this profile
	existingProfile, err := p.profileService.GetProfile(profileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}
	if existingProfile.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only delete your own profiles")
	}

	if err := p.profileService.DeleteProfile(profileID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Profile deleted successfully",
		"profile_id": profileID,
	})
}

// addRepositoryToProfile handles POST /profiles/:profileId/repositories requests
func (p *Proxy) addRepositoryToProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profileID := c.Param("profileId")
	if profileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	// Check if user owns this profile
	existingProfile, err := p.profileService.GetProfile(profileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}
	if existingProfile.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only modify your own profiles")
	}

	var entry profile.RepositoryEntry
	if err := c.Bind(&entry); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if err := p.profileService.AddRepositoryEntry(profileID, entry); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Repository added to profile successfully",
	})
}

// addTemplateToProfile handles POST /profiles/:profileId/templates requests
func (p *Proxy) addTemplateToProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	profileID := c.Param("profileId")
	if profileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	// Check if user owns this profile
	existingProfile, err := p.profileService.GetProfile(profileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}
	if existingProfile.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only modify your own profiles")
	}

	var template profile.MessageTemplate
	if err := c.Bind(&template); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if err := p.profileService.AddMessageTemplate(profileID, template); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Template added to profile successfully",
	})
}

// startSessionWithProfile handles POST /start-with-profile requests to start a session using a profile
func (p *Proxy) startSessionWithProfile(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req profile.StartSessionWithProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.ProfileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Profile ID is required")
	}

	// Get the profile
	profileData, err := p.profileService.GetProfile(req.ProfileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Profile not found")
	}

	// Check if user owns this profile
	if profileData.UserID != user.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "You can only use your own profiles")
	}

	// Merge environment variables from profile and request
	mergedEnv, err := p.profileService.MergeEnvironmentVariables(req.ProfileID, req.Environment)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Update profile last used timestamp
	if err := p.profileService.UpdateLastUsed(req.ProfileID); err != nil {
		// Log error but don't fail the request
		if p.verbose {
			log.Printf("Failed to update profile last used timestamp: %v", err)
		}
	}

	// Create a modified start request with merged environment
	startReq := StartRequest{
		Environment: mergedEnv,
		Tags:        req.Tags,
		Message:     req.Message,
	}

	// Convert to JSON and create a new request
	reqBody, err := json.Marshal(startReq)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session request")
	}

	// Create a new request context with the modified body
	newReq := c.Request().Clone(c.Request().Context())
	newReq.Body = io.NopCloser(strings.NewReader(string(reqBody)))
	newReq.ContentLength = int64(len(reqBody))
	c.SetRequest(newReq)

	// Call the original start session handler
	return p.startAgentAPIServer(c)
}

// profilePtrToValue converts []*Profile to []Profile for JSON response
func profilePtrToValue(profiles []*profile.Profile) []profile.Profile {
	result := make([]profile.Profile, len(profiles))
	for i, p := range profiles {
		result[i] = *p
	}
	return result
}
