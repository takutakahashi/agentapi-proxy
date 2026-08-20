package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type managedFilesManagerStub struct{ session entities.Session }

func (m managedFilesManagerStub) ValidateSessionControlToken(id, token string) bool {
	return id == "session-1" && token == "valid"
}
func (m managedFilesManagerStub) GetSession(string) entities.Session { return m.session }

type managedFilesStoreStub struct {
	owner string
	files []sessionsettings.ManagedFile
}

func (s *managedFilesStoreStub) SaveFiles(_ context.Context, owner string, files []sessionsettings.ManagedFile) error {
	s.owner, s.files = owner, files
	return nil
}

func TestManagedFilesControllerSaveUsesSessionOwner(t *testing.T) {
	session := entities.NewProxySession("session-1", "user-1", entities.ScopeUser, "", nil, time.Now())
	store := &managedFilesStoreStub{}
	controller := NewManagedFilesController(managedFilesManagerStub{session: session}, store)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"files":[{"path":"/home/agentapi/.codex/auth.json","content":"updated"}]}`))
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/internal/session-control/:sessionId/managed-files")
	c.SetParamNames("sessionId")
	c.SetParamValues("session-1")

	require.NoError(t, controller.Save(c))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "user-1", store.owner)
	require.Len(t, store.files, 1)
}

func TestManagedFilesControllerRejectsInvalidTokenAndPath(t *testing.T) {
	session := entities.NewProxySession("session-1", "user-1", entities.ScopeUser, "", nil, time.Now())
	controller := NewManagedFilesController(managedFilesManagerStub{session: session}, &managedFilesStoreStub{})
	for _, tc := range []struct {
		token, body string
		want        int
	}{
		{"bad", `{"files":[]}`, http.StatusUnauthorized},
		{"valid", `{"files":[{"path":"/etc/shadow","content":"x"}]}`, http.StatusBadRequest},
	} {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("sessionId")
		c.SetParamValues("session-1")
		require.NoError(t, controller.Save(c))
		require.Equal(t, tc.want, rec.Code)
	}
}
