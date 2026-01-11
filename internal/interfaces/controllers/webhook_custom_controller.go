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
	repo              repositories.WebhookRepository
	sessionManager    repositories.SessionManager
	signatureVerifier *webhook.SignatureVerifier
	jsonpathEvaluator *webhook.JSONPathEvaluator
}

// NewWebhookCustomController creates a new custom webhook controller
func NewWebhookCustomController(
	repo repositories.WebhookRepository,
	sessionManager repositories.SessionManager,
) *WebhookCustomController {
	return &WebhookCustomController{
		repo:              repo,
		sessionManager:    sessionManager,
		signatureVerifier: webhook.NewSignatureVerifier(),
		jsonpathEvaluator: webhook.NewJSONPathEvaluator(),
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

	// Verify signature using the webhook's secret
	// Use the configured signature header (defaults to X-Signature)
	headerName := matchedWebhook.SignatureHeader()
	signatureHeader := ctx.Request().Header.Get(headerName)

	if signatureHeader == "" {
		log.Printf("[WEBHOOK_CUSTOM] Missing signature header '%s' for webhook %s", headerName, webhookID)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": fmt.Sprintf("Missing signature header: %s", headerName)})
	}

	if !c.verifySignature(body, signatureHeader, matchedWebhook.Secret()) {
		log.Printf("[WEBHOOK_CUSTOM] Signature verification failed for webhook %s (header: %s)", webhookID, headerName)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
	}

	log.Printf("[WEBHOOK_CUSTOM] Signature verified for webhook %s (%s)", matchedWebhook.ID(), matchedWebhook.Name())

	// Parse payload as JSON
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to parse payload as JSON: %v", err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
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

	// Create session
	sessionID, err := c.createSessionFromWebhook(ctx, matchedWebhook, matchResult, payload)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to create session: %v", err)

		// Record failed delivery
		record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusFailed)
		record.SetMatchedTrigger(matchResult.ID())
		record.SetError(err.Error())
		if recordErr := c.repo.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), record); recordErr != nil {
			log.Printf("[WEBHOOK_CUSTOM] Failed to record delivery: %v", recordErr)
		}

		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	// Record successful delivery
	record := entities.NewWebhookDeliveryRecord("", entities.DeliveryStatusProcessed)
	record.SetMatchedTrigger(matchResult.ID())
	record.SetSessionID(sessionID)
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

// matchTrigger checks if a single trigger matches the payload using JSONPath conditions
func (c *WebhookCustomController) matchTrigger(
	trigger *entities.WebhookTrigger,
	payload map[string]interface{},
) bool {
	cond := trigger.Conditions()

	// Get JSONPath conditions
	jsonPathConditions := cond.JSONPath()
	if len(jsonPathConditions) == 0 {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): no JSONPath conditions defined", trigger.ID(), trigger.Name())
		return false
	}

	// Evaluate all JSONPath conditions (AND logic)
	matched, err := c.jsonpathEvaluator.Evaluate(payload, jsonPathConditions)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): JSONPath evaluation error: %v",
			trigger.ID(), trigger.Name(), err)
		return false
	}

	if !matched {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): JSONPath conditions not met",
			trigger.ID(), trigger.Name())
	}

	return matched
}

// createSessionFromWebhook creates a session based on webhook and trigger configuration
func (c *WebhookCustomController) createSessionFromWebhook(
	ctx echo.Context,
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
	payload map[string]interface{},
) (string, error) {
	sessionID := uuid.New().String()

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

	// Create the session
	session, err := c.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), nil
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

	// Override params if provided
	if override.Params() != nil {
		result.SetParams(override.Params())
	} else {
		result.SetParams(base.Params())
	}

	return result
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
