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

// --- Mock TaskGroup repository ---

type mockTaskGroupRepository struct {
	groups map[string]*entities.TaskGroup
}

func newMockTaskGroupRepository() *mockTaskGroupRepository {
	return &mockTaskGroupRepository{groups: make(map[string]*entities.TaskGroup)}
}

func (r *mockTaskGroupRepository) Create(_ context.Context, g *entities.TaskGroup) error {
	if _, exists := r.groups[g.ID()]; exists {
		return errors.New("already exists")
	}
	r.groups[g.ID()] = g
	return nil
}

func (r *mockTaskGroupRepository) GetByID(_ context.Context, id string) (*entities.TaskGroup, error) {
	if g, ok := r.groups[id]; ok {
		return g, nil
	}
	return nil, entities.ErrTaskGroupNotFound{ID: id}
}

func (r *mockTaskGroupRepository) List(_ context.Context, filter portrepos.TaskGroupFilter) ([]*entities.TaskGroup, error) {
	var result []*entities.TaskGroup
	for _, g := range r.groups {
		if filter.Scope != "" && g.Scope() != filter.Scope {
			continue
		}
		if filter.OwnerID != "" && g.OwnerID() != filter.OwnerID {
			continue
		}
		if filter.TeamID != "" && g.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 {
			found := false
			for _, tid := range filter.TeamIDs {
				if g.TeamID() == tid {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		result = append(result, g)
	}
	return result, nil
}

func (r *mockTaskGroupRepository) Update(_ context.Context, g *entities.TaskGroup) error {
	if _, exists := r.groups[g.ID()]; !exists {
		return entities.ErrTaskGroupNotFound{ID: g.ID()}
	}
	r.groups[g.ID()] = g
	return nil
}

func (r *mockTaskGroupRepository) Delete(_ context.Context, id string) error {
	if _, exists := r.groups[id]; !exists {
		return entities.ErrTaskGroupNotFound{ID: id}
	}
	delete(r.groups, id)
	return nil
}

// --- Test helpers ---

func makeTaskGroupEchoContext(t *testing.T, method, path string, body interface{}, user *entities.User) (echo.Context, *httptest.ResponseRecorder) {
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

func makeUserTaskGroup(id, name, ownerID string) *entities.TaskGroup {
	return entities.NewTaskGroup(id, name, "", entities.ScopeUser, ownerID, "")
}

func makeTeamTaskGroup(id, name, ownerID, teamID string) *entities.TaskGroup {
	return entities.NewTaskGroup(id, name, "", entities.ScopeTeam, ownerID, teamID)
}

// --- CreateTaskGroup tests ---

func TestCreateTaskGroup_UserScope_Success(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	c, rec := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Name:  "My Group",
		Scope: "user",
	}, user)

	err := controller.CreateTaskGroup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp TaskGroupResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "My Group", resp.Name)
	assert.Equal(t, "user", resp.Scope)
	assert.Equal(t, string(user.ID()), resp.OwnerID)
}

func TestCreateTaskGroup_TeamScope_Success_TeamMember(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	c, rec := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Name:   "Team Group",
		Scope:  "team",
		TeamID: "myorg/backend",
	}, user)

	err := controller.CreateTaskGroup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestCreateTaskGroup_TeamScope_Forbidden_NotMember(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Name:   "Team Group",
		Scope:  "team",
		TeamID: "myorg/backend",
	}, user)

	err := controller.CreateTaskGroup(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestCreateTaskGroup_MissingName_BadRequest(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Scope: "user",
	}, user)

	err := controller.CreateTaskGroup(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateTaskGroup_InvalidScope_BadRequest(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Name:  "Group",
		Scope: "invalid",
	}, user)

	err := controller.CreateTaskGroup(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateTaskGroup_Unauthenticated(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)

	c, _ := makeTaskGroupEchoContext(t, http.MethodPost, "/task-groups", CreateTaskGroupRequest{
		Name:  "Group",
		Scope: "user",
	}, nil)

	err := controller.CreateTaskGroup(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

// --- GetTaskGroup tests ---

func TestGetTaskGroup_Success_Owner(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	group := makeUserTaskGroup("group-1", "My Group", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	c, rec := makeTaskGroupEchoContext(t, http.MethodGet, "/task-groups/group-1", nil, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.GetTaskGroup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskGroupResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "group-1", resp.ID)
	assert.Equal(t, "My Group", resp.Name)
}

func TestGetTaskGroup_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-2")

	group := makeUserTaskGroup("group-1", "My Group", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	c, _ := makeTaskGroupEchoContext(t, http.MethodGet, "/task-groups/group-1", nil, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.GetTaskGroup(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestGetTaskGroup_NotFound(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskGroupEchoContext(t, http.MethodGet, "/task-groups/nonexistent", nil, user)
	c.SetParamNames("groupId")
	c.SetParamValues("nonexistent")

	err := controller.GetTaskGroup(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

// --- ListTaskGroups tests ---

func TestListTaskGroups_UserScope(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	g1 := makeUserTaskGroup("g1", "Group 1", "user-1")
	g2 := makeUserTaskGroup("g2", "Group 2", "user-2") // different owner
	require.NoError(t, repo.Create(context.Background(), g1))
	require.NoError(t, repo.Create(context.Background(), g2))

	c, rec := makeTaskGroupEchoContext(t, http.MethodGet, "/task-groups?scope=user", nil, user)
	c.QueryParams().Set("scope", "user")

	err := controller.ListTaskGroups(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskGroupListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "g1", resp.TaskGroups[0].ID)
}

func TestListTaskGroups_TeamScope(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	g1 := makeTeamTaskGroup("g1", "Backend Group", "user-1", "myorg/backend")
	g2 := makeTeamTaskGroup("g2", "Frontend Group", "user-2", "myorg/frontend")
	require.NoError(t, repo.Create(context.Background(), g1))
	require.NoError(t, repo.Create(context.Background(), g2))

	c, rec := makeTaskGroupEchoContext(t, http.MethodGet, "/task-groups?scope=team&team_id=myorg/backend", nil, user)
	c.QueryParams().Set("scope", "team")
	c.QueryParams().Set("team_id", "myorg/backend")

	err := controller.ListTaskGroups(c)
	require.NoError(t, err)

	var resp TaskGroupListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "g1", resp.TaskGroups[0].ID)
}

// --- UpdateTaskGroup tests ---

func TestUpdateTaskGroup_Name_Success(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	group := makeUserTaskGroup("group-1", "Old Name", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	newName := "New Name"
	c, rec := makeTaskGroupEchoContext(t, http.MethodPut, "/task-groups/group-1", UpdateTaskGroupRequest{
		Name: &newName,
	}, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.UpdateTaskGroup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskGroupResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "New Name", resp.Name)
}

func TestUpdateTaskGroup_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-2")

	group := makeUserTaskGroup("group-1", "My Group", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	newName := "New Name"
	c, _ := makeTaskGroupEchoContext(t, http.MethodPut, "/task-groups/group-1", UpdateTaskGroupRequest{
		Name: &newName,
	}, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.UpdateTaskGroup(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

// --- DeleteTaskGroup tests ---

func TestDeleteTaskGroup_Success(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-1")

	group := makeUserTaskGroup("group-1", "My Group", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	c, rec := makeTaskGroupEchoContext(t, http.MethodDelete, "/task-groups/group-1", nil, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.DeleteTaskGroup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify it's deleted
	_, err = repo.GetByID(context.Background(), "group-1")
	assert.Error(t, err)
}

func TestDeleteTaskGroup_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskGroupRepository()
	controller := NewTaskGroupController(repo)
	user := newTestAPIKeyUser("user-2")

	group := makeUserTaskGroup("group-1", "My Group", "user-1")
	require.NoError(t, repo.Create(context.Background(), group))

	c, _ := makeTaskGroupEchoContext(t, http.MethodDelete, "/task-groups/group-1", nil, user)
	c.SetParamNames("groupId")
	c.SetParamValues("group-1")

	err := controller.DeleteTaskGroup(c)
	assertHTTPError(t, err, http.StatusForbidden)
}
