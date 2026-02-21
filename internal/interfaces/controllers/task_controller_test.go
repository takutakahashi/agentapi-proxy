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

// --- Mock Task repository ---

type mockTaskRepository struct {
	tasks map[string]*entities.Task
}

func newMockTaskRepository() *mockTaskRepository {
	return &mockTaskRepository{tasks: make(map[string]*entities.Task)}
}

func (r *mockTaskRepository) Create(_ context.Context, t *entities.Task) error {
	if _, exists := r.tasks[t.ID()]; exists {
		return errors.New("already exists")
	}
	r.tasks[t.ID()] = t
	return nil
}

func (r *mockTaskRepository) GetByID(_ context.Context, id string) (*entities.Task, error) {
	if t, ok := r.tasks[id]; ok {
		return t, nil
	}
	return nil, entities.ErrTaskNotFound{ID: id}
}

func (r *mockTaskRepository) List(_ context.Context, filter portrepos.TaskFilter) ([]*entities.Task, error) {
	var result []*entities.Task
	for _, t := range r.tasks {
		if filter.Scope != "" && t.Scope() != filter.Scope {
			continue
		}
		if filter.OwnerID != "" && t.OwnerID() != filter.OwnerID {
			continue
		}
		if filter.TeamID != "" && t.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 {
			found := false
			for _, tid := range filter.TeamIDs {
				if t.TeamID() == tid {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if filter.GroupID != "" && t.GroupID() != filter.GroupID {
			continue
		}
		if filter.Status != "" && t.Status() != filter.Status {
			continue
		}
		if filter.TaskType != "" && t.TaskType() != filter.TaskType {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

func (r *mockTaskRepository) Update(_ context.Context, t *entities.Task) error {
	if _, exists := r.tasks[t.ID()]; !exists {
		return entities.ErrTaskNotFound{ID: t.ID()}
	}
	r.tasks[t.ID()] = t
	return nil
}

func (r *mockTaskRepository) Delete(_ context.Context, id string) error {
	if _, exists := r.tasks[id]; !exists {
		return entities.ErrTaskNotFound{ID: id}
	}
	delete(r.tasks, id)
	return nil
}

// --- Test helpers ---

func makeTaskEchoContext(t *testing.T, method, path string, body interface{}, user *entities.User) (echo.Context, *httptest.ResponseRecorder) {
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

func makeUserTask(id, title, ownerID string) *entities.Task {
	return entities.NewTask(id, title, "", entities.TaskStatusTodo, entities.TaskTypeUser, entities.ScopeUser, ownerID, "", "", "", nil)
}

func makeTeamTask(id, title, ownerID, teamID string) *entities.Task {
	return entities.NewTask(id, title, "", entities.TaskStatusTodo, entities.TaskTypeUser, entities.ScopeTeam, ownerID, teamID, "", "", nil)
}

// --- CreateTask tests ---

func TestCreateTask_UserScope_Success(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, rec := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "My Task",
		TaskType: "user",
		Scope:    "user",
	}, user)

	err := controller.CreateTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "My Task", resp.Title)
	assert.Equal(t, "user", resp.Scope)
	assert.Equal(t, "todo", resp.Status)
	assert.Equal(t, "user", resp.TaskType)
	assert.Equal(t, string(user.ID()), resp.OwnerID)
	assert.Empty(t, resp.Links)
}

func TestCreateTask_WithLinks_Success(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, rec := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "Task with links",
		TaskType: "user",
		Scope:    "user",
		Links: []TaskLinkRequest{
			{URL: "https://github.com/example/repo", Title: "Repo"},
			{URL: "https://docs.example.com"},
		},
	}, user)

	err := controller.CreateTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Links, 2)
	assert.Equal(t, "https://github.com/example/repo", resp.Links[0].URL)
	assert.Equal(t, "Repo", resp.Links[0].Title)
	assert.Equal(t, "https://docs.example.com", resp.Links[1].URL)
}

func TestCreateTask_AgentType_Success(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, rec := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:     "Agent Task",
		TaskType:  "agent",
		Scope:     "user",
		SessionID: "session-123",
	}, user)

	err := controller.CreateTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "agent", resp.TaskType)
	assert.Equal(t, "session-123", resp.SessionID)
}

func TestCreateTask_TeamScope_Success_TeamMember(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	c, rec := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "Team Task",
		TaskType: "user",
		Scope:    "team",
		TeamID:   "myorg/backend",
	}, user)

	err := controller.CreateTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestCreateTask_TeamScope_Forbidden_NotMember(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "Team Task",
		TaskType: "user",
		Scope:    "team",
		TeamID:   "myorg/backend",
	}, user)

	err := controller.CreateTask(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestCreateTask_MissingTitle_BadRequest(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		TaskType: "user",
		Scope:    "user",
	}, user)

	err := controller.CreateTask(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateTask_InvalidTaskType_BadRequest(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "Task",
		TaskType: "invalid",
		Scope:    "user",
	}, user)

	err := controller.CreateTask(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateTask_Unauthenticated(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)

	c, _ := makeTaskEchoContext(t, http.MethodPost, "/tasks", CreateTaskRequest{
		Title:    "Task",
		TaskType: "user",
		Scope:    "user",
	}, nil)

	err := controller.CreateTask(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

// --- GetTask tests ---

func TestGetTask_Success_Owner(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	c, rec := makeTaskEchoContext(t, http.MethodGet, "/tasks/task-1", nil, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.GetTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "task-1", resp.ID)
	assert.Equal(t, "My Task", resp.Title)
}

func TestGetTask_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-2")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	c, _ := makeTaskEchoContext(t, http.MethodGet, "/tasks/task-1", nil, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.GetTask(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestGetTask_NotFound(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	c, _ := makeTaskEchoContext(t, http.MethodGet, "/tasks/nonexistent", nil, user)
	c.SetParamNames("taskId")
	c.SetParamValues("nonexistent")

	err := controller.GetTask(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

// --- ListTasks tests ---

func TestListTasks_UserScope(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task1 := makeUserTask("t1", "Task 1", "user-1")
	task2 := makeUserTask("t2", "Task 2", "user-2") // different owner
	require.NoError(t, repo.Create(context.Background(), task1))
	require.NoError(t, repo.Create(context.Background(), task2))

	c, rec := makeTaskEchoContext(t, http.MethodGet, "/tasks?scope=user", nil, user)
	c.QueryParams().Set("scope", "user")

	err := controller.ListTasks(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "t1", resp.Tasks[0].ID)
}

func TestListTasks_TeamScope(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestGitHubUser("user-1", "myorg", "backend")

	task1 := makeTeamTask("t1", "Team Task 1", "user-1", "myorg/backend")
	task2 := makeTeamTask("t2", "Team Task 2", "user-2", "myorg/frontend") // different team
	require.NoError(t, repo.Create(context.Background(), task1))
	require.NoError(t, repo.Create(context.Background(), task2))

	c, rec := makeTaskEchoContext(t, http.MethodGet, "/tasks?scope=team&team_id=myorg/backend", nil, user)
	c.QueryParams().Set("scope", "team")
	c.QueryParams().Set("team_id", "myorg/backend")

	err := controller.ListTasks(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "t1", resp.Tasks[0].ID)
}

func TestListTasks_FilterByStatus(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task1 := makeUserTask("t1", "Todo Task", "user-1")
	task2 := makeUserTask("t2", "Done Task", "user-1")
	task2.SetStatus(entities.TaskStatusDone)
	require.NoError(t, repo.Create(context.Background(), task1))
	require.NoError(t, repo.Create(context.Background(), task2))

	c, rec := makeTaskEchoContext(t, http.MethodGet, "/tasks?scope=user&status=todo", nil, user)
	c.QueryParams().Set("scope", "user")
	c.QueryParams().Set("status", "todo")

	err := controller.ListTasks(c)
	require.NoError(t, err)

	var resp TaskListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "t1", resp.Tasks[0].ID)
}

func TestListTasks_FilterByGroupID(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task1 := makeUserTask("t1", "Task in group", "user-1")
	task1.SetGroupID("group-1")
	task2 := makeUserTask("t2", "Task not in group", "user-1")
	require.NoError(t, repo.Create(context.Background(), task1))
	require.NoError(t, repo.Create(context.Background(), task2))

	c, rec := makeTaskEchoContext(t, http.MethodGet, "/tasks?scope=user&group_id=group-1", nil, user)
	c.QueryParams().Set("scope", "user")
	c.QueryParams().Set("group_id", "group-1")

	err := controller.ListTasks(c)
	require.NoError(t, err)

	var resp TaskListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "t1", resp.Tasks[0].ID)
}

// --- UpdateTask tests ---

func TestUpdateTask_Status_Success(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	doneStatus := "done"
	c, rec := makeTaskEchoContext(t, http.MethodPut, "/tasks/task-1", UpdateTaskRequest{
		Status: &doneStatus,
	}, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.UpdateTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "done", resp.Status)
}

func TestUpdateTask_Links_Replaced(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	newLinks := []TaskLinkRequest{
		{URL: "https://github.com/example/repo", Title: "Repo"},
	}
	c, rec := makeTaskEchoContext(t, http.MethodPut, "/tasks/task-1", UpdateTaskRequest{
		Links: &newLinks,
	}, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.UpdateTask(c)
	require.NoError(t, err)

	var resp TaskResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Links, 1)
	assert.Equal(t, "https://github.com/example/repo", resp.Links[0].URL)
}

func TestUpdateTask_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-2")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	doneStatus := "done"
	c, _ := makeTaskEchoContext(t, http.MethodPut, "/tasks/task-1", UpdateTaskRequest{
		Status: &doneStatus,
	}, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.UpdateTask(c)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestUpdateTask_InvalidStatus_BadRequest(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	invalidStatus := "invalid"
	c, _ := makeTaskEchoContext(t, http.MethodPut, "/tasks/task-1", UpdateTaskRequest{
		Status: &invalidStatus,
	}, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.UpdateTask(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

// --- DeleteTask tests ---

func TestDeleteTask_Success(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-1")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	c, rec := makeTaskEchoContext(t, http.MethodDelete, "/tasks/task-1", nil, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.DeleteTask(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify it's deleted
	_, err = repo.GetByID(context.Background(), "task-1")
	assert.Error(t, err)
}

func TestDeleteTask_Forbidden_DifferentUser(t *testing.T) {
	repo := newMockTaskRepository()
	controller := NewTaskController(repo)
	user := newTestAPIKeyUser("user-2")

	task := makeUserTask("task-1", "My Task", "user-1")
	require.NoError(t, repo.Create(context.Background(), task))

	c, _ := makeTaskEchoContext(t, http.MethodDelete, "/tasks/task-1", nil, user)
	c.SetParamNames("taskId")
	c.SetParamValues("task-1")

	err := controller.DeleteTask(c)
	assertHTTPError(t, err, http.StatusForbidden)
}
