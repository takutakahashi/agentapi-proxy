package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

type mockAssetStore struct {
	html string
}

func (m *mockAssetStore) SaveHTML(ctx context.Context, userID string, html string) (*services.Asset, error) {
	_ = ctx
	_ = userID
	m.html = html
	return &services.Asset{ID: "asset-1", URL: "https://example.com/assets/asset-1/index.html"}, nil
}

func TestAssetControllerCreateAssetJSON(t *testing.T) {
	e := echo.New()
	store := &mockAssetStore{}
	controller := NewAssetController(store)

	req := httptest.NewRequest(http.MethodPost, "/assets", strings.NewReader(`{"html":"<h1>Hello</h1>"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("internal_user", entities.NewUser(entities.UserID("user-1"), entities.UserTypeRegular, "user-1"))

	if err := controller.CreateAsset(c); err != nil {
		t.Fatalf("CreateAsset returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if store.html != "<h1>Hello</h1>" {
		t.Fatalf("stored HTML = %q", store.html)
	}

	var resp AssetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.URL == "" {
		t.Fatal("response URL is empty")
	}
}
