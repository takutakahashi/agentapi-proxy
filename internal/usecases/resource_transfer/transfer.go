package resource_transfer

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type ResourceType string

const (
	ResourceMemory         ResourceType = "memory"
	ResourceTask           ResourceType = "task"
	ResourceTaskGroup      ResourceType = "task_group"
	ResourceWebhook        ResourceType = "webhook"
	ResourceSlackBot       ResourceType = "slackbot"
	ResourceSessionProfile ResourceType = "session_profile"
	ResourceSandboxPolicy  ResourceType = "sandbox_policy"
)

type Request struct {
	ResourceType ResourceType
	ResourceID   string
	TargetScope  entities.ResourceScope
	TargetUserID string
	TargetTeamID string
	DryRun       bool
	Actor        *entities.User
}

type Endpoint struct {
	Scope  entities.ResourceScope `json:"scope"`
	UserID string                 `json:"user_id,omitempty"`
	TeamID string                 `json:"team_id,omitempty"`
}

type Result struct {
	ResourceType ResourceType `json:"resource_type"`
	ResourceID   string       `json:"resource_id"`
	From         Endpoint     `json:"from"`
	To           Endpoint     `json:"to"`
	Status       string       `json:"status"`
	DryRun       bool         `json:"dry_run"`
	Warnings     []string     `json:"warnings,omitempty"`
}

type UseCase struct {
	memoryRepo         portrepos.MemoryRepository
	taskRepo           portrepos.TaskRepository
	taskGroupRepo      portrepos.TaskGroupRepository
	webhookRepo        portrepos.WebhookRepository
	slackBotRepo       portrepos.SlackBotRepository
	sessionProfileRepo portrepos.SessionProfileRepository
	sandboxPolicyRepo  portrepos.SandboxPolicyRepository
}

type Option func(*UseCase)

func New(opts ...Option) *UseCase {
	uc := &UseCase{}
	for _, opt := range opts {
		opt(uc)
	}
	return uc
}

func WithMemoryRepository(repo portrepos.MemoryRepository) Option {
	return func(uc *UseCase) { uc.memoryRepo = repo }
}

func WithTaskRepository(repo portrepos.TaskRepository) Option {
	return func(uc *UseCase) { uc.taskRepo = repo }
}

func WithTaskGroupRepository(repo portrepos.TaskGroupRepository) Option {
	return func(uc *UseCase) { uc.taskGroupRepo = repo }
}

func WithWebhookRepository(repo portrepos.WebhookRepository) Option {
	return func(uc *UseCase) { uc.webhookRepo = repo }
}

func WithSlackBotRepository(repo portrepos.SlackBotRepository) Option {
	return func(uc *UseCase) { uc.slackBotRepo = repo }
}

func WithSessionProfileRepository(repo portrepos.SessionProfileRepository) Option {
	return func(uc *UseCase) { uc.sessionProfileRepo = repo }
}

func WithSandboxPolicyRepository(repo portrepos.SandboxPolicyRepository) Option {
	return func(uc *UseCase) { uc.sandboxPolicyRepo = repo }
}

func (uc *UseCase) Transfer(ctx context.Context, req Request) (Result, error) {
	if req.Actor == nil {
		return Result{}, fmt.Errorf("authentication required")
	}
	if req.ResourceID == "" {
		return Result{}, fmt.Errorf("resource_id is required")
	}
	if req.TargetScope == "" {
		req.TargetScope = entities.ScopeUser
	}
	targetUserID := req.TargetUserID
	if req.TargetScope == entities.ScopeUser && targetUserID == "" {
		targetUserID = string(req.Actor.ID())
	}
	if req.TargetScope == entities.ScopeTeam && targetUserID == "" {
		targetUserID = string(req.Actor.ID())
	}
	if err := authorizeTarget(req.Actor, req.TargetScope, targetUserID, req.TargetTeamID); err != nil {
		return Result{}, err
	}

	switch req.ResourceType {
	case ResourceMemory:
		return uc.transferMemory(ctx, req, targetUserID)
	case ResourceTask:
		return uc.transferTask(ctx, req, targetUserID)
	case ResourceTaskGroup:
		return uc.transferTaskGroup(ctx, req, targetUserID)
	case ResourceWebhook:
		return uc.transferWebhook(ctx, req, targetUserID)
	case ResourceSlackBot:
		return uc.transferSlackBot(ctx, req, targetUserID)
	case ResourceSessionProfile:
		return uc.transferSessionProfile(ctx, req, targetUserID)
	case ResourceSandboxPolicy:
		return uc.transferSandboxPolicy(ctx, req, targetUserID)
	default:
		return Result{}, fmt.Errorf("unsupported resource_type: %s", req.ResourceType)
	}
}

func authorizeTarget(actor *entities.User, scope entities.ResourceScope, userID, teamID string) error {
	switch scope {
	case entities.ScopeUser:
		if userID == "" {
			return fmt.Errorf("target_user_id is required when target_scope is user")
		}
		if !actor.IsAdmin() && userID != string(actor.ID()) {
			return fmt.Errorf("cannot transfer to another user")
		}
	case entities.ScopeTeam:
		if teamID == "" {
			return fmt.Errorf("target_team_id is required when target_scope is team")
		}
		if !actor.IsAdmin() && !actor.IsMemberOfTeam(teamID) {
			return fmt.Errorf("you are not a member of target team")
		}
	default:
		return fmt.Errorf("target_scope must be user or team")
	}
	return nil
}

func canModify(actor *entities.User, ownerID string, scope entities.ResourceScope, teamID string) bool {
	if actor.IsAdmin() {
		return true
	}
	if scope == entities.ScopeTeam {
		return actor.IsMemberOfTeam(teamID)
	}
	return ownerID == string(actor.ID())
}

func endpoint(scope entities.ResourceScope, ownerID, teamID string) Endpoint {
	if scope == "" {
		scope = entities.ScopeUser
	}
	if scope == entities.ScopeTeam {
		return Endpoint{Scope: scope, TeamID: teamID}
	}
	return Endpoint{Scope: scope, UserID: ownerID}
}

func result(req Request, from Endpoint, targetUserID string) Result {
	to := endpoint(req.TargetScope, targetUserID, req.TargetTeamID)
	status := "transferred"
	if req.DryRun {
		status = "dry_run"
	}
	return Result{
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		From:         from,
		To:           to,
		Status:       status,
		DryRun:       req.DryRun,
	}
}

func (uc *UseCase) transferMemory(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.memoryRepo == nil {
		return Result{}, fmt.Errorf("memory repository is not configured")
	}
	m, err := uc.memoryRepo.GetByID(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, m.OwnerID(), m.Scope(), m.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(m.Scope(), m.OwnerID(), m.TeamID()), targetUserID)
	if req.DryRun {
		return res, nil
	}
	m.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.memoryRepo.Update(ctx, m); err != nil {
		return Result{}, err
	}
	updated, err := uc.memoryRepo.GetByID(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !sameOwnership(updated.Scope(), updated.OwnerID(), updated.TeamID(), req.TargetScope, targetUserID, req.TargetTeamID) {
		return Result{}, fmt.Errorf("memory ownership transfer was not persisted")
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func sameOwnership(actualScope entities.ResourceScope, actualOwnerID, actualTeamID string, targetScope entities.ResourceScope, targetOwnerID, targetTeamID string) bool {
	if actualScope != targetScope {
		return false
	}
	if targetScope == entities.ScopeTeam {
		return actualTeamID == targetTeamID
	}
	return actualOwnerID == targetOwnerID
}

func (uc *UseCase) transferTask(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.taskRepo == nil {
		return Result{}, fmt.Errorf("task repository is not configured")
	}
	t, err := uc.taskRepo.GetByID(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, t.OwnerID(), t.Scope(), t.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(t.Scope(), t.OwnerID(), t.TeamID()), targetUserID)
	if t.GroupID() != "" {
		res.Warnings = append(res.Warnings, "task group ownership is not changed automatically")
	}
	if req.DryRun {
		return res, nil
	}
	t.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.taskRepo.Update(ctx, t); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func (uc *UseCase) transferTaskGroup(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.taskGroupRepo == nil {
		return Result{}, fmt.Errorf("task group repository is not configured")
	}
	g, err := uc.taskGroupRepo.GetByID(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, g.OwnerID(), g.Scope(), g.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(g.Scope(), g.OwnerID(), g.TeamID()), targetUserID)
	res.Warnings = append(res.Warnings, "tasks in this group are not transferred automatically")
	if req.DryRun {
		return res, nil
	}
	g.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.taskGroupRepo.Update(ctx, g); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func (uc *UseCase) transferWebhook(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.webhookRepo == nil {
		return Result{}, fmt.Errorf("webhook repository is not configured")
	}
	w, err := uc.webhookRepo.Get(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, w.UserID(), w.Scope(), w.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(w.Scope(), w.UserID(), w.TeamID()), targetUserID)
	res.Warnings = append(res.Warnings, "resources referenced by this webhook are not transferred automatically")
	if req.DryRun {
		return res, nil
	}
	w.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.webhookRepo.Update(ctx, w); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func (uc *UseCase) transferSlackBot(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.slackBotRepo == nil {
		return Result{}, fmt.Errorf("slackbot repository is not configured")
	}
	b, err := uc.slackBotRepo.Get(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, b.UserID(), b.Scope(), b.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(b.Scope(), b.UserID(), b.TeamID()), targetUserID)
	res.Warnings = append(res.Warnings, "resources referenced by this slackbot are not transferred automatically")
	if req.DryRun {
		return res, nil
	}
	b.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.slackBotRepo.Update(ctx, b); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func (uc *UseCase) transferSessionProfile(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.sessionProfileRepo == nil {
		return Result{}, fmt.Errorf("session profile repository is not configured")
	}
	p, err := uc.sessionProfileRepo.Get(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, p.UserID(), p.Scope(), p.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(p.Scope(), p.UserID(), p.TeamID()), targetUserID)
	if req.DryRun {
		return res, nil
	}
	p.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.sessionProfileRepo.Update(ctx, p); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}

func (uc *UseCase) transferSandboxPolicy(ctx context.Context, req Request, targetUserID string) (Result, error) {
	if uc.sandboxPolicyRepo == nil {
		return Result{}, fmt.Errorf("sandbox policy repository is not configured")
	}
	p, err := uc.sandboxPolicyRepo.GetByID(ctx, req.ResourceID)
	if err != nil {
		return Result{}, err
	}
	if !canModify(req.Actor, p.OwnerID(), p.Scope(), p.TeamID()) {
		return Result{}, fmt.Errorf("access denied")
	}
	res := result(req, endpoint(p.Scope(), p.OwnerID(), p.TeamID()), targetUserID)
	res.Warnings = append(res.Warnings, "resources referencing this sandbox policy are not transferred automatically")
	if req.DryRun {
		return res, nil
	}
	p.SetOwnership(req.TargetScope, targetUserID, req.TargetTeamID)
	if err := uc.sandboxPolicyRepo.Update(ctx, p); err != nil {
		return Result{}, err
	}
	log.Printf("[RESOURCE_TRANSFER] actor=%s type=%s id=%s from=%+v to=%+v", req.Actor.ID(), req.ResourceType, req.ResourceID, res.From, res.To)
	return res, nil
}
