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

// --- Mock repository ---

type mockMemoryRepository struct {
	entries map[string]*entities.Memory
}

func newMockMemoryRepository() *mockMemoryRepository {
	return &mockMemoryRepository{entries: make(map[string]*entities.Memory)}
}

func (r *mockMemoryRepository) Create(_ context.Context, m *entities.Memory) error {
	if _, exists := r.entries[m.ID()]; exists {
		return errors.New("already exists")
	}
	r.entries[m.ID()] = m
	return nil
}

func (r *mockMemoryRepository) GetByID(_ context.Context, id string) (*entities.Memory, error) {
	if m, ok := r.entries[id]; ok {
		return m, nil
	}
	return nil, entities.ErrMemoryNotFound{ID: id}
}

func (r *mockMemoryRepository) List(_ context.Context, filter portrepos.MemoryFilter) ([]*entities.Memory, error) {
	var result []*entities.Memory
	for _, m := range r.entries {
		if filter.Scope != "" && m.Scope() != filter.Scope {
			continue
		}
		if filter.OwnerID != "" && m.OwnerID() != filter.OwnerID {
			continue
		}
		if filter.TeamID != "" && m.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 {
			found := false
			for _, tid := range filter.TeamIDs {
				if m.TeamID() == tid {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if !m.MatchesTags(filter.Tags) {
			continue
		}
		if !m.MatchesText(filter.Query) {
			continue
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *mockMemoryRepository) Update(_ context.Context, m *entities.Memory) error {
	if _, exists := r.entries[m.ID()]; !exists {
		return entities.ErrMemoryNotFound{ID: m.ID()}
	}
	r.entries[m.ID()] = m
	return nil
}

func (r *mockMemoryRepository) Delete(_ context.Context, id string) error {
	if _, exists := r.entries[id]; !exists {
		return entities.ErrMemoryNotFound{ID: id}
	}
	delete(r.entries, id)
	return nil
}

// --- Test helpers ---

// assertHTTPError checks that the error is an *echo.HTTPError with the expected status code.
func assertHTTPError(t *testing.T, err error, expectedCode int) {
	t.Helper()
	require.Error(t, err, "expected an HTTP error but got nil")
	var httpErr *echo.HTTPError
	require.True(t, errors.As(err, &httpErr), "expected *echo.HTTPError, got %T: %v", err, err)
	assert.Equal(t, expectedCode, httpErr.Code, "unexpected HTTP status code")
}

func makeUserMemory(id, title, content, ownerID string) *entities.Memory {
	return entities.NewMemory(id, title, content, entities.ScopeUser, ownerID, "")
}

func makeTeamMemory(id, title, content, ownerID, teamID string) *entities.Memory {
	return entities.NewMemory(id, title, content, entities.ScopeTeam, ownerID, teamID)
}

func newTestAPIKeyUser(userID string) *entities.User {
	u := entities.NewUser(entities.UserID(userID), entities.UserTypeAPIKey, userID)
	return u
}

func newTestGitHubUser(userID, org, teamSlug string) *entities.User {
	info := entities.NewGitHubUserInfo(1, userID, userID, "", "", "", "")
	teams := []entities.GitHubTeamMembership{
		{Organization: org, TeamSlug: teamSlug, TeamName: teamSlug, Role: "member"},
	}
	u := entities.NewGitHubUser(entities.UserID(userID), userID, "", info)
	u.SetGitHubInfo(info, teams)
	return u
}

func newTestAdminUser(userID string) *entities.User {
	u := entities.NewUser(entities.UserID(userID), entities.UserTypeAdmin, userID)
	_ = u.SetRoles([]entities.Role{entities.RoleAdmin})
	u.SetPermissions([]entities.Permission{entities.PermissionAdmin})
	return u
}

func makeMemoryEchoContext(t *testing.T, method, path string, body interface{}, user *entities.User) (echo.Context, *httptest.ResponseRecorder) {
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
	if user != nil {
		c.Set("internal_user", user)
	}
	return c, rec
}

// --- CreateMemory tests ---

func TestCreateMemory_UserScope_Success(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	c, rec := makeMemoryEchoContext(t, http.MethodPost, "/memories", CreateMemoryRequest{
		Title:   "Test Note",
		Content: "Some text",
		Scope:   "user",
	}, user)

	err := controller.CreateMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp MemoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Test Note", resp.Title)
	assert.Equal(t, "user", resp.Scope)
	assert.Equal(t, string(user.ID()), resp.OwnerID)
}

func TestCreateMemory_TeamScope_Success_TeamMember(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	c, rec := makeMemoryEchoContext(t, http.MethodPost, "/memories", CreateMemoryRequest{
		Title:  "Team Note",
		Scope:  "team",
		TeamID: "myorg/backend",
	}, user)

	err := controller.CreateMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestCreateMemory_TeamScope_Forbidden_NotMember(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1") // API key user has no GitHub teams

	c, _ := makeMemoryEchoContext(t, http.MethodPost, "/memories", CreateMemoryRequest{
		Title:  "Team Note",
		Scope:  "team",
		TeamID: "myorg/backend",
	}, user)

	err := controller.CreateMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestCreateMemory_Unauthenticated(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)

	c, _ := makeMemoryEchoContext(t, http.MethodPost, "/memories", CreateMemoryRequest{
		Title: "Note",
		Scope: "user",
	}, nil) // no user

	err := controller.CreateMemory(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

func TestCreateMemory_MissingTitle(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeMemoryEchoContext(t, http.MethodPost, "/memories", CreateMemoryRequest{
		Scope: "user",
	}, user)

	err := controller.CreateMemory(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

// --- GetMemory tests ---

func TestGetMemory_UserScope_OwnerAccess_Success(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")
	m := makeUserMemory("mem-1", "My Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	c, rec := makeMemoryEchoContext(t, http.MethodGet, "/memories/mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.GetMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// CRITICAL: Admin cannot access another user's user-scoped memory
func TestGetMemory_UserScope_AdminCannotAccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	admin := newTestAdminUser("admin-user")

	// Entry owned by "user-1", NOT admin
	m := makeUserMemory("mem-1", "Private Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	c, _ := makeMemoryEchoContext(t, http.MethodGet, "/memories/mem-1", nil, admin)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.GetMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestGetMemory_UserScope_OtherUserCannotAccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user2 := newTestAPIKeyUser("user-2")

	m := makeUserMemory("mem-1", "User1 Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	c, _ := makeMemoryEchoContext(t, http.MethodGet, "/memories/mem-1", nil, user2)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.GetMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestGetMemory_TeamScope_MemberAccess_Success(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	m := makeTeamMemory("team-mem-1", "Team Note", "content", "user-1", "myorg/backend")
	_ = repo.Create(context.Background(), m)

	c, rec := makeMemoryEchoContext(t, http.MethodGet, "/memories/team-mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("team-mem-1")

	err := controller.GetMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetMemory_TeamScope_NonMemberCannotAccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("outsider") // Not a GitHub user, no teams

	m := makeTeamMemory("team-mem-1", "Team Note", "content", "user-1", "myorg/backend")
	_ = repo.Create(context.Background(), m)

	c, _ := makeMemoryEchoContext(t, http.MethodGet, "/memories/team-mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("team-mem-1")

	err := controller.GetMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestGetMemory_NotFound(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeMemoryEchoContext(t, http.MethodGet, "/memories/nonexistent", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("nonexistent")

	err := controller.GetMemory(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

// --- ListMemories tests ---

func TestListMemories_NoScope_ReturnsOnlyOwnAndTeamEntries(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	// Own user-scope entries
	_ = repo.Create(context.Background(), makeUserMemory("u1", "My Note", "c", "user-1"))
	// Other user's entries (should NOT be visible)
	_ = repo.Create(context.Background(), makeUserMemory("u2", "Other's Note", "c", "user-2"))
	// Team entry for user's team
	_ = repo.Create(context.Background(), makeTeamMemory("t1", "Team Note", "c", "user-1", "myorg/backend"))
	// Team entry for different team (should NOT be visible)
	_ = repo.Create(context.Background(), makeTeamMemory("t2", "Other Team Note", "c", "user-2", "myorg/other"))

	c, rec := makeMemoryEchoContext(t, http.MethodGet, "/memories", nil, user)

	err := controller.ListMemories(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp MemoryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total) // u1 + t1
}

// Admin should only see their own entries, not all users' entries
func TestListMemories_UserScope_AdminSeesOnlyOwn(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	admin := newTestAdminUser("admin-user")

	_ = repo.Create(context.Background(), makeUserMemory("u1", "Admin's Note", "c", "admin-user"))
	_ = repo.Create(context.Background(), makeUserMemory("u2", "User's Note", "c", "regular-user"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/memories?scope=user", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("internal_user", admin)

	err := controller.ListMemories(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp MemoryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Admin sees only their own entries, NOT all users' entries
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "admin-user", resp.Memories[0].OwnerID)
}

func TestListMemories_TagFilter_Applied(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	m1 := entities.NewMemoryWithTags("tag1", "T1", "c", entities.ScopeUser, "user-1", "", map[string]string{"cat": "a"})
	m2 := entities.NewMemoryWithTags("tag2", "T2", "c", entities.ScopeUser, "user-1", "", map[string]string{"cat": "b"})
	_ = repo.Create(context.Background(), m1)
	_ = repo.Create(context.Background(), m2)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/memories?scope=user&tag.cat=a", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("internal_user", user)

	err := controller.ListMemories(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp MemoryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "tag1", resp.Memories[0].ID)
}

// --- UpdateMemory tests ---

func TestUpdateMemory_OwnerSuccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	m := makeUserMemory("mem-1", "Original", "original content", "user-1")
	_ = repo.Create(context.Background(), m)

	newTitle := "Updated Title"
	newContent := "updated content"
	c, rec := makeMemoryEchoContext(t, http.MethodPut, "/memories/mem-1", UpdateMemoryRequest{
		Title:   &newTitle,
		Content: &newContent,
	}, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.UpdateMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp MemoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Updated Title", resp.Title)
	assert.Equal(t, "updated content", resp.Content)
}

// CRITICAL: Admin cannot update another user's user-scoped memory
func TestUpdateMemory_AdminCannotUpdate(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	admin := newTestAdminUser("admin-user")

	m := makeUserMemory("mem-1", "User's Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	newTitle := "Admin Update"
	c, _ := makeMemoryEchoContext(t, http.MethodPut, "/memories/mem-1", UpdateMemoryRequest{
		Title: &newTitle,
	}, admin)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.UpdateMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestUpdateMemory_PartialUpdate_NilTagsPreserved(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	m := entities.NewMemoryWithTags("mem-1", "Title", "Content", entities.ScopeUser, "user-1", "", map[string]string{"k": "v"})
	_ = repo.Create(context.Background(), m)

	// Update only title, tags field is absent → tags should be preserved
	newTitle := "New Title"
	c, rec := makeMemoryEchoContext(t, http.MethodPut, "/memories/mem-1", UpdateMemoryRequest{
		Title: &newTitle,
		Tags:  nil, // absent → preserve existing
	}, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.UpdateMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp MemoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "New Title", resp.Title)
	// Tags should still be {"k": "v"}
	assert.Equal(t, "v", resp.Tags["k"])
}

// --- DeleteMemory tests ---

func TestDeleteMemory_OwnerSuccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-1")

	m := makeUserMemory("mem-1", "Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	c, rec := makeMemoryEchoContext(t, http.MethodDelete, "/memories/mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.DeleteMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDeleteMemory_TeamMemberSuccess(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestGitHubUser("user-2", "myorg", "backend")

	m := makeTeamMemory("team-mem-1", "Team Note", "content", "user-1", "myorg/backend")
	_ = repo.Create(context.Background(), m)

	c, rec := makeMemoryEchoContext(t, http.MethodDelete, "/memories/team-mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("team-mem-1")

	err := controller.DeleteMemory(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDeleteMemory_NonOwnerForbidden(t *testing.T) {
	repo := newMockMemoryRepository()
	controller := NewMemoryController(repo)
	user := newTestAPIKeyUser("user-2")

	m := makeUserMemory("mem-1", "User1's Note", "content", "user-1")
	_ = repo.Create(context.Background(), m)

	c, _ := makeMemoryEchoContext(t, http.MethodDelete, "/memories/mem-1", nil, user)
	c.SetParamNames("memoryId")
	c.SetParamValues("mem-1")

	err := controller.DeleteMemory(c)
	assertHTTPError(t, err, http.StatusForbidden)
}
