package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/profile"
)

func TestProfileIntegration(t *testing.T) {
	// Create test configuration
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false // Disable auth for testing

	// Create proxy with test config
	proxy := NewProxy(cfg, false)

	// Create test request context
	e := echo.New()

	t.Run("Create Profile", func(t *testing.T) {
		reqBody := profile.CreateProfileRequest{
			Name:        "Test Profile",
			Description: "A test profile for integration testing",
			Environment: map[string]string{
				"TEST_VAR": "test_value",
				"API_URL":  "https://api.example.com",
			},
			SystemPrompt: "You are a helpful test assistant",
			MessageTemplates: []profile.MessageTemplate{
				{
					Name:     "Greeting",
					Content:  "Hello! How can I help you today?",
					Category: "general",
				},
			},
		}

		reqBodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set mock user in context
		c.Set("user", &auth.UserContext{UserID: "test-user", Role: "user", Permissions: []string{"*"}})

		// Call handler
		err := proxy.createProfile(c)
		if err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}

		// Check response
		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status %d, got %d", http.StatusCreated, rec.Code)
		}

		var response profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.Profile.Name != reqBody.Name {
			t.Errorf("Expected name %s, got %s", reqBody.Name, response.Profile.Name)
		}
		if response.Profile.UserID != "test-user" {
			t.Errorf("Expected user ID 'test-user', got %s", response.Profile.UserID)
		}
		if len(response.Profile.Environment) != 2 {
			t.Errorf("Expected 2 environment variables, got %d", len(response.Profile.Environment))
		}
	})

	t.Run("List Profiles", func(t *testing.T) {
		// First create a couple of profiles
		profiles := []profile.CreateProfileRequest{
			{
				Name:         "Profile 1",
				SystemPrompt: "Assistant 1",
			},
			{
				Name:         "Profile 2",
				SystemPrompt: "Assistant 2",
			},
		}

		for _, profileReq := range profiles {
			reqBodyBytes, _ := json.Marshal(profileReq)
			req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.Set("user", &auth.UserContext{UserID: "test-user-2", Role: "user", Permissions: []string{"*"}})

			if err := proxy.createProfile(c); err != nil {
				t.Fatalf("Failed to create profile: %v", err)
			}
		}

		// Now list profiles
		req := httptest.NewRequest(http.MethodGet, "/profiles", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user", &auth.UserContext{UserID: "test-user-2", Role: "user", Permissions: []string{"*"}})

		err := proxy.listProfiles(c)
		if err != nil {
			t.Fatalf("Failed to list profiles: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var response profile.ProfileListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if len(response.Profiles) != 2 {
			t.Errorf("Expected 2 profiles, got %d", len(response.Profiles))
		}
		if response.Total != 2 {
			t.Errorf("Expected total 2, got %d", response.Total)
		}
	})

	t.Run("Get Profile", func(t *testing.T) {
		// Create a profile first
		reqBody := profile.CreateProfileRequest{
			Name:         "Get Test Profile",
			SystemPrompt: "Get test assistant",
		}

		reqBodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user", &auth.UserContext{UserID: "test-user-3", Role: "user", Permissions: []string{"*"}})

		if err := proxy.createProfile(c); err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}

		var createResponse profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &createResponse); err != nil {
			t.Fatalf("Failed to unmarshal create response: %v", err)
		}
		profileID := createResponse.Profile.ID

		// Now get the profile
		req = httptest.NewRequest(http.MethodGet, "/profiles/"+profileID, nil)
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-3", Role: "user", Permissions: []string{"*"}})

		err := proxy.getProfile(c)
		if err != nil {
			t.Fatalf("Failed to get profile: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var response profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.Profile.ID != profileID {
			t.Errorf("Expected profile ID %s, got %s", profileID, response.Profile.ID)
		}
		if response.Profile.Name != reqBody.Name {
			t.Errorf("Expected name %s, got %s", reqBody.Name, response.Profile.Name)
		}
	})

	t.Run("Update Profile", func(t *testing.T) {
		// Create a profile first
		reqBody := profile.CreateProfileRequest{
			Name:         "Update Test Profile",
			SystemPrompt: "Original prompt",
			Environment: map[string]string{
				"ORIGINAL_VAR": "original_value",
			},
		}

		reqBodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user", &auth.UserContext{UserID: "test-user-4", Role: "user", Permissions: []string{"*"}})

		if err := proxy.createProfile(c); err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}

		var createResponse profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &createResponse); err != nil {
			t.Fatalf("Failed to unmarshal create response: %v", err)
		}
		profileID := createResponse.Profile.ID

		// Now update the profile
		newName := "Updated Profile Name"
		newPrompt := "Updated system prompt"
		updateReq := profile.UpdateProfileRequest{
			Name:         &newName,
			SystemPrompt: &newPrompt,
			Environment: map[string]string{
				"UPDATED_VAR": "updated_value",
				"NEW_VAR":     "new_value",
			},
		}

		updateBodyBytes, _ := json.Marshal(updateReq)
		req = httptest.NewRequest(http.MethodPut, "/profiles/"+profileID, bytes.NewReader(updateBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-4", Role: "user", Permissions: []string{"*"}})

		err := proxy.updateProfile(c)
		if err != nil {
			t.Fatalf("Failed to update profile: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var response profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.Profile.Name != newName {
			t.Errorf("Expected name %s, got %s", newName, response.Profile.Name)
		}
		if response.Profile.SystemPrompt != newPrompt {
			t.Errorf("Expected system prompt %s, got %s", newPrompt, response.Profile.SystemPrompt)
		}
		if len(response.Profile.Environment) != 2 {
			t.Errorf("Expected 2 environment variables, got %d", len(response.Profile.Environment))
		}
	})

	t.Run("Delete Profile", func(t *testing.T) {
		// Create a profile first
		reqBody := profile.CreateProfileRequest{
			Name:         "Delete Test Profile",
			SystemPrompt: "To be deleted",
		}

		reqBodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user", &auth.UserContext{UserID: "test-user-5", Role: "user", Permissions: []string{"*"}})

		if err := proxy.createProfile(c); err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}

		var createResponse profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &createResponse); err != nil {
			t.Fatalf("Failed to unmarshal create response: %v", err)
		}
		profileID := createResponse.Profile.ID

		// Now delete the profile
		req = httptest.NewRequest(http.MethodDelete, "/profiles/"+profileID, nil)
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-5", Role: "user", Permissions: []string{"*"}})

		err := proxy.deleteProfile(c)
		if err != nil {
			t.Fatalf("Failed to delete profile: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		// Verify profile is deleted by trying to get it
		req = httptest.NewRequest(http.MethodGet, "/profiles/"+profileID, nil)
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-5", Role: "user", Permissions: []string{"*"}})

		err = proxy.getProfile(c)
		if err == nil {
			t.Error("Expected error when getting deleted profile")
		}
	})

	t.Run("Add Repository to Profile", func(t *testing.T) {
		// Create a profile first
		reqBody := profile.CreateProfileRequest{
			Name:         "Repo Test Profile",
			SystemPrompt: "Test with repositories",
		}

		reqBodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user", &auth.UserContext{UserID: "test-user-6", Role: "user", Permissions: []string{"*"}})

		if err := proxy.createProfile(c); err != nil {
			t.Fatalf("Failed to create profile: %v", err)
		}

		var createResponse profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &createResponse); err != nil {
			t.Fatalf("Failed to unmarshal create response: %v", err)
		}
		profileID := createResponse.Profile.ID

		// Add repository
		repoEntry := profile.RepositoryEntry{
			URL:    "https://github.com/example/repo",
			Name:   "example-repo",
			Branch: "main",
		}

		repoBodyBytes, _ := json.Marshal(repoEntry)
		req = httptest.NewRequest(http.MethodPost, "/profiles/"+profileID+"/repositories", bytes.NewReader(repoBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-6", Role: "user", Permissions: []string{"*"}})

		err := proxy.addRepositoryToProfile(c)
		if err != nil {
			t.Fatalf("Failed to add repository: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		// Verify repository was added by getting the profile
		req = httptest.NewRequest(http.MethodGet, "/profiles/"+profileID, nil)
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("profileId")
		c.SetParamValues(profileID)
		c.Set("user", &auth.UserContext{UserID: "test-user-6", Role: "user", Permissions: []string{"*"}})

		if err := proxy.getProfile(c); err != nil {
			t.Fatalf("Failed to get profile: %v", err)
		}

		var getResponse profile.ProfileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &getResponse); err != nil {
			t.Fatalf("Failed to unmarshal get response: %v", err)
		}

		if len(getResponse.Profile.RepositoryHistory) != 1 {
			t.Errorf("Expected 1 repository, got %d", len(getResponse.Profile.RepositoryHistory))
		}

		repo := getResponse.Profile.RepositoryHistory[0]
		if repo.URL != repoEntry.URL {
			t.Errorf("Expected URL %s, got %s", repoEntry.URL, repo.URL)
		}
	})
}
