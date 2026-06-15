package session

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// LaunchRequest contains all parameters needed to create a session from any external
// trigger source (schedule, webhook, slackbot, etc.).
// Callers should use ResolveTeams() to populate the Teams field correctly.
type LaunchRequest struct {
	// Identity
	UserID string
	Scope  entities.ResourceScope
	TeamID string
	// Teams is the list of GitHub team slugs for settings injection.
	// MUST be populated via ResolveTeams() — never leave empty for team-scoped sessions.
	Teams []string

	// Session configuration
	Environment              map[string]string
	Tags                     map[string]string
	InitialMessage           string
	GithubToken              string
	AgentType                string
	SlackParams              *entities.SlackParams
	Oneshot                  bool
	RepoInfo                 *entities.RepositoryInfo
	InitialMessageWaitSecond *int
	Sandbox                  *entities.SandboxParams
	Docker                   *entities.DockerParams
	CycleMessage             string
	CycleMaxCount            int
	SessionTTL               string

	// Webhook payload to mount in the session filesystem (optional)
	WebhookPayload []byte

	// MemoryKey is an optional tag map used to identify memories for this session.
	// When non-empty, memories matching these tags are injected into CLAUDE.md at startup.
	// When empty, memory integration is disabled.
	MemoryKey map[string]string

	// Session reuse: when ReuseSession is true and ReuseMatchTags is non-empty,
	// an existing active session matching those tags is sent ReuseMessage instead
	// of creating a new session.
	ReuseSession   bool
	ReuseMatchTags map[string]string
	// ReuseMessage is sent to the reused session. Falls back to InitialMessage when empty.
	ReuseMessage string
	// StopBeforeReuse stops the running agent before sending ReuseMessage.
	// This keeps the session resources in place while making a busy agent receive follow-up input.
	StopBeforeReuse bool

	// Session limit: when MaxSessions > 0, launch fails if the number of sessions
	// matching LimitMatchTags already equals or exceeds MaxSessions.
	MaxSessions    int
	LimitMatchTags map[string]string

	// SessionProfileID is an optional reference to a SessionProfile.
	// When set, the profile's config is merged as a base; explicit request fields override it.
	SessionProfileID string
}

// LaunchResult is returned by LaunchUseCase.Launch.
type LaunchResult struct {
	SessionID     string
	SessionReused bool
}

// ResolveTeams returns the GitHub team slugs to inject into a session's settings.
//
// The rule mirrors what session_controller.go does for direct API requests:
//   - team-scoped: exactly the designated team's settings apply → [teamID]
//   - user-scoped: the user's full team membership list applies → userTeams
//
// Every trigger source (schedule, webhook, slackbot) must call this function when
// building a LaunchRequest so that team-level settings (Bedrock, MCP servers, etc.)
// are always injected correctly.
func ResolveTeams(scope entities.ResourceScope, teamID string, userTeams []string) []string {
	if scope == entities.ScopeTeam && teamID != "" {
		return []string{teamID}
	}
	return userTeams
}

// LaunchUseCase creates sessions from external triggers.
// It centralises the concerns shared by all trigger sources:
//   - session profile resolution
//   - session-reuse check
//   - session-limit enforcement
//   - RunServerRequest construction (Teams is always set via the caller's ResolveTeams call)
//   - auto-creation of missing memories when MemoryKey is set (requires memoryRepo)
type LaunchUseCase struct {
	sessionManager     repositories.SessionManager
	memoryRepo         repositories.MemoryRepository         // optional; if set, auto-creates missing memories
	sessionProfileRepo repositories.SessionProfileRepository // optional; if set, resolves profile configs
}

// NewLaunchUseCase creates a new LaunchUseCase.
func NewLaunchUseCase(sessionManager repositories.SessionManager) *LaunchUseCase {
	return &LaunchUseCase{sessionManager: sessionManager}
}

// WithMemoryRepository configures the memory repository used to auto-create memory entries
// when a session is launched with a MemoryKey that has no matching memory.
// Calling this is optional; if not called, memory auto-creation is disabled.
func (uc *LaunchUseCase) WithMemoryRepository(repo repositories.MemoryRepository) *LaunchUseCase {
	uc.memoryRepo = repo
	return uc
}

// WithSessionProfileRepository configures the session profile repository used to resolve
// profile configs when a SessionProfileID is present in the LaunchRequest.
// Calling this is optional; if not called, profile resolution is disabled.
func (uc *LaunchUseCase) WithSessionProfileRepository(repo repositories.SessionProfileRepository) *LaunchUseCase {
	uc.sessionProfileRepo = repo
	return uc
}

// Launch creates or reuses a session according to the LaunchRequest.
//
// Execution order:
//  0. Resolve session profile config (explicit ID, or default profile when ID is empty).
//  1. Try to reuse an existing active session (when ReuseSession is true).
//  2. Check the session limit (when MaxSessions > 0).
//  3. Create a new session.
func (uc *LaunchUseCase) Launch(ctx context.Context, sessionID string, req LaunchRequest) (LaunchResult, error) {
	// 0. Resolve session profile: merge profile config as base; explicit request fields override.
	// When SessionProfileID is empty, fall back to the default profile for the user/team.
	if uc.sessionProfileRepo != nil {
		profile := uc.resolveSessionProfile(ctx, req)
		if profile != nil {
			applyProfileToLaunchRequest(profile.Config(), &req)
		}
	}

	// 1. Try session reuse
	if req.ReuseSession && len(req.ReuseMatchTags) > 0 {
		filter := entities.SessionFilter{
			Tags:   req.ReuseMatchTags,
			Status: "active",
		}
		if existing := uc.sessionManager.ListSessions(filter); len(existing) > 0 {
			reuseMessage := req.ReuseMessage
			if reuseMessage == "" {
				reuseMessage = req.InitialMessage
			}
			if req.StopBeforeReuse {
				if err := uc.sessionManager.StopAgent(ctx, existing[0].ID()); err != nil {
					return LaunchResult{}, fmt.Errorf("failed to stop existing session before reuse: %w", err)
				}
			}
			if err := uc.sessionManager.SendMessage(ctx, existing[0].ID(), reuseMessage); err != nil {
				return LaunchResult{}, fmt.Errorf("failed to route message to existing session: %w", err)
			}
			return LaunchResult{SessionID: existing[0].ID(), SessionReused: true}, nil
		}
	}

	// 2. Check session limit
	if req.MaxSessions > 0 {
		filter := entities.SessionFilter{Tags: req.LimitMatchTags}
		if existing := uc.sessionManager.ListSessions(filter); len(existing) >= req.MaxSessions {
			return LaunchResult{}, fmt.Errorf("session limit reached: maximum %d sessions", req.MaxSessions)
		}
	}

	// 2.5. Auto-create memory entry if MemoryKey is set and no matching memory exists.
	if uc.memoryRepo != nil && len(req.MemoryKey) > 0 {
		if err := uc.ensureMemoryExists(ctx, req); err != nil {
			// Non-fatal: log and continue so session creation is not blocked.
			log.Printf("[LAUNCH] Warning: failed to auto-create memory: %v", err)
		}
	}

	// 3. Build RunServerRequest and create the session.
	// Teams is provided by the caller via ResolveTeams() so it is always set correctly.
	runReq := &entities.RunServerRequest{
		UserID:                   req.UserID,
		Environment:              req.Environment,
		Tags:                     req.Tags,
		Scope:                    req.Scope,
		TeamID:                   req.TeamID,
		Teams:                    req.Teams,
		InitialMessage:           req.InitialMessage,
		GithubToken:              req.GithubToken,
		AgentType:                req.AgentType,
		SlackParams:              req.SlackParams,
		Oneshot:                  req.Oneshot,
		RepoInfo:                 req.RepoInfo,
		InitialMessageWaitSecond: req.InitialMessageWaitSecond,
		MemoryKey:                req.MemoryKey,
		CycleMessage:             req.CycleMessage,
		CycleMaxCount:            req.CycleMaxCount,
		Sandbox:                  req.Sandbox,
		Docker:                   req.Docker,
		SessionTTL:               req.SessionTTL,
	}

	session, err := uc.sessionManager.CreateSession(ctx, sessionID, runReq, req.WebhookPayload)
	if err != nil {
		return LaunchResult{}, err
	}
	return LaunchResult{SessionID: session.ID(), SessionReused: false}, nil
}

// ensureMemoryExists checks whether a memory with all tags in req.MemoryKey already exists
// for the session owner. If none is found, it creates a new empty memory entry with those tags.
func (uc *LaunchUseCase) ensureMemoryExists(ctx context.Context, req LaunchRequest) error {
	scope := req.Scope
	if scope == "" {
		scope = entities.ScopeUser
	}

	filter := repositories.MemoryFilter{
		Scope:   scope,
		OwnerID: req.UserID,
		Tags:    req.MemoryKey,
	}
	if scope == entities.ScopeTeam {
		filter.TeamID = req.TeamID
	}

	existing, err := uc.memoryRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to list memories: %w", err)
	}
	if len(existing) > 0 {
		// At least one memory already has all the required tags — nothing to do.
		return nil
	}

	// Build a human-readable title from the memory key tags.
	keys := make([]string, 0, len(req.MemoryKey))
	for k := range req.MemoryKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, req.MemoryKey[k]))
	}
	title := "Auto-created: " + strings.Join(parts, ", ")

	memoryID := uuid.New().String()
	mem := entities.NewMemoryWithTags(memoryID, title, "", scope, req.UserID, req.TeamID, req.MemoryKey)
	if err := uc.memoryRepo.Create(ctx, mem); err != nil {
		return fmt.Errorf("failed to create memory: %w", err)
	}
	log.Printf("[LAUNCH] Auto-created memory %s (title=%q) with tags %v for user %s", memoryID, title, req.MemoryKey, req.UserID)
	return nil
}

// resolveSessionProfile returns the session profile to apply for the given request.
// If SessionProfileID is set, it fetches that profile directly.
// Otherwise it searches for a selector_tags match before falling back to the default profile.
func (uc *LaunchUseCase) resolveSessionProfile(ctx context.Context, req LaunchRequest) *entities.SessionProfile {
	if req.SessionProfileID != "" {
		profile, err := uc.sessionProfileRepo.Get(ctx, req.SessionProfileID)
		if err != nil {
			log.Printf("[LAUNCH] Warning: could not resolve session_profile_id %q: %v", req.SessionProfileID, err)
			return nil
		}
		return profile
	}

	scope := req.Scope
	if scope == "" {
		scope = entities.ScopeUser
	}
	filter := repositories.SessionProfileFilter{
		UserID: req.UserID,
		Scope:  scope,
	}
	if scope == entities.ScopeTeam {
		filter.TeamID = req.TeamID
	}
	profiles, err := uc.sessionProfileRepo.List(ctx, filter)
	if err != nil {
		log.Printf("[LAUNCH] Warning: could not list session profiles for default lookup: %v", err)
		return nil
	}
	if profile := selectProfileByTags(profiles, req.Tags); profile != nil {
		log.Printf("[LAUNCH] Applying tag-selected session profile %q (%s) for user %s", profile.ID(), profile.Name(), req.UserID)
		return profile
	}
	for _, p := range profiles {
		if p.IsDefault() {
			log.Printf("[LAUNCH] Applying default session profile %q (%s) for user %s", p.ID(), p.Name(), req.UserID)
			return p
		}
	}
	return nil
}

func selectProfileByTags(profiles []*entities.SessionProfile, tags map[string]string) *entities.SessionProfile {
	var matches []*entities.SessionProfile
	for _, p := range profiles {
		if p.MatchesSelectorTags(tags) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].SelectorSpecificity() != matches[j].SelectorSpecificity() {
			return matches[i].SelectorSpecificity() > matches[j].SelectorSpecificity()
		}
		if matches[i].IsDefault() != matches[j].IsDefault() {
			return matches[i].IsDefault()
		}
		if matches[i].Name() != matches[j].Name() {
			return matches[i].Name() < matches[j].Name()
		}
		return matches[i].ID() < matches[j].ID()
	})
	return matches[0]
}

// applyProfileToLaunchRequest merges a SessionProfileConfig into a LaunchRequest.
// The profile provides the base; explicit request fields override.
func applyProfileToLaunchRequest(cfg entities.SessionProfileConfig, req *LaunchRequest) {
	// Environment: profile is base, request overrides key-by-key
	if len(cfg.Environment()) > 0 {
		merged := make(map[string]string, len(cfg.Environment()))
		for k, v := range cfg.Environment() {
			merged[k] = v
		}
		for k, v := range req.Environment {
			merged[k] = v
		}
		req.Environment = merged
	}
	// Tags: profile is base, request overrides key-by-key
	if len(cfg.Tags()) > 0 {
		merged := make(map[string]string, len(cfg.Tags()))
		for k, v := range cfg.Tags() {
			merged[k] = v
		}
		for k, v := range req.Tags {
			merged[k] = v
		}
		req.Tags = merged
	}
	// MemoryKey: profile is base, request overrides key-by-key
	if len(cfg.MemoryKey()) > 0 {
		merged := make(map[string]string, len(cfg.MemoryKey()))
		for k, v := range cfg.MemoryKey() {
			merged[k] = v
		}
		for k, v := range req.MemoryKey {
			merged[k] = v
		}
		req.MemoryKey = merged
	}
	// Params: profile fills in empty request fields
	if cfg.Params() != nil {
		if req.AgentType == "" {
			req.AgentType = cfg.Params().AgentType
		}
		if req.GithubToken == "" {
			req.GithubToken = cfg.Params().GithubToken
		}
		if req.InitialMessage == "" && cfg.Params().Message != "" {
			req.InitialMessage = cfg.Params().Message
		}
		if req.Sandbox == nil && cfg.Params().Sandbox != nil {
			req.Sandbox = cfg.Params().Sandbox
		}
		if req.Docker == nil && cfg.Params().Docker != nil {
			req.Docker = cfg.Params().Docker
		}
		if req.InitialMessageWaitSecond == nil && cfg.Params().InitialMessageWaitSecond != nil {
			req.InitialMessageWaitSecond = cfg.Params().InitialMessageWaitSecond
		}
		if req.CycleMessage == "" {
			req.CycleMessage = cfg.Params().CycleMessage
		}
		if req.CycleMaxCount == 0 {
			req.CycleMaxCount = cfg.Params().CycleMaxCount
		}
		if req.SessionTTL == "" {
			req.SessionTTL = cfg.Params().SessionTTL
		}
	}
	if cfg.SessionTTL() != "" && req.SessionTTL == "" {
		req.SessionTTL = cfg.SessionTTL()
	}
}
