package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// --- Mock SlackBot repository ---

type mockSlackBotRepository struct {
	bots  map[string]*entities.SlackBot
	errOn string // if set, returns error for this method
}

func newMockSlackBotRepository() *mockSlackBotRepository {
	return &mockSlackBotRepository{bots: make(map[string]*entities.SlackBot)}
}

func (r *mockSlackBotRepository) Create(_ context.Context, bot *entities.SlackBot) error {
	if r.errOn == "Create" {
		return errors.New("storage error")
	}
	r.bots[bot.ID()] = bot
	return nil
}

func (r *mockSlackBotRepository) Get(_ context.Context, id string) (*entities.SlackBot, error) {
	if r.errOn == "Get" {
		return nil, errors.New("storage error")
	}
	bot, ok := r.bots[id]
	if !ok {
		return nil, entities.ErrSlackBotNotFound{ID: id}
	}
	return bot, nil
}

func (r *mockSlackBotRepository) List(_ context.Context, filter portrepos.SlackBotFilter) ([]*entities.SlackBot, error) {
	if r.errOn == "List" {
		return nil, errors.New("storage error")
	}
	var result []*entities.SlackBot
	for _, bot := range r.bots {
		accessible := false
		if bot.Scope() == entities.ScopeTeam && len(filter.TeamIDs) > 0 {
			for _, teamID := range filter.TeamIDs {
				if bot.TeamID() == teamID {
					accessible = true
					break
				}
			}
			if !accessible && filter.UserID != "" && bot.UserID() == filter.UserID {
				accessible = true
			}
		} else {
			if filter.UserID == "" || bot.UserID() == filter.UserID {
				accessible = true
			}
		}
		if !accessible {
			continue
		}
		result = append(result, bot)
	}
	return result, nil
}

func (r *mockSlackBotRepository) Update(_ context.Context, bot *entities.SlackBot) error {
	if r.errOn == "Update" {
		return errors.New("storage error")
	}
	if _, ok := r.bots[bot.ID()]; !ok {
		return entities.ErrSlackBotNotFound{ID: bot.ID()}
	}
	r.bots[bot.ID()] = bot
	return nil
}

func (r *mockSlackBotRepository) Delete(_ context.Context, id string) error {
	if r.errOn == "Delete" {
		return errors.New("storage error")
	}
	if _, ok := r.bots[id]; !ok {
		return entities.ErrSlackBotNotFound{ID: id}
	}
	delete(r.bots, id)
	return nil
}

// --- Test helpers ---

func makeSlackBotEchoContext(t *testing.T, method, path string, body interface{}, userID string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	return makeSlackBotEchoContextWithTeams(t, method, path, body, userID, nil)
}

// makeSlackBotEchoContextWithTeams creates an echo context with user ID and team IDs set in auth context.
func makeSlackBotEchoContextWithTeams(t *testing.T, method, path string, body interface{}, userID string, teamIDs []string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader([]byte("{}"))
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if userID != "" {
		user := entities.NewUser(entities.UserID(userID), entities.UserTypeAPIKey, userID)
		c.Set("internal_user", user)

		authzCtx := &auth.AuthorizationContext{
			User: user,
			PersonalScope: auth.PersonalScopeAuth{
				UserID:    userID,
				CanCreate: true,
				CanRead:   true,
				CanUpdate: true,
				CanDelete: true,
			},
			TeamScope: auth.TeamScopeAuth{
				Teams:           teamIDs,
				TeamPermissions: make(map[string]auth.TeamPermissions),
				IsAdmin:         false,
			},
		}
		for _, tid := range teamIDs {
			authzCtx.TeamScope.TeamPermissions[tid] = auth.TeamPermissions{
				TeamID:    tid,
				CanCreate: true,
				CanRead:   true,
				CanUpdate: true,
				CanDelete: true,
			}
		}
		c.Set("authz_context", authzCtx)
	}
	return c, rec
}

// --- CreateSlackBot tests ---

func TestCreateSlackBot_Success(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "https://example.com", "")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "secret-abc123",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "My Bot", resp.Name)
	assert.Equal(t, "user-1", resp.UserID)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, entities.SlackBotStatusActive, resp.Status)
	// Signing secret must be masked
	assert.Contains(t, resp.SigningSecret, "****")
	assert.NotEqual(t, "secret-abc123", resp.SigningSecret)
	// Hook URL must be set
	assert.Contains(t, resp.HookURL, "/hooks/slack/")
	assert.Contains(t, resp.HookURL, "https://example.com")
}

func TestCreateSlackBot_UsesServerDefaultSigningSecret(t *testing.T) {
	repo := newMockSlackBotRepository()
	// Controller configured with a default signing secret
	controller := NewSlackBotController(repo, "", "default-server-secret")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "Bot Without Secret",
		// No signing_secret in request — should use the server default hook endpoint
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Bot Without Secret", resp.Name)
	assert.Contains(t, resp.SigningSecret, "****")
	// When no signing_secret is provided, the response must point to the "default" hook
	assert.Equal(t, slackBotDefaultID, resp.ID)
	assert.Equal(t, "/hooks/slack/default", resp.HookURL)
	// No new bot entity should be stored in the repository
	assert.Len(t, repo.bots, 0, "default-hook bots are not persisted in the repository")
}

func TestCreateSlackBot_MissingName(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		SigningSecret: "secret",
		// Name is missing
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateSlackBot_MissingSigningSecretAndNoDefault(t *testing.T) {
	repo := newMockSlackBotRepository()
	// No default signing secret configured
	controller := NewSlackBotController(repo, "", "")

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
		// No signing_secret, no server default
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateSlackBot_Unauthenticated(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "secret",
	}, "") // no user_id

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

func TestCreateSlackBot_WithAllOptionalFields(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "https://proxy.example.com", "")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:                "Full Bot",
		SigningSecret:       "supersecret",
		BotTokenSecretName:  "my-k8s-secret",
		BotTokenSecretKey:   "xoxb-token",
		AllowedEventTypes:   []string{"message", "app_mention"},
		AllowedChannelNames: []string{"dev-alerts"},
		MaxSessions:         5,
		SessionConfig: &SlackBotSessionConfig{
			InitialMessageTemplate: "Hello from Slack: {{.event.text}}",
			ReuseMessageTemplate:   "Continue: {{.event.text}}",
			Tags:                   map[string]string{"team": "engineering"},
			Environment:            map[string]string{"LOG_LEVEL": "debug"},
		},
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Full Bot", resp.Name)
	assert.Equal(t, "my-k8s-secret", resp.BotTokenSecretName)
	assert.Equal(t, "xoxb-token", resp.BotTokenSecretKey)
	assert.Equal(t, []string{"message", "app_mention"}, resp.AllowedEventTypes)
	assert.Equal(t, []string{"dev-alerts"}, resp.AllowedChannelNames)
	assert.Equal(t, 5, resp.MaxSessions)
	require.NotNil(t, resp.SessionConfig)
	assert.Equal(t, "Hello from Slack: {{.event.text}}", resp.SessionConfig.InitialMessageTemplate)
	assert.Equal(t, "Continue: {{.event.text}}", resp.SessionConfig.ReuseMessageTemplate)
	assert.Equal(t, "engineering", resp.SessionConfig.Tags["team"])
	assert.Equal(t, "debug", resp.SessionConfig.Environment["LOG_LEVEL"])
}

func TestCreateSlackBot_WithTeamScope(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "Team Bot",
		SigningSecret: "team-secret",
		Scope:         entities.ScopeTeam,
		TeamID:        "myorg/backend",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, string(entities.ScopeTeam), string(resp.Scope))
	assert.Equal(t, "myorg/backend", resp.TeamID)
}

func TestCreateSlackBot_StorageError(t *testing.T) {
	repo := newMockSlackBotRepository()
	repo.errOn = "Create"
	controller := NewSlackBotController(repo, "", "")

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "secret",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusInternalServerError)
}

func TestCreateSlackBot_DefaultMaxSessions(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "secret",
		// MaxSessions not set → should default to 10
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 10, resp.MaxSessions)
}

func TestCreateSlackBot_HookURLWithoutBaseURL(t *testing.T) {
	repo := newMockSlackBotRepository()
	// No base URL configured
	controller := NewSlackBotController(repo, "", "")

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "secret",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Without base URL, hook URL should still contain /hooks/slack/<id>
	assert.Contains(t, resp.HookURL, "/hooks/slack/")
	assert.NotEmpty(t, resp.ID)
	assert.True(t, resp.HookURL == "/hooks/slack/"+resp.ID)
}

func TestCreateSlackBot_RequestSigningSecretTakesPrecedenceOverDefault(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	// Request provides its own signing secret
	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:          "My Bot",
		SigningSecret: "request-secret-xyz",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Masked secret should show last 4 chars of "request-secret-xyz" → "...-xyz"
	assert.Equal(t, "****-xyz", resp.SigningSecret)
}

// --- ListSlackBots team visibility tests ---

// TestListSlackBots_TeamMemberSeesTeamBot verifies that a team member can see
// a team-scoped bot created by another user in the same team.
func TestListSlackBots_TeamMemberSeesTeamBot(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	// user-a creates a team-scoped bot for "myorg/backend"
	botID := "bot-team-123"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
	teamBot.SetSigningSecret("some-secret-value")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	repo.bots[botID] = teamBot

	// user-b is a member of "myorg/backend" and lists bots
	c, rec := makeSlackBotEchoContextWithTeams(t, http.MethodGet, "/slackbots", nil, "user-b", []string{"myorg/backend"})
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 1, "team member should see the team-scoped bot created by another user")
	assert.Equal(t, botID, bots[0].ID)
}

// TestListSlackBots_NonTeamMemberCannotSeeTeamBot verifies that a user NOT in the team
// cannot see the team-scoped bot.
func TestListSlackBots_NonTeamMemberCannotSeeTeamBot(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	botID := "bot-team-456"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
	teamBot.SetSigningSecret("some-secret-value")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	repo.bots[botID] = teamBot

	// user-c is NOT in "myorg/backend"
	c, rec := makeSlackBotEchoContextWithTeams(t, http.MethodGet, "/slackbots", nil, "user-c", []string{"myorg/frontend"})
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 0, "user not in the team should not see the team-scoped bot")
}

// TestListSlackBots_CreatorSeesOwnTeamBot verifies that the creator can see their team bot.
func TestListSlackBots_CreatorSeesOwnTeamBot(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo, "", "default-secret")

	botID := "bot-team-789"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
	teamBot.SetSigningSecret("some-secret-value")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	repo.bots[botID] = teamBot

	// user-a is the creator and is also a member of "myorg/backend"
	c, rec := makeSlackBotEchoContextWithTeams(t, http.MethodGet, "/slackbots", nil, "user-a", []string{"myorg/backend"})
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 1, "creator should see their own team-scoped bot")
}
