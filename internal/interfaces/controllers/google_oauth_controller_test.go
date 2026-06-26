package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateAuthorizationURLProxiesScopeIDsToScia(t *testing.T) {
	var gotScope string
	var gotUserToken string
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
			gotUserToken = r.Header.Get("X-Scia-User-Token")
			var req sciaAuthorizationURLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			gotScope = req.Scope
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
	}, nil, "").WithPersonalAPIKeyRepository(&fakeGoogleOAuthPersonalAPIKeyRepo{
		keys: map[entities.UserID]string{"takutakahashi": "ap-user-token"},
	})

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
	if gotScope != "calendar-write tasks-write" {
		t.Fatalf("scope = %q", gotScope)
	}
	if gotUserToken != "ap-user-token" {
		t.Fatalf("X-Scia-User-Token = %q", gotUserToken)
	}
	var body IntegrationAuthorizationURLResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.AuthorizationURL != "https://accounts.google.com/o/oauth2/v2/auth?scope=calendar" {
		t.Fatalf("authorization_url = %q", body.AuthorizationURL)
	}
}

func TestCreateAuthorizationURLAddsScopeIDsToFallbackStartURL(t *testing.T) {
	var gotScope string
	var gotUserToken string
	scia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/integrations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"integrations": []map[string]any{
					{
						"id":            "takutakahashi.google",
						"provider":      "google",
						"namespace":     "takutakahashi",
						"credential_id": "takutakahashi.google",
						"name":          "Google",
						"released":      true,
						"start_url":     "/oauth/google/start",
						"scopes":        []map[string]any{},
					},
				},
			})
		case "/oauth/google/start":
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			gotScope = r.URL.Query().Get("scope")
			gotUserToken = r.URL.Query().Get("user_token")
			http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?scope=drive", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer scia.Close()

	controller := NewGoogleOAuthController(config.SciaConfig{
		Enabled:          true,
		OAuthInternalURL: scia.URL,
	}, nil, "").WithPersonalAPIKeyRepository(&fakeGoogleOAuthPersonalAPIKeyRepo{
		keys: map[entities.UserID]string{"takutakahashi": "ap-user-token"},
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/integrations/takutakahashi.google/authorization-url", strings.NewReader(`{"scope_ids":["drive-read","gmail-read"],"redirect_uri":"https://app.example.com/oauth/google/callback"}`))
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
	if gotScope != "drive-read gmail-read" {
		t.Fatalf("scope = %q", gotScope)
	}
	if gotUserToken != "ap-user-token" {
		t.Fatalf("user_token = %q", gotUserToken)
	}
}

func TestRevokeIntegrationProxiesToSciaNamespaceRevoke(t *testing.T) {
	var revoked bool
	var gotUserToken string
	scia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/integrations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"integrations": []map[string]any{
					{
						"id":                         "takutakahashi.todoist",
						"provider":                   "todoist",
						"namespace":                  "takutakahashi",
						"credential_id":              "takutakahashi.todoist",
						"name":                       "Todoist",
						"released":                   true,
						"start_url":                  "/oauth/takutakahashi/todoist/start",
						"authorization_url_endpoint": "/oauth/takutakahashi/todoist/authorization-url",
						"scopes":                     []map[string]any{},
					},
				},
			})
		case "/oauth/takutakahashi/todoist/revoke":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			gotUserToken = r.Header.Get("X-Scia-User-Token")
			revoked = true
			_ = json.NewEncoder(w).Encode(IntegrationRevokeResponse{
				Revoked:      true,
				CredentialID: "takutakahashi.todoist",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer scia.Close()

	controller := NewGoogleOAuthController(config.SciaConfig{
		Enabled:          true,
		OAuthInternalURL: scia.URL,
	}, nil, "").WithPersonalAPIKeyRepository(&fakeGoogleOAuthPersonalAPIKeyRepo{
		keys: map[entities.UserID]string{"takutakahashi": "ap-user-token"},
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/integrations/takutakahashi.todoist/revoke", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues("takutakahashi.todoist")
	ctx.Set("internal_user", entities.NewUser("takutakahashi", entities.UserTypeRegular, "takutakahashi"))

	if err := controller.RevokeIntegration(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !revoked {
		t.Fatalf("scia revoke endpoint was not called")
	}
	if gotUserToken != "ap-user-token" {
		t.Fatalf("X-Scia-User-Token = %q", gotUserToken)
	}
	var body IntegrationRevokeResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Revoked || body.CredentialID != "takutakahashi.todoist" {
		t.Fatalf("unexpected revoke response: %#v", body)
	}
}

func TestGetStatusIncludesPersonalAPIKeyUserTokenInOAuthStartURL(t *testing.T) {
	scia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_scia/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer scia.Close()

	controller := NewGoogleOAuthController(config.SciaConfig{
		Enabled:          true,
		OAuthInternalURL: scia.URL,
		PublicBaseURL:    "https://agentapi.example.com",
	}, nil, "").WithPersonalAPIKeyRepository(&fakeGoogleOAuthPersonalAPIKeyRepo{
		keys: map[entities.UserID]string{"Alice_Example": "ap-user-token"},
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/integrations/google-oauth/status", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", entities.NewUser("Alice_Example", entities.UserTypeRegular, "Alice_Example"))

	if err := controller.GetStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body GoogleOAuthStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.UserNamespace != "alice-example" {
		t.Fatalf("user_namespace = %q", body.UserNamespace)
	}
	startURL, err := url.Parse(body.OAuthStartURL)
	if err != nil {
		t.Fatal(err)
	}
	if startURL.Query().Get("user_token") != "ap-user-token" {
		t.Fatalf("user_token = %q", startURL.Query().Get("user_token"))
	}
}

type fakeGoogleOAuthPersonalAPIKeyRepo struct {
	keys map[entities.UserID]string
}

func (r *fakeGoogleOAuthPersonalAPIKeyRepo) FindByUserID(_ context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error) {
	key := r.keys[userID]
	if key == "" {
		return nil, assertAnError{}
	}
	return entities.NewPersonalAPIKey(userID, key), nil
}

func (r *fakeGoogleOAuthPersonalAPIKeyRepo) Save(context.Context, *entities.PersonalAPIKey) error {
	return nil
}

func (r *fakeGoogleOAuthPersonalAPIKeyRepo) Delete(context.Context, entities.UserID) error {
	return nil
}

func (r *fakeGoogleOAuthPersonalAPIKeyRepo) List(context.Context) ([]*entities.PersonalAPIKey, error) {
	return nil, nil
}

type assertAnError struct{}

func (assertAnError) Error() string { return "not found" }

func TestGetIntegrationsMarksConnectedPerCredentialToken(t *testing.T) {
	scia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_scia/healthz":
			w.WriteHeader(http.StatusOK)
		case "/api/integrations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"integrations": []map[string]any{
					{
						"id":                         "alice.google",
						"provider":                   "google",
						"namespace":                  "alice",
						"credential_id":              "alice.google",
						"name":                       "Google",
						"released":                   true,
						"start_url":                  "/oauth/alice/google/start",
						"authorization_url_endpoint": "/oauth/alice/google/authorization-url",
						"scopes":                     []map[string]any{},
					},
					{
						"id":                         "alice.todoist",
						"provider":                   "todoist",
						"namespace":                  "alice",
						"credential_id":              "alice.todoist",
						"name":                       "Todoist",
						"released":                   true,
						"start_url":                  "/oauth/alice/todoist/start",
						"authorization_url_endpoint": "/oauth/alice/todoist/authorization-url",
						"scopes":                     []map[string]any{},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer scia.Close()

	k8s := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "scia-oauth-alice", Namespace: "agentapi"},
		Data: map[string][]byte{
			"refresh_token": []byte("legacy-google-refresh-token"),
		},
	})
	controller := NewGoogleOAuthController(config.SciaConfig{
		Enabled:          true,
		OAuthInternalURL: scia.URL,
	}, k8s, "agentapi")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/integrations", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("internal_user", entities.NewUser("alice", entities.UserTypeRegular, "alice"))

	if err := controller.GetIntegrations(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var body IntegrationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Integrations) != 2 {
		t.Fatalf("integrations = %#v", body.Integrations)
	}
	connected := map[string]bool{}
	for _, integration := range body.Integrations {
		connected[integration.ID] = integration.Connected
	}
	if !connected["alice.google"] {
		t.Fatalf("google connected = false, want true")
	}
	if connected["alice.todoist"] {
		t.Fatalf("todoist connected = true, want false")
	}
}
