package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type createStatusWebhookRepository struct {
	created *entities.Webhook
}

func (r *createStatusWebhookRepository) Create(_ context.Context, webhook *entities.Webhook) error {
	r.created = webhook
	return nil
}

func (r *createStatusWebhookRepository) Get(context.Context, string) (*entities.Webhook, error) {
	return nil, nil
}

func (r *createStatusWebhookRepository) List(context.Context, repositories.WebhookFilter) ([]*entities.Webhook, error) {
	return nil, nil
}

func (r *createStatusWebhookRepository) Update(context.Context, *entities.Webhook) error {
	return nil
}

func (r *createStatusWebhookRepository) Delete(context.Context, string) error {
	return nil
}

func (r *createStatusWebhookRepository) FindByGitHubRepository(context.Context, repositories.GitHubMatcher) ([]*entities.Webhook, error) {
	return nil, nil
}

func (r *createStatusWebhookRepository) RecordDelivery(context.Context, string, *entities.WebhookDeliveryRecord) error {
	return nil
}

func TestCreateWebhook_Paused(t *testing.T) {
	repo := &createStatusWebhookRepository{}
	controller := NewWebhookController(repo)
	body, err := json.Marshal(CreateWebhookRequest{
		Name:   "Paused Webhook",
		Status: entities.WebhookStatusPaused,
		Type:   entities.WebhookTypeCustom,
		Triggers: []TriggerRequest{
			{Name: "all"},
		},
	})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	err = controller.CreateWebhook(e.NewContext(req, rec))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, repo.created)
	assert.Equal(t, entities.WebhookStatusPaused, repo.created.Status())
}

func TestCreateWebhook_InvalidStatus(t *testing.T) {
	controller := NewWebhookController(&createStatusWebhookRepository{})
	body, err := json.Marshal(CreateWebhookRequest{
		Name:   "Invalid Webhook",
		Status: entities.WebhookStatus("invalid"),
		Type:   entities.WebhookTypeCustom,
		Triggers: []TriggerRequest{
			{Name: "all"},
		},
	})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	err = controller.CreateWebhook(e.NewContext(req, rec))
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}
