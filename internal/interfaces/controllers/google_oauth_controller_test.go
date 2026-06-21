package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestCreateAuthorizationURLProxiesScopeIDsToScia(t *testing.T) {
	var gotScopeIDs []string
	scia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/integrations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"integrations": []map[string]any{
					{
						"id":                         "takutakahashi.google",
						"provider":                   "google",
						"namespace":                  "takutakahashi",
						"credential_id":              "takutakahashi.google",
						"name":                       "Google",
						"released":                   true,
						"start_url":                  "/oauth/takutakahashi/google/start",
						"authorization_url_endpoint": "/oauth/takutakahashi/google/authorization-url",
						"scopes":                     []map[string]any{},
					},
				},
			})
		case "/oauth/takutakahashi/google/authorization-url":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			var req IntegrationAuthorizationURLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			gotScopeIDs = req.ScopeIDs
			if req.RedirectURI != "https://app.example.com/api/oauth/google/callback" {
				t.Fatalf("redirect_uri = %q", req.RedirectURI)
			}
			_ = json.NewEncoder(w).Encode(IntegrationAuthorizationURLResponse{
				CredentialID:     "takutakahashi.google",
				AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?scope=calendar",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer scia.Close()

	controller := NewGoogleOAuthController(config.SciaConfig{
		Enabled:          true,
		OAuthInternalURL: scia.URL,
	}, nil, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/integrations/takutakahashi.google/authorization-url", strings.NewReader(`{"scope_ids":["calendar-write","tasks-write"],"redirect_uri":"https://app.example.com/api/oauth/google/callback"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues("takutakahashi.google")
	ctx.Set("internal_user", entities.NewUser("takutakahashi", entities.UserTypeRegular, "takutakahashi"))

	if err := controller.CreateAuthorizationURL(ctx); err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Join(gotScopeIDs, ",") != "calendar-write,tasks-write" {
		t.Fatalf("scope_ids = %#v", gotScopeIDs)
	}
	var body IntegrationAuthorizationURLResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.AuthorizationURL != "https://accounts.google.com/o/oauth2/v2/auth?scope=calendar" {
		t.Fatalf("authorization_url = %q", body.AuthorizationURL)
	}
}
