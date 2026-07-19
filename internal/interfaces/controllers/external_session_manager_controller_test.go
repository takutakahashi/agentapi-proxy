package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func esmTestContext(e *echo.Echo, method, path string, body interface{}, userID string) (echo.Context, *httptest.ResponseRecorder) {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath(path)
	ctx.Set("internal_user", createTestUser(userID, false))
	return ctx, rec
}

func TestExternalSessionManagerRegistrationIsIdempotentAndHeartbeatUsesToken(t *testing.T) {
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer probe.Close()

	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil, "", "")
	e := echo.New()
	body := ESMRegistrationRequest{InstanceID: "machine-1", Name: "native-1", PublicURL: probe.URL,
		Labels: map[string]string{"os": "linux", "arch": "amd64"}}
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers", body, "user1")
	require.NoError(t, controller.RegisterExternalSessionManager(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	var created esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.True(t, created.Created)
	require.NotEmpty(t, created.ConnectionToken)

	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers", body, "user1")
	require.NoError(t, controller.RegisterExternalSessionManager(ctx))
	var repeated esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &repeated))
	require.False(t, repeated.Created)
	require.Equal(t, created.ID, repeated.ID)
	require.Empty(t, repeated.ConnectionToken)
	require.Len(t, repo.settings["user1"].ExternalSessionManagers(), 1)

	heartbeat := ESMHeartbeatRequest{PublicURL: probe.URL, Version: "test-version", ActiveSessions: 2}
	ctx, rec = esmTestContext(e, http.MethodPost, "/external-session-managers/:id/heartbeat", heartbeat, "")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	ctx.Request().Header.Set("Authorization", "Bearer "+created.ConnectionToken)
	require.NoError(t, controller.HeartbeatExternalSessionManager(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	manager := repo.settings["user1"].ExternalSessionManagers()[0]
	require.False(t, manager.LastHeartbeatAt.IsZero())
	require.Equal(t, "test-version", manager.Version)
	require.Equal(t, 2, manager.ActiveSessions)
}

func TestExternalSessionManagerHeartbeatRejectsUnreachablePublicURL(t *testing.T) {
	repo := newMockSettingsRepository()
	controller := NewSettingsController(repo, nil, "", "")
	e := echo.New()
	body := ESMRegistrationRequest{InstanceID: "machine-2", Name: "native-2"}
	ctx, rec := esmTestContext(e, http.MethodPost, "/external-session-managers", body, "user1")
	require.NoError(t, controller.RegisterExternalSessionManager(ctx))
	var created esmRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	heartbeat := ESMHeartbeatRequest{PublicURL: "http://127.0.0.1:1"}
	ctx, _ = esmTestContext(e, http.MethodPost, "/external-session-managers/:id/heartbeat", heartbeat, "")
	ctx.SetParamNames("id")
	ctx.SetParamValues(created.ID)
	ctx.Request().Header.Set("Authorization", "Bearer "+created.ConnectionToken)
	err := controller.HeartbeatExternalSessionManager(ctx)
	require.Error(t, err)
}
