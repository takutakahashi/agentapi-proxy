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
)

// --- Mock SlackBot repository ---

type mockSlackBotRepository struct {
	bots   map[string]*entities.SlackBot
	errOn  string // if set, returns error for this method
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
		if filter.UserID != "" && bot.UserID() != filter.UserID {
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
		// No signing_secret in request — should fall back to server default
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Bot Without Secret", resp.Name)
	assert.Contains(t, resp.SigningSecret, "****")
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
		Name:               "Full Bot",
		SigningSecret:      "supersecret",
		BotTokenSecretName: "my-k8s-secret",
		BotTokenSecretKey:  "xoxb-token",
		AllowedEventTypes:  []string{"message", "app_mention"},
		AllowedChannelIDs:  []string{"C01234567"},
		MaxSessions:        5,
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
	assert.Equal(t, []string{"C01234567"}, resp.AllowedChannelIDs)
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
