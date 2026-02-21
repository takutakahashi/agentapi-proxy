package entities

import (
	"errors"
	"time"
)

// ErrTaskNotFound is returned when a task is not found
type ErrTaskNotFound struct {
	ID string
}

func (e ErrTaskNotFound) Error() string {
	return "task not found: " + e.ID
}

// ErrTaskGroupNotFound is returned when a task group is not found
type ErrTaskGroupNotFound struct {
	ID string
}

func (e ErrTaskGroupNotFound) Error() string {
	return "task group not found: " + e.ID
}

// TaskStatus represents the current status of a task
type TaskStatus string

const (
	// TaskStatusTodo indicates the task has not been started
	TaskStatusTodo TaskStatus = "todo"
	// TaskStatusDone indicates the task has been completed
	TaskStatusDone TaskStatus = "done"
)

// TaskType represents who manages/owns the task
type TaskType string

const (
	// TaskTypeUser indicates a task managed by a human user
	TaskTypeUser TaskType = "user"
	// TaskTypeAgent indicates a task created and managed by an AI agent (session)
	TaskTypeAgent TaskType = "agent"
)

// TaskLink represents a URL associated with a task
type TaskLink struct {
	id    string
	url   string
	title string
}

// NewTaskLink creates a new TaskLink
func NewTaskLink(id, url, title string) *TaskLink {
	return &TaskLink{
		id:    id,
		url:   url,
		title: title,
	}
}

// ID returns the link ID
func (l *TaskLink) ID() string { return l.id }

// URL returns the link URL
func (l *TaskLink) URL() string { return l.url }

// Title returns the link title (optional label)
func (l *TaskLink) Title() string { return l.title }

// Task represents a task belonging to a user or team
type Task struct {
	id          string
	title       string
	description string
	status      TaskStatus
	taskType    TaskType
	scope       ResourceScope
	ownerID     string
	teamID      string
	groupID     string
	sessionID   string
	links       []*TaskLink
	createdAt   time.Time
	updatedAt   time.Time
}

// NewTask creates a new Task with the given fields.
// id should be a UUID string.
func NewTask(id, title, description string, status TaskStatus, taskType TaskType, scope ResourceScope, ownerID, teamID, groupID, sessionID string, links []*TaskLink) *Task {
	now := time.Now()
	if links == nil {
		links = []*TaskLink{}
	}
	return &Task{
		id:          id,
		title:       title,
		description: description,
		status:      status,
		taskType:    taskType,
		scope:       scope,
		ownerID:     ownerID,
		teamID:      teamID,
		groupID:     groupID,
		sessionID:   sessionID,
		links:       links,
		createdAt:   now,
		updatedAt:   now,
	}
}

// ID returns the task ID
func (t *Task) ID() string { return t.id }

// Title returns the task title
func (t *Task) Title() string { return t.title }

// Description returns the task description
func (t *Task) Description() string { return t.description }

// Status returns the task status
func (t *Task) Status() TaskStatus { return t.status }

// TaskType returns the task type (user or agent)
func (t *Task) TaskType() TaskType { return t.taskType }

// Scope returns the resource scope (user or team)
func (t *Task) Scope() ResourceScope { return t.scope }

// OwnerID returns the user ID of the owner
func (t *Task) OwnerID() string { return t.ownerID }

// TeamID returns the team ID (populated only when scope == ScopeTeam)
func (t *Task) TeamID() string { return t.teamID }

// GroupID returns the group ID (empty if not part of a group)
func (t *Task) GroupID() string { return t.groupID }

// SessionID returns the session ID (for agent tasks)
func (t *Task) SessionID() string { return t.sessionID }

// Links returns a copy of the task links
func (t *Task) Links() []*TaskLink {
	result := make([]*TaskLink, len(t.links))
	copy(result, t.links)
	return result
}

// CreatedAt returns the creation timestamp
func (t *Task) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt returns the last update timestamp
func (t *Task) UpdatedAt() time.Time { return t.updatedAt }

// SetTitle sets the title and updates the updatedAt timestamp
func (t *Task) SetTitle(title string) {
	t.title = title
	t.updatedAt = time.Now()
}

// SetDescription sets the description and updates the updatedAt timestamp
func (t *Task) SetDescription(description string) {
	t.description = description
	t.updatedAt = time.Now()
}

// SetStatus sets the status and updates the updatedAt timestamp
func (t *Task) SetStatus(status TaskStatus) {
	t.status = status
	t.updatedAt = time.Now()
}

// SetGroupID sets the group ID and updates the updatedAt timestamp
func (t *Task) SetGroupID(groupID string) {
	t.groupID = groupID
	t.updatedAt = time.Now()
}

// SetSessionID sets the session ID and updates the updatedAt timestamp
func (t *Task) SetSessionID(sessionID string) {
	t.sessionID = sessionID
	t.updatedAt = time.Now()
}

// SetLinks replaces all links and updates the updatedAt timestamp
func (t *Task) SetLinks(links []*TaskLink) {
	if links == nil {
		t.links = []*TaskLink{}
	} else {
		t.links = make([]*TaskLink, len(links))
		copy(t.links, links)
	}
	t.updatedAt = time.Now()
}

// SetCreatedAt sets the createdAt field (for deserialization only)
func (t *Task) SetCreatedAt(ts time.Time) { t.createdAt = ts }

// SetUpdatedAt sets the updatedAt field (for deserialization only)
func (t *Task) SetUpdatedAt(ts time.Time) { t.updatedAt = ts }

// Validate returns an error if the task is in an invalid state
func (t *Task) Validate() error {
	if t.id == "" {
		return errors.New("task id is required")
	}
	if t.title == "" {
		return errors.New("task title is required")
	}
	if t.ownerID == "" {
		return errors.New("task owner_id is required")
	}
	if t.status != TaskStatusTodo && t.status != TaskStatusDone {
		return errors.New("task status must be 'todo' or 'done'")
	}
	if t.taskType != TaskTypeUser && t.taskType != TaskTypeAgent {
		return errors.New("task type must be 'user' or 'agent'")
	}
	if t.scope != ScopeUser && t.scope != ScopeTeam {
		return errors.New("task scope must be 'user' or 'team'")
	}
	if t.scope == ScopeTeam && t.teamID == "" {
		return errors.New("task team_id is required when scope is 'team'")
	}
	return nil
}

// TaskGroup represents a group of tasks
type TaskGroup struct {
	id          string
	name        string
	description string
	scope       ResourceScope
	ownerID     string
	teamID      string
	createdAt   time.Time
	updatedAt   time.Time
}

// NewTaskGroup creates a new TaskGroup with the given fields.
// id should be a UUID string.
func NewTaskGroup(id, name, description string, scope ResourceScope, ownerID, teamID string) *TaskGroup {
	now := time.Now()
	return &TaskGroup{
		id:          id,
		name:        name,
		description: description,
		scope:       scope,
		ownerID:     ownerID,
		teamID:      teamID,
		createdAt:   now,
		updatedAt:   now,
	}
}

// ID returns the group ID
func (g *TaskGroup) ID() string { return g.id }

// Name returns the group name
func (g *TaskGroup) Name() string { return g.name }

// Description returns the group description
func (g *TaskGroup) Description() string { return g.description }

// Scope returns the resource scope (user or team)
func (g *TaskGroup) Scope() ResourceScope { return g.scope }

// OwnerID returns the user ID of the owner
func (g *TaskGroup) OwnerID() string { return g.ownerID }

// TeamID returns the team ID (populated only when scope == ScopeTeam)
func (g *TaskGroup) TeamID() string { return g.teamID }

// CreatedAt returns the creation timestamp
func (g *TaskGroup) CreatedAt() time.Time { return g.createdAt }

// UpdatedAt returns the last update timestamp
func (g *TaskGroup) UpdatedAt() time.Time { return g.updatedAt }

// SetName sets the name and updates the updatedAt timestamp
func (g *TaskGroup) SetName(name string) {
	g.name = name
	g.updatedAt = time.Now()
}

// SetDescription sets the description and updates the updatedAt timestamp
func (g *TaskGroup) SetDescription(description string) {
	g.description = description
	g.updatedAt = time.Now()
}

// SetCreatedAt sets the createdAt field (for deserialization only)
func (g *TaskGroup) SetCreatedAt(ts time.Time) { g.createdAt = ts }

// SetUpdatedAt sets the updatedAt field (for deserialization only)
func (g *TaskGroup) SetUpdatedAt(ts time.Time) { g.updatedAt = ts }

// Validate returns an error if the task group is in an invalid state
func (g *TaskGroup) Validate() error {
	if g.id == "" {
		return errors.New("task group id is required")
	}
	if g.name == "" {
		return errors.New("task group name is required")
	}
	if g.ownerID == "" {
		return errors.New("task group owner_id is required")
	}
	if g.scope != ScopeUser && g.scope != ScopeTeam {
		return errors.New("task group scope must be 'user' or 'team'")
	}
	if g.scope == ScopeTeam && g.teamID == "" {
		return errors.New("task group team_id is required when scope is 'team'")
	}
	return nil
}
