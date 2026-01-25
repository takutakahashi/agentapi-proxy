package controllers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WebhookGitHubController handles GitHub webhook reception
type WebhookGitHubController struct {
	repo                repositories.WebhookRepository
	sessionManager      repositories.SessionManager
	gotemplateEvaluator *webhook.GoTemplateEvaluator
}

// NewWebhookGitHubController creates a new GitHub webhook controller
func NewWebhookGitHubController(repo repositories.WebhookRepository, sessionManager repositories.SessionManager) *WebhookGitHubController {
	return &WebhookGitHubController{
		repo:                repo,
		sessionManager:      sessionManager,
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
	// Get webhook ID from URL path
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

	// Get the webhook by ID
	matchedWebhook, err := c.repo.Get(ctx.Request().Context(), webhookID)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to get webhook %s: %v", webhookID, err)
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "Webhook not found"})
	}

	// Verify signature using the webhook's secret
	if !c.verifyGitHubSignature(body, signature, matchedWebhook.Secret()) {
		log.Printf("[WEBHOOK] Signature verification failed for webhook %s", webhookID)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
	}

	log.Printf("[WEBHOOK] Signature verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())

	// Handle ping event
	if event == "ping" {
		log.Printf("[WEBHOOK] Received ping event, responding with pong")
		return ctx.JSON(http.StatusOK, map[string]string{"message": "pong", "webhook_id": matchedWebhook.ID()})
	}

	// Parse payload to extract repository info
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

		// Record skipped delivery
		record := entities.NewWebhookDeliveryRecord(deliveryID, entities.DeliveryStatusSkipped)
		if err := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); err != nil {
			log.Printf("[WEBHOOK] Failed to record delivery: %v", err)
		}

		return ctx.JSON(http.StatusOK, map[string]string{
			"message":    "No matching trigger",
			"webhook_id": matchedWebhook.ID(),
		})
	}

	log.Printf("[WEBHOOK] Trigger matched: %s (%s)", matchResult.ID(), matchResult.Name())

	// Create or reuse session
	sessionID, sessionReused, err := c.createSessionFromWebhook(ctx, matchedWebhook, matchResult, event, &payload)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to create session: %v", err)

		// Record failed delivery
		record := entities.NewWebhookDeliveryRecord(deliveryID, entities.DeliveryStatusFailed)
		record.SetMatchedTrigger(matchResult.ID())
		record.SetError(err.Error())
		if recordErr := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); recordErr != nil {
			log.Printf("[WEBHOOK] Failed to record delivery: %v", recordErr)
		}

		// Check if error is due to session limit
		if strings.Contains(err.Error(), "session limit reached") {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	// Record successful delivery
	record := entities.NewWebhookDeliveryRecord(deliveryID, entities.DeliveryStatusProcessed)
	record.SetMatchedTrigger(matchResult.ID())
	record.SetSessionID(sessionID)
	record.SetSessionReused(sessionReused)
	if err := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); err != nil {
		log.Printf("[WEBHOOK] Failed to record delivery: %v", err)
	}

	log.Printf("[WEBHOOK] Session created successfully: %s", sessionID)

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":    "Session created",
		"session_id": sessionID,
		"webhook_id": matchedWebhook.ID(),
		"trigger_id": matchResult.ID(),
	})
}

// verifyGitHubSignature verifies a GitHub webhook signature
func (c *WebhookGitHubController) verifyGitHubSignature(payload []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}

	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	signature := parts[1]

	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = hmac.New(sha256.New, []byte(secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secret))
	default:
		return false
	}

	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

// matchTriggers evaluates all triggers against a GitHub payload and returns the first matching trigger
func (c *WebhookGitHubController) matchTriggers(triggers []entities.WebhookTrigger, event string, payload *GitHubPayload) *entities.WebhookTrigger {
	// Sort triggers by priority
	sortedTriggers := make([]entities.WebhookTrigger, len(triggers))
	copy(sortedTriggers, triggers)
	sort.Slice(sortedTriggers, func(i, j int) bool {
		return sortedTriggers[i].Priority() < sortedTriggers[j].Priority()
	})

	for i := range sortedTriggers {
		trigger := &sortedTriggers[i]
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
	if len(cond.Events()) > 0 {
		if !containsString(cond.Events(), event) {
			log.Printf("[WEBHOOK] Trigger %s (%s): event mismatch - received=%s, allowed=%v",
				trigger.ID(), trigger.Name(), event, cond.Events())
			return false
		}
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
		if payload.Repository == nil {
			return false
		}
		matched := false
		for _, pattern := range cond.Repositories() {
			if matchRepository(pattern, payload.Repository.FullName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check branches
	if len(cond.Branches()) > 0 {
		branch := extractBranch(event, payload)
		if branch == "" {
			return false
		}
		if !matchPatterns(cond.Branches(), branch) {
			return false
		}
	}

	// Check base branches (for PR only - skip if not a PR event)
	if len(cond.BaseBranches()) > 0 {
		// Only apply base branch filter when PR information is available
		if payload.PullRequest != nil && payload.PullRequest.Base != nil {
			if !matchPatterns(cond.BaseBranches(), payload.PullRequest.Base.Ref) {
				return false
			}
		}
		// If no PR info, skip this check (allows non-PR events to pass)
	}

	// Check draft status
	if cond.Draft() != nil {
		if payload.PullRequest == nil {
			return false
		}
		if payload.PullRequest.Draft != *cond.Draft() {
			return false
		}
	}

	// Check labels
	if len(cond.Labels()) > 0 {
		labels := extractLabels(payload)
		if len(labels) == 0 {
			return false
		}
		hasLabel := false
		for _, requiredLabel := range cond.Labels() {
			if containsString(labels, requiredLabel) {
				hasLabel = true
				break
			}
		}
		if !hasLabel {
			return false
		}
	}

	// Check sender
	if len(cond.Sender()) > 0 {
		if payload.Sender == nil {
			return false
		}
		if !containsString(cond.Sender(), payload.Sender.Login) {
			return false
		}
	}

	// Check paths (for push events)
	if len(cond.Paths()) > 0 {
		changedFiles := extractChangedFiles(payload)
		if len(changedFiles) == 0 {
			return false
		}
		hasMatch := false
		for _, file := range changedFiles {
			for _, pattern := range cond.Paths() {
				if matchPath(pattern, file) {
					hasMatch = true
					break
				}
			}
			if hasMatch {
				break
			}
		}
		if !hasMatch {
			return false
		}
	}

	// Check Go template condition if defined
	goTemplateCondition := trigger.Conditions().GoTemplate()
	if goTemplateCondition != "" {
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

// createSessionFromWebhook creates a session based on webhook and trigger configuration
// Returns sessionID, sessionReused flag, and error
func (c *WebhookGitHubController) createSessionFromWebhook(ctx echo.Context, webhook *entities.Webhook, trigger *entities.WebhookTrigger, event string, payload *GitHubPayload) (string, bool, error) {
	// Merge session configs (trigger overrides webhook default)
	sessionConfig := c.mergeSessionConfigs(webhook.SessionConfig(), trigger.SessionConfig())

	// Build environment variables
	env := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Environment() != nil {
		for k, v := range sessionConfig.Environment() {
			env[k] = v
		}
	}

	// Build tags
	tags := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Tags() != nil {
		for k, v := range sessionConfig.Tags() {
			tags[k] = v
		}
	}

	// Add webhook metadata tags
	tags["webhook_id"] = webhook.ID()
	tags["webhook_name"] = webhook.Name()
	tags["trigger_id"] = trigger.ID()
	tags["trigger_name"] = trigger.Name()
	tags["github_event"] = event
	if payload.Repository != nil {
		tags["repository"] = payload.Repository.FullName
	}
	if payload.Action != "" {
		tags["github_action"] = payload.Action
	}

	// Generate initial message from template
	var initialMessage string
	if sessionConfig != nil && sessionConfig.InitialMessageTemplate() != "" {
		msg, err := c.renderTemplate(sessionConfig.InitialMessageTemplate(), event, payload)
		if err != nil {
			log.Printf("[WEBHOOK] Failed to render initial message template: %v", err)
			initialMessage = fmt.Sprintf("GitHub %s event received for %s", event, payload.Repository.FullName)
		} else {
			initialMessage = msg
		}
	} else {
		initialMessage = c.buildDefaultInitialMessage(event, payload)
	}

	// Check if session reuse is enabled
	if sessionConfig != nil && sessionConfig.ReuseSession() {
		log.Printf("[WEBHOOK] Session reuse is enabled, searching for existing session with tags: %v", tags)
		// Try to find existing session with all the same tags
		filter := entities.SessionFilter{
			Tags:   tags,
			Status: "active",
		}
		existingSessions := c.sessionManager.ListSessions(filter)
		log.Printf("[WEBHOOK] Found %d existing sessions matching filter", len(existingSessions))
		if len(existingSessions) > 0 {
			// Reuse the first matching session
			existingSession := existingSessions[0]
			log.Printf("[WEBHOOK] Reusing existing session %s with tags: %v", existingSession.ID(), existingSession.Tags())

			// Generate reuse message
			var reuseMessage string
			if sessionConfig.ReuseMessageTemplate() != "" {
				// Use reuse message template if specified
				msg, err := c.renderTemplate(sessionConfig.ReuseMessageTemplate(), event, payload)
				if err != nil {
					log.Printf("[WEBHOOK] Failed to render reuse message template: %v", err)
					reuseMessage = initialMessage
				} else {
					reuseMessage = msg
				}
			} else {
				// Fall back to initial message
				reuseMessage = initialMessage
			}

			// Send message to existing session
			err := c.sessionManager.SendMessage(ctx.Request().Context(), existingSession.ID(), reuseMessage)
			if err == nil {
				return existingSession.ID(), true, nil
			}
			log.Printf("[WEBHOOK] Failed to send message to existing session: %v, creating new session instead", err)
		} else {
			log.Printf("[WEBHOOK] No existing sessions found with matching tags, creating new session")
		}
	} else {
		if sessionConfig == nil {
			log.Printf("[WEBHOOK] Session reuse disabled: sessionConfig is nil")
		} else {
			log.Printf("[WEBHOOK] Session reuse disabled: reuse_session=%v", sessionConfig.ReuseSession())
		}
	}

	// Create new session
	sessionID := uuid.New().String()

	// Build session request
	req := &entities.RunServerRequest{
		UserID:         webhook.UserID(),
		Environment:    env,
		Tags:           tags,
		Scope:          webhook.Scope(),
		TeamID:         webhook.TeamID(),
		InitialMessage: initialMessage,
	}

	// Handle GitHub token
	if sessionConfig != nil && sessionConfig.Params() != nil && sessionConfig.Params().GithubToken() != "" {
		req.GithubToken = sessionConfig.Params().GithubToken()
	}

	// Set repository info from tags
	if repoFullName, ok := req.Tags["repository"]; ok && repoFullName != "" {
		req.RepoInfo = &entities.RepositoryInfo{
			FullName: repoFullName,
			CloneDir: sessionID,
		}
	}

	// Check session limit per webhook
	filter := entities.SessionFilter{
		Tags: map[string]string{
			"webhook_id": webhook.ID(),
		},
	}
	existingSessions := c.sessionManager.ListSessions(filter)
	maxSessions := webhook.MaxSessions()
	if len(existingSessions) >= maxSessions {
		return "", false, fmt.Errorf("session limit reached: maximum %d sessions per webhook", maxSessions)
	}

	// Create the session
	session, err := c.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req)
	if err != nil {
		return "", false, fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), false, nil
}

// mergeSessionConfigs merges two session configs, with override taking precedence
func (c *WebhookGitHubController) mergeSessionConfigs(base, override *entities.WebhookSessionConfig) *entities.WebhookSessionConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := entities.NewWebhookSessionConfig()

	// Merge environment
	env := make(map[string]string)
	for k, v := range base.Environment() {
		env[k] = v
	}
	for k, v := range override.Environment() {
		env[k] = v
	}
	result.SetEnvironment(env)

	// Merge tags
	tags := make(map[string]string)
	for k, v := range base.Tags() {
		tags[k] = v
	}
	for k, v := range override.Tags() {
		tags[k] = v
	}
	result.SetTags(tags)

	// Override template if provided
	if override.InitialMessageTemplate() != "" {
		result.SetInitialMessageTemplate(override.InitialMessageTemplate())
	} else {
		result.SetInitialMessageTemplate(base.InitialMessageTemplate())
	}

	// Override reuse message template if provided
	if override.ReuseMessageTemplate() != "" {
		result.SetReuseMessageTemplate(override.ReuseMessageTemplate())
	} else {
		result.SetReuseMessageTemplate(base.ReuseMessageTemplate())
	}

	// Override params if provided
	if override.Params() != nil {
		result.SetParams(override.Params())
	} else {
		result.SetParams(base.Params())
	}

	// Merge reuse session flag (override takes precedence, but also consider base if override is false)
	if override.ReuseSession() {
		result.SetReuseSession(true)
	} else if base.ReuseSession() {
		result.SetReuseSession(true)
	} else {
		result.SetReuseSession(false)
	}

	return result
}

// renderTemplate renders a Go template with webhook payload data
// Uses the raw GitHub payload directly, same as GoTemplate conditions
func (c *WebhookGitHubController) renderTemplate(tmplStr, event string, payload *GitHubPayload) (string, error) {
	tmpl, err := template.New("initial_message").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Use the raw payload directly, same as GoTemplate matcher
	// This allows templates to access payload fields like {{ .action }}, {{ .pull_request.number }}, etc.
	data := payload.Raw

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildDefaultInitialMessage builds a default initial message based on the event type
func (c *WebhookGitHubController) buildDefaultInitialMessage(event string, payload *GitHubPayload) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("GitHub %s event received.\n\n", event))

	if payload.Repository != nil {
		sb.WriteString(fmt.Sprintf("Repository: %s\n", payload.Repository.FullName))
		if payload.Repository.HTMLURL != "" {
			sb.WriteString(fmt.Sprintf("URL: %s\n", payload.Repository.HTMLURL))
		}
	}

	switch event {
	case "push":
		if payload.Ref != "" {
			branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
			sb.WriteString(fmt.Sprintf("Branch: %s\n", branch))
		}
		if payload.HeadCommit != nil {
			sb.WriteString(fmt.Sprintf("Commit: %s\n", payload.HeadCommit.ID[:7]))
			sb.WriteString(fmt.Sprintf("Message: %s\n", payload.HeadCommit.Message))
		}

	case "pull_request":
		if payload.PullRequest != nil {
			sb.WriteString(fmt.Sprintf("\nPull Request #%d: %s\n", payload.PullRequest.Number, payload.PullRequest.Title))
			sb.WriteString(fmt.Sprintf("Action: %s\n", payload.Action))
			sb.WriteString(fmt.Sprintf("URL: %s\n", payload.PullRequest.HTMLURL))
			if payload.PullRequest.Base != nil && payload.PullRequest.Head != nil {
				sb.WriteString(fmt.Sprintf("Base: %s <- Head: %s\n", payload.PullRequest.Base.Ref, payload.PullRequest.Head.Ref))
			}
			if payload.PullRequest.Body != "" {
				sb.WriteString(fmt.Sprintf("\nDescription:\n%s\n", payload.PullRequest.Body))
			}
		}

	case "issues":
		if payload.Issue != nil {
			sb.WriteString(fmt.Sprintf("\nIssue #%d: %s\n", payload.Issue.Number, payload.Issue.Title))
			sb.WriteString(fmt.Sprintf("Action: %s\n", payload.Action))
			sb.WriteString(fmt.Sprintf("URL: %s\n", payload.Issue.HTMLURL))
			if payload.Issue.Body != "" {
				sb.WriteString(fmt.Sprintf("\nDescription:\n%s\n", payload.Issue.Body))
			}
		}
	}

	if payload.Sender != nil {
		sb.WriteString(fmt.Sprintf("\nTriggered by: %s\n", payload.Sender.Login))
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

func matchRepository(pattern, fullName string) bool {
	if pattern == fullName {
		return true
	}
	// Support owner/* pattern
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

	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	return files
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
