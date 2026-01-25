package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WebhookCustomController handles custom webhook reception
type WebhookCustomController struct {
	repo                repositories.WebhookRepository
	sessionManager      repositories.SessionManager
	signatureVerifier   *webhook.SignatureVerifier
	jsonpathEvaluator   *webhook.JSONPathEvaluator
	gotemplateEvaluator *webhook.GoTemplateEvaluator
}

// NewWebhookCustomController creates a new custom webhook controller
func NewWebhookCustomController(
	repo repositories.WebhookRepository,
	sessionManager repositories.SessionManager,
) *WebhookCustomController {
	return &WebhookCustomController{
		repo:                repo,
		sessionManager:      sessionManager,
		signatureVerifier:   webhook.NewSignatureVerifier(),
		jsonpathEvaluator:   webhook.NewJSONPathEvaluator(),
		gotemplateEvaluator: webhook.NewGoTemplateEvaluator(),
	}
}

// GetName returns the name of this controller for logging
func (c *WebhookCustomController) GetName() string {
	return "WebhookCustomController"
}

// HandleCustomWebhook handles POST /hooks/custom/:id
func (c *WebhookCustomController) HandleCustomWebhook(ctx echo.Context) error {
	// Get webhook ID from URL path
	webhookID := ctx.Param("id")
	if webhookID == "" {
		log.Printf("[WEBHOOK_CUSTOM] Missing webhook ID in URL path")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Missing webhook ID"})
	}

	// Read the raw body for signature verification
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to read request body: %v", err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to read request body"})
	}
	ctx.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	log.Printf("[WEBHOOK_CUSTOM] Received custom webhook: webhook_id=%s, content_type=%s, body_size=%d",
		webhookID, ctx.Request().Header.Get("Content-Type"), len(body))

	// Get the webhook by ID
	matchedWebhook, err := c.repo.Get(ctx.Request().Context(), webhookID)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to get webhook %s: %v", webhookID, err)
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "Webhook not found"})
	}

	// Verify webhook type
	if matchedWebhook.WebhookType() != entities.WebhookTypeCustom {
		log.Printf("[WEBHOOK_CUSTOM] Webhook %s is not a custom webhook (type=%s)", webhookID, matchedWebhook.WebhookType())
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Webhook is not a custom webhook"})
	}

	// Verify signature based on the configured signature type
	sigType := matchedWebhook.SignatureType()

	switch sigType {
	case entities.WebhookSignatureTypeHMAC:
		// HMAC signature verification (default)
		headerName := matchedWebhook.SignatureHeader()
		signatureHeader := ctx.Request().Header.Get(headerName)

		if signatureHeader == "" {
			log.Printf("[WEBHOOK_CUSTOM] Missing signature header '%s' for webhook %s", headerName, webhookID)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Missing signature header"})
		}

		if !c.verifySignature(body, signatureHeader, matchedWebhook.Secret()) {
			log.Printf("[WEBHOOK_CUSTOM] Signature verification failed for webhook %s (header: %s)", webhookID, headerName)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
		}

		log.Printf("[WEBHOOK_CUSTOM] HMAC signature verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())

	case entities.WebhookSignatureTypeStatic:
		// Static token comparison
		headerName := matchedWebhook.SignatureHeader()
		token := ctx.Request().Header.Get(headerName)

		if token == "" {
			log.Printf("[WEBHOOK_CUSTOM] Missing token header '%s' for webhook %s", headerName, webhookID)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		}

		if token != matchedWebhook.Secret() {
			log.Printf("[WEBHOOK_CUSTOM] Token verification failed for webhook %s (header: %s)", webhookID, headerName)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Token verification failed"})
		}

		log.Printf("[WEBHOOK_CUSTOM] Static token verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())

	default:
		log.Printf("[WEBHOOK_CUSTOM] Unknown signature type '%s' for webhook %s, defaulting to HMAC", sigType, webhookID)

		// Default to HMAC verification
		headerName := matchedWebhook.SignatureHeader()
		signatureHeader := ctx.Request().Header.Get(headerName)

		if signatureHeader == "" {
			log.Printf("[WEBHOOK_CUSTOM] Missing signature header '%s' for webhook %s", headerName, webhookID)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Missing signature header"})
		}

		if !c.verifySignature(body, signatureHeader, matchedWebhook.Secret()) {
			log.Printf("[WEBHOOK_CUSTOM] Signature verification failed for webhook %s (header: %s)", webhookID, headerName)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
		}

		log.Printf("[WEBHOOK_CUSTOM] Signature verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())
	}

	// Parse payload as JSON
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to parse payload as JSON: %v", err)

		// Create session with parse error message
		sessionID, sessionErr := c.createSessionForParseError(ctx, matchedWebhook, err, body)
		if sessionErr != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to create error session: %v", sessionErr)
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
		}

		// Record failed delivery
		record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusFailed)
		record.SetSessionID(sessionID)
		record.SetError(fmt.Sprintf("JSON parse error: %v", err))
		if recordErr := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); recordErr != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to record delivery: %v", recordErr)
		}

		log.Printf("[WEBHOOK_CUSTOM] Session created with parse error: %s", sessionID)

		return ctx.JSON(http.StatusOK, map[string]string{
			"message":    "Session created with parse error",
			"session_id": sessionID,
			"webhook_id": matchedWebhook.ID(),
		})
	}

	// Match triggers based on JSONPath conditions
	matchResult := c.matchTriggers(matchedWebhook.Triggers(), payload)
	if matchResult == nil {
		log.Printf("[WEBHOOK_CUSTOM] No matching trigger for webhook %s", matchedWebhook.ID())

		// Record skipped delivery
		record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusSkipped)
		if err := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); err != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to record delivery: %v", err)
		}

		return ctx.JSON(http.StatusOK, map[string]string{
			"message":    "No matching trigger",
			"webhook_id": matchedWebhook.ID(),
		})
	}

	log.Printf("[WEBHOOK_CUSTOM] Trigger matched: %s (%s)", matchResult.ID(), matchResult.Name())

	// Create or reuse session
	sessionID, sessionReused, err := c.createSessionFromWebhook(ctx, matchedWebhook, matchResult, payload)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to create session: %v", err)

		// Record failed delivery
		record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusFailed)
		record.SetMatchedTrigger(matchResult.ID())
		record.SetError(err.Error())
		if recordErr := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); recordErr != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to record delivery: %v", recordErr)
		}

		// Check if error is due to session limit
		if strings.Contains(err.Error(), "session limit reached") {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	// Record successful delivery
	record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusProcessed)
	record.SetMatchedTrigger(matchResult.ID())
	record.SetSessionID(sessionID)
	record.SetSessionReused(sessionReused)
	if err := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to record delivery: %v", err)
	}

	log.Printf("[WEBHOOK_CUSTOM] Session created successfully: %s", sessionID)

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":    "Session created",
		"session_id": sessionID,
		"webhook_id": matchedWebhook.ID(),
		"trigger_id": matchResult.ID(),
	})
}

// verifySignature verifies a webhook signature
func (c *WebhookCustomController) verifySignature(payload []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}

	// Determine algorithm from signature header format
	algorithm := "sha256" // default
	if strings.Contains(signatureHeader, "sha1=") {
		algorithm = "sha1"
	} else if strings.Contains(signatureHeader, "sha512=") {
		algorithm = "sha512"
	}

	config := webhook.SignatureConfig{
		Secret:    secret,
		Algorithm: algorithm,
	}

	return c.signatureVerifier.Verify(payload, signatureHeader, config)
}

// matchTriggers evaluates all triggers against a payload and returns the first matching trigger
func (c *WebhookCustomController) matchTriggers(
	triggers []entities.WebhookTrigger,
	payload map[string]interface{},
) *entities.WebhookTrigger {
	// Sort triggers by priority
	sortedTriggers := make([]entities.WebhookTrigger, len(triggers))
	copy(sortedTriggers, triggers)

	// Simple bubble sort by priority (lower number = higher priority)
	for i := 0; i < len(sortedTriggers)-1; i++ {
		for j := 0; j < len(sortedTriggers)-i-1; j++ {
			if sortedTriggers[j].Priority() > sortedTriggers[j+1].Priority() {
				sortedTriggers[j], sortedTriggers[j+1] = sortedTriggers[j+1], sortedTriggers[j]
			}
		}
	}

	for i := range sortedTriggers {
		trigger := &sortedTriggers[i]
		if !trigger.Enabled() {
			continue
		}

		if c.matchTrigger(trigger, payload) {
			return trigger
		}

		// If stop_on_match is set, don't evaluate further triggers
		// even if this one didn't match
		if trigger.StopOnMatch() {
			break
		}
	}

	return nil
}

// matchTrigger checks if a single trigger matches the payload using JSONPath and/or GoTemplate conditions
func (c *WebhookCustomController) matchTrigger(
	trigger *entities.WebhookTrigger,
	payload map[string]interface{},
) bool {
	cond := trigger.Conditions()

	// Get both condition types
	jsonPathConditions := cond.JSONPath()
	goTemplateCondition := cond.GoTemplate()

	// At least one condition type must be defined
	if len(jsonPathConditions) == 0 && goTemplateCondition == "" {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): no conditions defined", trigger.ID(), trigger.Name())
		return false
	}

	// Evaluate JSONPath conditions if present
	jsonPathMatched := true
	if len(jsonPathConditions) > 0 {
		matched, err := c.jsonpathEvaluator.Evaluate(payload, jsonPathConditions)
		if err != nil {
			log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): JSONPath evaluation error: %v",
				trigger.ID(), trigger.Name(), err)
			return false
		}
		jsonPathMatched = matched
		if !matched {
			log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): JSONPath conditions not met",
				trigger.ID(), trigger.Name())
		}
	}

	// Evaluate GoTemplate condition if present
	goTemplateMatched := true
	if goTemplateCondition != "" {
		matched, err := c.gotemplateEvaluator.Evaluate(payload, goTemplateCondition)
		if err != nil {
			log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): GoTemplate evaluation error: %v",
				trigger.ID(), trigger.Name(), err)
			return false
		}
		goTemplateMatched = matched
		if !matched {
			log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): GoTemplate condition not met",
				trigger.ID(), trigger.Name())
		}
	}

	// Both conditions must match (AND logic)
	return jsonPathMatched && goTemplateMatched
}

// createSessionFromWebhook creates a session based on webhook and trigger configuration
func (c *WebhookCustomController) createSessionFromWebhook(
	ctx echo.Context,
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
	payload map[string]interface{},
) (string, bool, error) {
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
	tags["webhook_type"] = string(webhook.WebhookType())
	tags["trigger_id"] = trigger.ID()
	tags["trigger_name"] = trigger.Name()

	// Generate initial message from template
	var initialMessage string
	if sessionConfig != nil && sessionConfig.InitialMessageTemplate() != "" {
		msg, err := c.renderTemplate(sessionConfig.InitialMessageTemplate(), payload)
		if err != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to render initial message template: %v", err)
			initialMessage = "Custom webhook event received"
		} else {
			initialMessage = msg
		}
	} else {
		initialMessage = c.buildDefaultInitialMessage(payload)
	}

	// Check if session reuse is enabled
	if sessionConfig != nil && sessionConfig.ReuseSession() {
		log.Printf("[WEBHOOK_CUSTOM] Session reuse is enabled, searching for existing session with tags: %v", tags)
		// Try to find existing session with all the same tags
		filter := entities.SessionFilter{
			Tags:   tags,
			Status: "active",
		}
		existingSessions := c.sessionManager.ListSessions(filter)
		log.Printf("[WEBHOOK_CUSTOM] Found %d existing sessions matching filter", len(existingSessions))
		if len(existingSessions) > 0 {
			// Reuse the first matching session
			existingSession := existingSessions[0]
			log.Printf("[WEBHOOK_CUSTOM] Reusing existing session %s with tags: %v", existingSession.ID(), existingSession.Tags())

			// Generate reuse message
			var reuseMessage string
			if sessionConfig.ReuseMessageTemplate() != "" {
				// Use reuse message template if specified
				msg, err := c.renderTemplate(sessionConfig.ReuseMessageTemplate(), payload)
				if err != nil {
					log.Printf("[WEBHOOK_CUSTOM] Failed to render reuse message template: %v", err)
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
			log.Printf("[WEBHOOK_CUSTOM] Failed to send message to existing session: %v, creating new session instead", err)
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

	// Handle GitHub token if provided
	if sessionConfig != nil && sessionConfig.Params() != nil && sessionConfig.Params().GithubToken() != "" {
		req.GithubToken = sessionConfig.Params().GithubToken()
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
func (c *WebhookCustomController) mergeSessionConfigs(
	base, override *entities.WebhookSessionConfig,
) *entities.WebhookSessionConfig {
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

// createSessionForParseError creates a session when payload parsing fails
func (c *WebhookCustomController) createSessionForParseError(
	ctx echo.Context,
	webhook *entities.Webhook,
	parseErr error,
	rawBody []byte,
) (string, error) {
	sessionID := uuid.New().String()

	// Use webhook-level session config
	sessionConfig := webhook.SessionConfig()

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
	tags["webhook_type"] = string(webhook.WebhookType())
	tags["parse_error"] = "true"

	// Create error message
	initialMessage := fmt.Sprintf(`Custom webhook parse error

Webhook: %s
Error: %s

Raw payload (first 500 chars):
%s

Please ensure the webhook payload is valid JSON.
`, webhook.Name(), parseErr.Error(), c.truncateString(string(rawBody), 500))

	// Build session request
	req := &entities.RunServerRequest{
		UserID:         webhook.UserID(),
		Environment:    env,
		Tags:           tags,
		Scope:          webhook.Scope(),
		TeamID:         webhook.TeamID(),
		InitialMessage: initialMessage,
	}

	// Handle GitHub token if provided
	if sessionConfig != nil && sessionConfig.Params() != nil && sessionConfig.Params().GithubToken() != "" {
		req.GithubToken = sessionConfig.Params().GithubToken()
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
		return "", fmt.Errorf("session limit reached: maximum %d sessions per webhook", maxSessions)
	}

	// Create the session
	session, err := c.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), nil
}

// truncateString truncates a string to the specified length
func (c *WebhookCustomController) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// renderTemplate renders a Go template with webhook payload data
func (c *WebhookCustomController) renderTemplate(tmplStr string, payload map[string]interface{}) (string, error) {
	tmpl, err := template.New("initial_message").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Payload is already a map[string]interface{}, use it directly
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildDefaultInitialMessage builds a default initial message from the payload
func (c *WebhookCustomController) buildDefaultInitialMessage(payload map[string]interface{}) string {
	// Try to extract common fields
	eventType := ""
	if event, ok := payload["event"].(string); ok {
		eventType = event
	} else if eventMap, ok := payload["event"].(map[string]interface{}); ok {
		if eventTypeVal, ok := eventMap["type"].(string); ok {
			eventType = eventTypeVal
		}
	}

	// Build a simple message
	if eventType != "" {
		return fmt.Sprintf("Custom webhook event received: %s\n\nPayload: %v", eventType, payload)
	}

	return fmt.Sprintf("Custom webhook event received\n\nPayload: %v", payload)
}
