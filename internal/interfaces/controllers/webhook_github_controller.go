package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WebhookGitHubController handles GitHub webhook reception
type WebhookGitHubController struct {
	repo                repositories.WebhookRepository
	sessionService      *WebhookSessionService
	signatureVerifier   *webhook.SignatureVerifier
	gotemplateEvaluator *webhook.GoTemplateEvaluator
}

// NewWebhookGitHubController creates a new GitHub webhook controller
func NewWebhookGitHubController(repo repositories.WebhookRepository, sessionManager repositories.SessionManager) *WebhookGitHubController {
	return &WebhookGitHubController{
		repo:                repo,
		sessionService:      NewWebhookSessionService(repo, sessionManager),
		signatureVerifier:   webhook.NewSignatureVerifier(),
		gotemplateEvaluator: webhook.NewGoTemplateEvaluator(),
	}
}

// GetName returns the name of this controller for logging
func (c *WebhookGitHubController) GetName() string {
	return "WebhookGitHubController"
}

// GitHubPayload represents relevant fields from a GitHub webhook payload
type GitHubPayload struct {
	Action     string            `json:"action,omitempty"`
	Ref        string            `json:"ref,omitempty"`
	Repository *GitHubRepository `json:"repository,omitempty"`
	Sender     *GitHubUser       `json:"sender,omitempty"`

	// Pull request specific
	PullRequest *GitHubPullRequest `json:"pull_request,omitempty"`

	// Issue specific
	Issue *GitHubIssue `json:"issue,omitempty"`

	// Push specific
	Commits    []GitHubCommit `json:"commits,omitempty"`
	HeadCommit *GitHubCommit  `json:"head_commit,omitempty"`

	// Raw payload for template rendering
	Raw map[string]interface{} `json:"-"`
}

// GitHubRepository represents a GitHub repository
type GitHubRepository struct {
	FullName      string      `json:"full_name"`
	Name          string      `json:"name"`
	Owner         *GitHubUser `json:"owner,omitempty"`
	DefaultBranch string      `json:"default_branch,omitempty"`
	HTMLURL       string      `json:"html_url,omitempty"`
	CloneURL      string      `json:"clone_url,omitempty"`
}

// GitHubUser represents a GitHub user
type GitHubUser struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatar_url,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
}

// GitHubPullRequest represents a GitHub pull request
type GitHubPullRequest struct {
	Number   int           `json:"number"`
	Title    string        `json:"title"`
	Body     string        `json:"body,omitempty"`
	State    string        `json:"state"`
	Draft    bool          `json:"draft"`
	HTMLURL  string        `json:"html_url"`
	User     *GitHubUser   `json:"user,omitempty"`
	Head     *GitHubRef    `json:"head,omitempty"`
	Base     *GitHubRef    `json:"base,omitempty"`
	Labels   []GitHubLabel `json:"labels,omitempty"`
	Merged   bool          `json:"merged"`
	MergedAt string        `json:"merged_at,omitempty"`
}

// GitHubRef represents a git reference
type GitHubRef struct {
	Ref  string            `json:"ref"`
	SHA  string            `json:"sha"`
	Repo *GitHubRepository `json:"repo,omitempty"`
}

// GitHubIssue represents a GitHub issue
type GitHubIssue struct {
	Number  int           `json:"number"`
	Title   string        `json:"title"`
	Body    string        `json:"body,omitempty"`
	State   string        `json:"state"`
	HTMLURL string        `json:"html_url"`
	User    *GitHubUser   `json:"user,omitempty"`
	Labels  []GitHubLabel `json:"labels,omitempty"`
}

// GitHubLabel represents a GitHub label
type GitHubLabel struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// GitHubCommit represents a GitHub commit
type GitHubCommit struct {
	ID       string              `json:"id"`
	Message  string              `json:"message"`
	Author   *GitHubCommitAuthor `json:"author,omitempty"`
	Added    []string            `json:"added,omitempty"`
	Removed  []string            `json:"removed,omitempty"`
	Modified []string            `json:"modified,omitempty"`
}

// GitHubCommitAuthor represents a commit author
type GitHubCommitAuthor struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
}

// HandleGitHubWebhook handles POST /hooks/github/:id
func (c *WebhookGitHubController) HandleGitHubWebhook(ctx echo.Context) error {
	webhookID := ctx.Param("id")
	if webhookID == "" {
		log.Printf("[WEBHOOK] Missing webhook ID in URL path")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Missing webhook ID"})
	}

	// Read the raw body for signature verification
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to read request body: %v", err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to read request body"})
	}
	ctx.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	// Extract GitHub headers
	event := ctx.Request().Header.Get("X-GitHub-Event")
	deliveryID := ctx.Request().Header.Get("X-GitHub-Delivery")
	signature := ctx.Request().Header.Get("X-Hub-Signature-256")

	if event == "" {
		log.Printf("[WEBHOOK] Missing X-GitHub-Event header")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Missing required header"})
	}

	log.Printf("[WEBHOOK] Received GitHub webhook: webhook_id=%s, event=%s, delivery=%s", webhookID, event, deliveryID)

	matchedWebhook, err := c.repo.Get(ctx.Request().Context(), webhookID)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to get webhook %s: %v", webhookID, err)
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "Webhook not found"})
	}

	// Verify signature using the shared SignatureVerifier
	if !c.signatureVerifier.VerifyGitHubSignature(body, signature, matchedWebhook.Secret()) {
		log.Printf("[WEBHOOK] Signature verification failed for webhook %s", webhookID)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
	}

	log.Printf("[WEBHOOK] Signature verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())

	// Handle ping event
	if event == "ping" {
		log.Printf("[WEBHOOK] Received ping event, responding with pong")
		return ctx.JSON(http.StatusOK, map[string]string{"message": "pong", "webhook_id": matchedWebhook.ID()})
	}

	// Parse payload
	var payload GitHubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[WEBHOOK] Failed to parse payload: %v", err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
	}

	// Store raw payload for template rendering
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		log.Printf("[WEBHOOK] Failed to parse raw payload: %v", err)
	}
	payload.Raw = rawPayload

	if payload.Repository == nil {
		log.Printf("[WEBHOOK] Payload missing repository information")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Payload missing repository information"})
	}

	// Match triggers
	matchResult := c.matchTriggers(matchedWebhook.Triggers(), event, &payload)
	if matchResult == nil {
		log.Printf("[WEBHOOK] No matching trigger for webhook %s, event=%s, action=%s",
			matchedWebhook.ID(), event, payload.Action)
		c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), deliveryID, entities.DeliveryStatusSkipped, nil, "", false, nil)
		return ctx.JSON(http.StatusOK, map[string]string{
			"message":    "No matching trigger",
			"webhook_id": matchedWebhook.ID(),
		})
	}

	log.Printf("[WEBHOOK] Trigger matched: %s (%s)", matchResult.ID(), matchResult.Name())

	// Build GitHub-specific metadata tags
	tags := map[string]string{
		"webhook_id":   matchedWebhook.ID(),
		"webhook_name": matchedWebhook.Name(),
		"trigger_id":   matchResult.ID(),
		"trigger_name": matchResult.Name(),
		"github_event": event,
	}
	if payload.Repository != nil {
		tags["repository"] = payload.Repository.FullName
	}
	if payload.Action != "" {
		tags["github_action"] = payload.Action
	}

	// Create or reuse session via shared service
	sessionID, sessionReused, err := c.sessionService.CreateSessionFromWebhook(ctx, SessionCreationParams{
		Webhook:        matchedWebhook,
		Trigger:        matchResult,
		Payload:        payload.Raw,
		RawPayload:     nil, // GitHub webhooks do not mount payload
		Tags:           tags,
		DefaultMessage: c.buildDefaultInitialMessage(event, &payload),
	})
	if err != nil {
		log.Printf("[WEBHOOK] Failed to create session: %v", err)
		c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), deliveryID, entities.DeliveryStatusFailed, matchResult, "", false, err)

		if IsSessionLimitError(err) {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), deliveryID, entities.DeliveryStatusProcessed, matchResult, sessionID, sessionReused, nil)

	log.Printf("[WEBHOOK] Session created successfully: %s", sessionID)

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":    "Session created",
		"session_id": sessionID,
		"webhook_id": matchedWebhook.ID(),
		"trigger_id": matchResult.ID(),
	})
}

// matchTriggers evaluates all triggers against a GitHub payload and returns the first matching trigger
func (c *WebhookGitHubController) matchTriggers(triggers []entities.WebhookTrigger, event string, payload *GitHubPayload) *entities.WebhookTrigger {
	sorted := SortTriggersByPriority(triggers)

	for i := range sorted {
		trigger := &sorted[i]
		if !trigger.Enabled() {
			continue
		}
		if c.matchTrigger(trigger, event, payload) {
			return trigger
		}
	}

	return nil
}

// matchTrigger checks if a single trigger matches the payload
func (c *WebhookGitHubController) matchTrigger(trigger *entities.WebhookTrigger, event string, payload *GitHubPayload) bool {
	cond := trigger.Conditions().GitHub()
	if cond == nil {
		log.Printf("[WEBHOOK] Trigger %s (%s): no GitHub conditions defined", trigger.ID(), trigger.Name())
		return false
	}

	// Check event type
	if len(cond.Events()) > 0 && !containsString(cond.Events(), event) {
		log.Printf("[WEBHOOK] Trigger %s (%s): event mismatch - received=%s, allowed=%v",
			trigger.ID(), trigger.Name(), event, cond.Events())
		return false
	}

	// Check action
	if len(cond.Actions()) > 0 {
		if payload.Action == "" || !containsString(cond.Actions(), payload.Action) {
			log.Printf("[WEBHOOK] Trigger %s (%s): action mismatch - received=%s, allowed=%v",
				trigger.ID(), trigger.Name(), payload.Action, cond.Actions())
			return false
		}
	}

	// Check repository
	if len(cond.Repositories()) > 0 {
		if payload.Repository == nil || !matchAnyRepository(cond.Repositories(), payload.Repository.FullName) {
			return false
		}
	}

	// Check branches
	if len(cond.Branches()) > 0 {
		branch := extractBranch(event, payload)
		if branch == "" || !matchPatterns(cond.Branches(), branch) {
			return false
		}
	}

	// Check base branches (for PR only)
	if len(cond.BaseBranches()) > 0 {
		if payload.PullRequest != nil && payload.PullRequest.Base != nil {
			if !matchPatterns(cond.BaseBranches(), payload.PullRequest.Base.Ref) {
				return false
			}
		}
	}

	// Check draft status
	if cond.Draft() != nil {
		if payload.PullRequest == nil || payload.PullRequest.Draft != *cond.Draft() {
			return false
		}
	}

	// Check labels
	if len(cond.Labels()) > 0 {
		labels := extractLabels(payload)
		if !anyStringInSlice(cond.Labels(), labels) {
			return false
		}
	}

	// Check sender
	if len(cond.Sender()) > 0 {
		if payload.Sender == nil || !containsString(cond.Sender(), payload.Sender.Login) {
			return false
		}
	}

	// Check paths (for push events)
	if len(cond.Paths()) > 0 {
		changedFiles := extractChangedFiles(payload)
		if !anyFileMatchesPatterns(cond.Paths(), changedFiles) {
			return false
		}
	}

	// Check Go template condition
	if goTemplateCondition := trigger.Conditions().GoTemplate(); goTemplateCondition != "" {
		matched, err := c.gotemplateEvaluator.Evaluate(payload.Raw, goTemplateCondition)
		if err != nil {
			log.Printf("[WEBHOOK] Trigger %s (%s): GoTemplate evaluation error: %v",
				trigger.ID(), trigger.Name(), err)
			return false
		}
		if !matched {
			log.Printf("[WEBHOOK] Trigger %s (%s): GoTemplate condition not met",
				trigger.ID(), trigger.Name())
			return false
		}
	}

	return true
}

// buildDefaultInitialMessage builds a default initial message based on the event type
func (c *WebhookGitHubController) buildDefaultInitialMessage(event string, payload *GitHubPayload) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "GitHub %s event received.\n\n", event)

	if payload.Repository != nil {
		fmt.Fprintf(&sb, "Repository: %s\n", payload.Repository.FullName)
		if payload.Repository.HTMLURL != "" {
			fmt.Fprintf(&sb, "URL: %s\n", payload.Repository.HTMLURL)
		}
	}

	switch event {
	case "push":
		if payload.Ref != "" {
			branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
			fmt.Fprintf(&sb, "Branch: %s\n", branch)
		}
		if payload.HeadCommit != nil {
			fmt.Fprintf(&sb, "Commit: %s\n", payload.HeadCommit.ID[:7])
			fmt.Fprintf(&sb, "Message: %s\n", payload.HeadCommit.Message)
		}

	case "pull_request":
		if payload.PullRequest != nil {
			fmt.Fprintf(&sb, "\nPull Request #%d: %s\n", payload.PullRequest.Number, payload.PullRequest.Title)
			fmt.Fprintf(&sb, "Action: %s\n", payload.Action)
			fmt.Fprintf(&sb, "URL: %s\n", payload.PullRequest.HTMLURL)
			if payload.PullRequest.Base != nil && payload.PullRequest.Head != nil {
				fmt.Fprintf(&sb, "Base: %s <- Head: %s\n", payload.PullRequest.Base.Ref, payload.PullRequest.Head.Ref)
			}
			if payload.PullRequest.Body != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", payload.PullRequest.Body)
			}
		}

	case "issues":
		if payload.Issue != nil {
			fmt.Fprintf(&sb, "\nIssue #%d: %s\n", payload.Issue.Number, payload.Issue.Title)
			fmt.Fprintf(&sb, "Action: %s\n", payload.Action)
			fmt.Fprintf(&sb, "URL: %s\n", payload.Issue.HTMLURL)
			if payload.Issue.Body != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", payload.Issue.Body)
			}
		}
	}

	if payload.Sender != nil {
		fmt.Fprintf(&sb, "\nTriggered by: %s\n", payload.Sender.Login)
	}

	return sb.String()
}

// Helper functions

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func matchAnyRepository(patterns []string, fullName string) bool {
	for _, pattern := range patterns {
		if matchRepository(pattern, fullName) {
			return true
		}
	}
	return false
}

func matchRepository(pattern, fullName string) bool {
	if pattern == fullName {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		owner := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(fullName, owner+"/")
	}
	return false
}

func extractBranch(event string, payload *GitHubPayload) string {
	switch event {
	case "push":
		if strings.HasPrefix(payload.Ref, "refs/heads/") {
			return strings.TrimPrefix(payload.Ref, "refs/heads/")
		}
		return payload.Ref
	case "pull_request":
		if payload.PullRequest != nil && payload.PullRequest.Head != nil {
			return payload.PullRequest.Head.Ref
		}
	case "create", "delete":
		return payload.Ref
	}
	return ""
}

func extractLabels(payload *GitHubPayload) []string {
	var labels []string
	if payload.PullRequest != nil {
		for _, label := range payload.PullRequest.Labels {
			labels = append(labels, label.Name)
		}
	}
	if payload.Issue != nil {
		for _, label := range payload.Issue.Labels {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

func extractChangedFiles(payload *GitHubPayload) []string {
	fileSet := make(map[string]bool)
	for _, commit := range payload.Commits {
		for _, f := range commit.Added {
			fileSet[f] = true
		}
		for _, f := range commit.Modified {
			fileSet[f] = true
		}
		for _, f := range commit.Removed {
			fileSet[f] = true
		}
	}

	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	return files
}

// anyStringInSlice returns true if any string from required is found in available.
func anyStringInSlice(required, available []string) bool {
	if len(available) == 0 {
		return false
	}
	for _, r := range required {
		if containsString(available, r) {
			return true
		}
	}
	return false
}

// anyFileMatchesPatterns returns true if any file matches any of the patterns.
func anyFileMatchesPatterns(patterns, files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		for _, pattern := range patterns {
			if matchPath(pattern, file) {
				return true
			}
		}
	}
	return false
}

func matchPatterns(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, value string) bool {
	if pattern == value {
		return true
	}
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

func matchPath(pattern, path string) bool {
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}

			if suffix != "" {
				remaining := path
				if prefix != "" {
					remaining = strings.TrimPrefix(path, prefix)
					remaining = strings.TrimPrefix(remaining, "/")
				}
				pathParts := strings.Split(remaining, "/")
				for i := range pathParts {
					testPath := strings.Join(pathParts[i:], "/")
					if matched, _ := filepath.Match(suffix, testPath); matched {
						return true
					}
					if matched, _ := filepath.Match(suffix, pathParts[len(pathParts)-1]); matched {
						return true
					}
				}
				return false
			}
			return true
		}
	}

	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}

// MatchTriggersForTest evaluates triggers against a test payload.
// This reuses the same matching logic as HandleGitHubWebhook but without
// signature verification or HTTP context dependency.
func (c *WebhookGitHubController) MatchTriggersForTest(
	triggers []entities.WebhookTrigger,
	event string,
	payload *GitHubPayload,
) *entities.WebhookTrigger {
	return c.matchTriggers(triggers, event, payload)
}

// BuildDefaultInitialMessageForTest builds a default initial message for a test payload.
func (c *WebhookGitHubController) BuildDefaultInitialMessageForTest(event string, payload *GitHubPayload) string {
	return c.buildDefaultInitialMessage(event, payload)
}

// BuildGitHubTagsForTest builds GitHub-specific metadata tags for a test payload.
func (c *WebhookGitHubController) BuildGitHubTagsForTest(
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
	event string,
	payload *GitHubPayload,
) map[string]string {
	tags := map[string]string{
		"webhook_id":   webhook.ID(),
		"webhook_name": webhook.Name(),
		"trigger_id":   trigger.ID(),
		"trigger_name": trigger.Name(),
		"github_event": event,
	}
	if payload.Repository != nil {
		tags["repository"] = payload.Repository.FullName
	}
	if payload.Action != "" {
		tags["github_action"] = payload.Action
	}
	return tags
}

// SessionService returns the session service for external access.
func (c *WebhookGitHubController) SessionService() *WebhookSessionService {
	return c.sessionService
}
