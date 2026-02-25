package session

import (
	"context"
	"fmt"

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

	// Webhook payload to mount in the session filesystem (optional)
	WebhookPayload []byte

	// Session reuse: when ReuseSession is true and ReuseMatchTags is non-empty,
	// an existing active session matching those tags is sent ReuseMessage instead
	// of creating a new session.
	ReuseSession   bool
	ReuseMatchTags map[string]string
	// ReuseMessage is sent to the reused session. Falls back to InitialMessage when empty.
	ReuseMessage string

	// Session limit: when MaxSessions > 0, launch fails if the number of sessions
	// matching LimitMatchTags already equals or exceeds MaxSessions.
	MaxSessions    int
	LimitMatchTags map[string]string
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
//   - session-reuse check
//   - session-limit enforcement
//   - RunServerRequest construction (Teams is always set via the caller's ResolveTeams call)
type LaunchUseCase struct {
	sessionManager repositories.SessionManager
}

// NewLaunchUseCase creates a new LaunchUseCase.
func NewLaunchUseCase(sessionManager repositories.SessionManager) *LaunchUseCase {
	return &LaunchUseCase{sessionManager: sessionManager}
}

// Launch creates or reuses a session according to the LaunchRequest.
//
// Execution order:
//  1. Try to reuse an existing active session (when ReuseSession is true).
//  2. Check the session limit (when MaxSessions > 0).
//  3. Create a new session.
func (uc *LaunchUseCase) Launch(ctx context.Context, sessionID string, req LaunchRequest) (LaunchResult, error) {
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
	}

	session, err := uc.sessionManager.CreateSession(ctx, sessionID, runReq, req.WebhookPayload)
	if err != nil {
		return LaunchResult{}, err
	}
	return LaunchResult{SessionID: session.ID(), SessionReused: false}, nil
}
