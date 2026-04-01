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
		// Apply additional explicit filters (mirrors KubernetesSlackBotRepository.List)
		if filter.Status != "" && bot.Status() != filter.Status {
			continue
		}
		if filter.Scope != "" && bot.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && bot.TeamID() != filter.TeamID {
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

func (r *mockSlackBotRepository) GetTokens(_ context.Context, _ string) (string, string, error) {
	if r.errOn == "GetTokens" {
		return "", "", errors.New("storage error")
	}
	return "", "", nil
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
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
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
}

func TestCreateSlackBot_MissingName(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		// Name is missing
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateSlackBot_Unauthenticated(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
	}, "") // no user_id

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

func TestCreateSlackBot_WithAllOptionalFields(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:                "Full Bot",
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
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:   "Team Bot",
		Scope:  entities.ScopeTeam,
		TeamID: "myorg/backend",
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
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusInternalServerError)
}

func TestCreateSlackBot_DefaultMaxSessions(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
		// MaxSessions not set → should default to 10
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 10, resp.MaxSessions)
}

// --- ListSlackBots team visibility tests ---

// TestListSlackBots_TeamMemberSeesTeamBot verifies that a team member can see
// a team-scoped bot created by another user in the same team.
func TestListSlackBots_TeamMemberSeesTeamBot(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	// user-a creates a team-scoped bot for "myorg/backend"
	botID := "bot-team-123"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
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
	controller := NewSlackBotController(repo)

	botID := "bot-team-456"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
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
	controller := NewSlackBotController(repo)

	botID := "bot-team-789"
	teamBot := entities.NewSlackBot(botID, "Team Bot", "user-a")
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

// --- ListSlackBots query parameter filter tests ---

// makeSlackBotEchoContextWithQuery creates an echo context with query parameters in the URL.
func makeSlackBotEchoContextWithQuery(t *testing.T, path string, userID string, teamIDs []string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, bytes.NewReader([]byte("{}")))
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

// TestListSlackBots_ScopeFilter verifies that the scope query parameter filters results correctly.
func TestListSlackBots_ScopeFilter(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	// Add a user-scoped bot and a team-scoped bot owned by the same user
	userBot := entities.NewSlackBot("bot-user-scope", "User Bot", "user-1")
	userBot.SetScope(entities.ScopeUser)
	repo.bots["bot-user-scope"] = userBot

	teamBot := entities.NewSlackBot("bot-team-scope", "Team Bot", "user-1")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	repo.bots["bot-team-scope"] = teamBot

	// Filter by scope=user — should only return the user-scoped bot
	c, rec := makeSlackBotEchoContextWithQuery(t, "/slackbots?scope=user", "user-1", []string{"myorg/backend"})
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 1, "scope=user should return only user-scoped bots")
	assert.Equal(t, "bot-user-scope", bots[0].ID)
}

// TestListSlackBots_TeamIDFilter verifies that the team_id query parameter filters results correctly.
func TestListSlackBots_TeamIDFilter(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	// Add two team-scoped bots for different teams
	backendBot := entities.NewSlackBot("bot-backend", "Backend Bot", "user-1")
	backendBot.SetScope(entities.ScopeTeam)
	backendBot.SetTeamID("myorg/backend")
	repo.bots["bot-backend"] = backendBot

	frontendBot := entities.NewSlackBot("bot-frontend", "Frontend Bot", "user-1")
	frontendBot.SetScope(entities.ScopeTeam)
	frontendBot.SetTeamID("myorg/frontend")
	repo.bots["bot-frontend"] = frontendBot

	// Filter by team_id=myorg/backend — should only return the backend bot
	c, rec := makeSlackBotEchoContextWithQuery(t, "/slackbots?team_id=myorg%2Fbackend", "user-1", []string{"myorg/backend", "myorg/frontend"})
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 1, "team_id filter should return only bots for that team")
	assert.Equal(t, "bot-backend", bots[0].ID)
}

// TestListSlackBots_StatusFilter verifies that the status query parameter filters results correctly.
func TestListSlackBots_StatusFilter(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	activeBot := entities.NewSlackBot("bot-active", "Active Bot", "user-1")
	activeBot.SetStatus(entities.SlackBotStatusActive)
	repo.bots["bot-active"] = activeBot

	pausedBot := entities.NewSlackBot("bot-paused", "Paused Bot", "user-1")
	pausedBot.SetStatus(entities.SlackBotStatusPaused)
	repo.bots["bot-paused"] = pausedBot

	// Filter by status=paused — should only return the paused bot
	c, rec := makeSlackBotEchoContextWithQuery(t, "/slackbots?status=paused", "user-1", nil)
	err := controller.ListSlackBots(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var bots []SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bots))
	assert.Len(t, bots, 1, "status filter should return only paused bots")
	assert.Equal(t, "bot-paused", bots[0].ID)
}

// --- UpdateSlackBot tests ---

func TestUpdateSlackBot_ClearBotTokenSecretName(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	// Create a bot with a custom bot_token_secret_name
	bot := entities.NewSlackBot("bot-1", "Test Bot", "user-1")
	bot.SetBotTokenSecretName("my-custom-secret")
	bot.SetBotTokenSecretKey("my-token-key")
	repo.bots["bot-1"] = bot

	// Update: clear bot_token_secret_name by passing empty string ""
	emptyStr := ""
	c, rec := makeSlackBotEchoContext(t, http.MethodPut, "/slackbots/bot-1", UpdateSlackBotRequest{
		BotTokenSecretName: &emptyStr,
		BotTokenSecretKey:  &emptyStr,
	}, "user-1")
	c.SetParamNames("id")
	c.SetParamValues("bot-1")

	err := controller.UpdateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// BotTokenSecretName should be cleared (empty means use global default)
	assert.Empty(t, resp.BotTokenSecretName, "bot_token_secret_name should be cleared")
	// BotTokenSecretKey returns "bot-token" as default even when cleared;
	// the important thing is BotTokenSecretName is empty so the global default is used.

	// Verify stored entity is cleared too
	stored, err := repo.Get(context.Background(), "bot-1")
	require.NoError(t, err)
	assert.Empty(t, stored.BotTokenSecretName(), "stored bot_token_secret_name should be empty")
}

func TestUpdateSlackBot_NilBotTokenSecretNameDoesNotClear(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	// Create a bot with a custom bot_token_secret_name
	bot := entities.NewSlackBot("bot-2", "Test Bot", "user-1")
	bot.SetBotTokenSecretName("my-custom-secret")
	repo.bots["bot-2"] = bot

	// Update: do not include bot_token_secret_name at all (nil = not provided)
	c, rec := makeSlackBotEchoContext(t, http.MethodPut, "/slackbots/bot-2", UpdateSlackBotRequest{
		Name: "Updated Name",
	}, "user-1")
	c.SetParamNames("id")
	c.SetParamValues("bot-2")

	err := controller.UpdateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// BotTokenSecretName should be preserved (not cleared)
	assert.Equal(t, "my-custom-secret", resp.BotTokenSecretName, "bot_token_secret_name should be preserved when not provided")
	assert.Equal(t, "Updated Name", resp.Name)
}
