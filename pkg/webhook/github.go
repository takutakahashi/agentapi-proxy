package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// HandleGitHubWebhook handles POST /hooks/github
func (h *Handlers) HandleGitHubWebhook(c echo.Context) error {
	// Read the raw body for signature verification
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to read request body: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to read request body"})
	}
	// Restore body for potential re-reading
	c.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	// Extract GitHub headers
	event := c.Request().Header.Get("X-GitHub-Event")
	deliveryID := c.Request().Header.Get("X-GitHub-Delivery")
	signature := c.Request().Header.Get("X-Hub-Signature-256")
	enterpriseHost := c.Request().Header.Get("X-GitHub-Enterprise-Host")

	if event == "" {
		log.Printf("[WEBHOOK] Missing X-GitHub-Event header")
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Missing X-GitHub-Event header"})
	}

	log.Printf("[WEBHOOK] Received GitHub webhook: event=%s, delivery=%s, enterprise=%s", event, deliveryID, enterpriseHost)

	// Handle ping event
	if event == "ping" {
		log.Printf("[WEBHOOK] Received ping event, responding with pong")
		return c.JSON(http.StatusOK, map[string]string{"message": "pong"})
	}

	// Parse payload to extract repository info
	var payload GitHubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[WEBHOOK] Failed to parse payload: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
	}

	// Store raw payload for template rendering
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		log.Printf("[WEBHOOK] Failed to parse raw payload: %v", err)
	}
	payload.Raw = rawPayload

	if payload.Repository == nil {
		log.Printf("[WEBHOOK] Payload missing repository information")
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Payload missing repository information"})
	}

	// Find matching webhooks
	matcher := GitHubMatcher{
		Repository:    payload.Repository.FullName,
		EnterpriseURL: enterpriseHost,
		Event:         event,
	}

	webhooks, err := h.manager.FindByGitHubRepository(c.Request().Context(), matcher)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to find matching webhooks: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
	}

	if len(webhooks) == 0 {
		log.Printf("[WEBHOOK] No matching webhooks found for repository %s", payload.Repository.FullName)
		return c.JSON(http.StatusOK, map[string]string{"message": "No matching webhooks"})
	}

	log.Printf("[WEBHOOK] Found %d candidate webhooks for repository %s", len(webhooks), payload.Repository.FullName)

	// Try to verify signature against each webhook's secret
	var matchedWebhook *WebhookConfig
	for _, webhook := range webhooks {
		if VerifyGitHubSignature(body, signature, webhook.Secret) {
			matchedWebhook = webhook
			log.Printf("[WEBHOOK] Signature verified for webhook %s (%s)", webhook.ID, webhook.Name)
			break
		}
	}

	if matchedWebhook == nil {
		log.Printf("[WEBHOOK] Signature verification failed for all candidate webhooks")
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
	}

	// Match triggers
	matchResult := MatchTriggers(matchedWebhook.Triggers, event, &payload)
	if !matchResult.Matched {
		log.Printf("[WEBHOOK] No matching trigger for webhook %s, event=%s, action=%s",
			matchedWebhook.ID, event, payload.Action)

		// Record skipped delivery
		record := DeliveryRecord{
			ID:         deliveryID,
			ReceivedAt: time.Now(),
			Status:     DeliveryStatusSkipped,
		}
		if err := h.manager.RecordDelivery(c.Request().Context(), matchedWebhook.ID, record); err != nil {
			log.Printf("[WEBHOOK] Failed to record delivery: %v", err)
		}

		return c.JSON(http.StatusOK, map[string]string{
			"message":    "No matching trigger",
			"webhook_id": matchedWebhook.ID,
		})
	}

	log.Printf("[WEBHOOK] Trigger matched: %s (%s)", matchResult.Trigger.ID, matchResult.Trigger.Name)

	// Create session
	sessionID, err := h.createSessionFromWebhook(c, matchedWebhook, matchResult.Trigger, event, &payload)
	if err != nil {
		log.Printf("[WEBHOOK] Failed to create session: %v", err)

		// Record failed delivery
		record := DeliveryRecord{
			ID:             deliveryID,
			ReceivedAt:     time.Now(),
			Status:         DeliveryStatusFailed,
			MatchedTrigger: matchResult.Trigger.ID,
			Error:          err.Error(),
		}
		if recordErr := h.manager.RecordDelivery(c.Request().Context(), matchedWebhook.ID, record); recordErr != nil {
			log.Printf("[WEBHOOK] Failed to record delivery: %v", recordErr)
		}

		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	// Record successful delivery
	record := DeliveryRecord{
		ID:             deliveryID,
		ReceivedAt:     time.Now(),
		Status:         DeliveryStatusProcessed,
		MatchedTrigger: matchResult.Trigger.ID,
		SessionID:      sessionID,
	}
	if err := h.manager.RecordDelivery(c.Request().Context(), matchedWebhook.ID, record); err != nil {
		log.Printf("[WEBHOOK] Failed to record delivery: %v", err)
	}

	log.Printf("[WEBHOOK] Session created successfully: %s", sessionID)

	return c.JSON(http.StatusOK, map[string]string{
		"message":    "Session created",
		"session_id": sessionID,
		"webhook_id": matchedWebhook.ID,
		"trigger_id": matchResult.Trigger.ID,
	})
}

// createSessionFromWebhook creates a session based on webhook and trigger configuration
func (h *Handlers) createSessionFromWebhook(c echo.Context, webhook *WebhookConfig, trigger *Trigger, event string, payload *GitHubPayload) (string, error) {
	sessionID := uuid.New().String()

	// Merge session configs (trigger overrides webhook default)
	sessionConfig := mergeSessionConfigs(webhook.SessionConfig, trigger.SessionConfig)

	// Build environment variables
	env := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Environment != nil {
		for k, v := range sessionConfig.Environment {
			env[k] = v
		}
	}

	// Build tags
	tags := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Tags != nil {
		for k, v := range sessionConfig.Tags {
			tags[k] = v
		}
	}

	// Add webhook metadata tags
	tags["webhook_id"] = webhook.ID
	tags["webhook_name"] = webhook.Name
	tags["trigger_id"] = trigger.ID
	tags["trigger_name"] = trigger.Name
	tags["github_event"] = event
	if payload.Repository != nil {
		tags["repository"] = payload.Repository.FullName
	}
	if payload.Action != "" {
		tags["github_action"] = payload.Action
	}

	// Generate initial message from template
	var initialMessage string
	if sessionConfig != nil && sessionConfig.InitialMessageTemplate != "" {
		msg, err := renderTemplate(sessionConfig.InitialMessageTemplate, event, payload)
		if err != nil {
			log.Printf("[WEBHOOK] Failed to render initial message template: %v", err)
			// Use a default message
			initialMessage = fmt.Sprintf("GitHub %s event received for %s", event, payload.Repository.FullName)
		} else {
			initialMessage = msg
		}
	} else {
		// Default message
		initialMessage = buildDefaultInitialMessage(event, payload)
	}

	// Build session request
	req := &entities.RunServerRequest{
		UserID:         webhook.UserID,
		Environment:    env,
		Tags:           tags,
		Scope:          webhook.Scope,
		TeamID:         webhook.TeamID,
		InitialMessage: initialMessage,
	}

	// Handle GitHub token
	if sessionConfig != nil && sessionConfig.Params != nil && sessionConfig.Params.GithubToken != "" {
		req.GithubToken = sessionConfig.Params.GithubToken
	}

	// Extract repository info from tags
	req.RepoInfo = app.ExtractRepositoryInfo(req.Tags, sessionID)

	// Create the session
	session, err := h.sessionManager.CreateSession(c.Request().Context(), sessionID, req)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), nil
}

// mergeSessionConfigs merges two session configs, with override taking precedence
func mergeSessionConfigs(base, override *SessionConfig) *SessionConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := &SessionConfig{
		Environment:            make(map[string]string),
		Tags:                   make(map[string]string),
		InitialMessageTemplate: base.InitialMessageTemplate,
		Params:                 base.Params,
	}

	// Merge environment
	for k, v := range base.Environment {
		result.Environment[k] = v
	}
	for k, v := range override.Environment {
		result.Environment[k] = v
	}

	// Merge tags
	for k, v := range base.Tags {
		result.Tags[k] = v
	}
	for k, v := range override.Tags {
		result.Tags[k] = v
	}

	// Override template if provided
	if override.InitialMessageTemplate != "" {
		result.InitialMessageTemplate = override.InitialMessageTemplate
	}

	// Override params if provided
	if override.Params != nil {
		result.Params = override.Params
	}

	return result
}

// renderTemplate renders a Go template with webhook payload data
func renderTemplate(tmplStr, event string, payload *GitHubPayload) (string, error) {
	tmpl, err := template.New("initial_message").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	data := map[string]interface{}{
		"event":   event,
		"payload": payload.Raw,
	}

	// Add convenience fields
	if payload.Repository != nil {
		data["repository"] = payload.Repository
	}
	if payload.Sender != nil {
		data["sender"] = payload.Sender
	}
	if payload.PullRequest != nil {
		data["pull_request"] = payload.PullRequest
	}
	if payload.Issue != nil {
		data["issue"] = payload.Issue
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildDefaultInitialMessage builds a default initial message based on the event type
func buildDefaultInitialMessage(event string, payload *GitHubPayload) string {
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
