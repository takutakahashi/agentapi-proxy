package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WebhookCustomController handles custom webhook reception
type WebhookCustomController struct {
	repo                repositories.WebhookRepository
	sessionService      *WebhookSessionService
	sessionManager      repositories.SessionManager
	signatureVerifier   *webhook.SignatureVerifier
	gotemplateEvaluator *webhook.GoTemplateEvaluator
}

// NewWebhookCustomController creates a new custom webhook controller
func NewWebhookCustomController(
	repo repositories.WebhookRepository,
	sessionManager repositories.SessionManager,
) *WebhookCustomController {
	return &WebhookCustomController{
		repo:                repo,
		sessionService:      NewWebhookSessionService(repo, sessionManager),
		sessionManager:      sessionManager,
		signatureVerifier:   webhook.NewSignatureVerifier(),
		gotemplateEvaluator: webhook.NewGoTemplateEvaluator(),
	}
}

// GetName returns the name of this controller for logging
func (c *WebhookCustomController) GetName() string {
	return "WebhookCustomController"
}

// HandleCustomWebhook handles POST /hooks/custom/:id
func (c *WebhookCustomController) HandleCustomWebhook(ctx echo.Context) error {
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

	// Verify signature
	if err := c.verifyWebhookSignature(ctx, body, matchedWebhook); err != nil {
		return err
	}

	// Parse payload as JSON
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to parse payload as JSON: %v", err)
		return c.handleParseError(ctx, matchedWebhook, err, body)
	}

	// Match triggers
	matchResult := c.matchTriggers(matchedWebhook.Triggers(), payload)
	if matchResult == nil {
		log.Printf("[WEBHOOK_CUSTOM] No matching trigger for webhook %s", matchedWebhook.ID())
		c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), "", entities.DeliveryStatusSkipped, nil, "", false, nil)
		return ctx.JSON(http.StatusOK, map[string]string{
			"message":    "No matching trigger",
			"webhook_id": matchedWebhook.ID(),
		})
	}

	log.Printf("[WEBHOOK_CUSTOM] Trigger matched: %s (%s)", matchResult.ID(), matchResult.Name())

	// Build custom webhook metadata tags
	tags := map[string]string{
		"webhook_id":   matchedWebhook.ID(),
		"webhook_name": matchedWebhook.Name(),
		"webhook_type": string(matchedWebhook.WebhookType()),
		"trigger_id":   matchResult.ID(),
		"trigger_name": matchResult.Name(),
	}

	// Create or reuse session via shared service
	sessionID, sessionReused, err := c.sessionService.CreateSessionFromWebhook(ctx, SessionCreationParams{
		Webhook:        matchedWebhook,
		Trigger:        matchResult,
		Payload:        payload,
		RawPayload:     body,
		Tags:           tags,
		DefaultMessage: buildCustomDefaultMessage(payload),
	})
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to create session: %v", err)
		c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), "", entities.DeliveryStatusFailed, matchResult, "", false, err)

		if IsSessionLimitError(err) {
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	c.sessionService.RecordDelivery(ctx.Request().Context(), matchedWebhook.ID(), "", entities.DeliveryStatusProcessed, matchResult, sessionID, sessionReused, nil)

	log.Printf("[WEBHOOK_CUSTOM] Session created successfully: %s", sessionID)

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":    "Session created",
		"session_id": sessionID,
		"webhook_id": matchedWebhook.ID(),
		"trigger_id": matchResult.ID(),
	})
}

// verifyWebhookSignature verifies the webhook signature based on the configured type.
// Returns an echo error response if verification fails, or nil on success.
func (c *WebhookCustomController) verifyWebhookSignature(ctx echo.Context, body []byte, wh *entities.Webhook) error {
	headerName := wh.SignatureHeader()
	headerValue := ctx.Request().Header.Get(headerName)

	if headerValue == "" {
		log.Printf("[WEBHOOK_CUSTOM] Missing signature header '%s' for webhook %s", headerName, wh.ID())
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Missing signature header"})
	}

	switch wh.SignatureType() {
	case entities.WebhookSignatureTypeStatic:
		if headerValue != wh.Secret() {
			log.Printf("[WEBHOOK_CUSTOM] Token verification failed for webhook %s (header: %s)", wh.ID(), headerName)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Token verification failed"})
		}
		log.Printf("[WEBHOOK_CUSTOM] Static token verified for webhook %s (%s)", wh.ID(), wh.Name())

	default:
		// HMAC verification (default for both "hmac" and unknown types)
		algorithm := detectAlgorithm(headerValue)
		config := webhook.SignatureConfig{
			Secret:    wh.Secret(),
			Algorithm: algorithm,
		}
		if !c.signatureVerifier.Verify(body, headerValue, config) {
			log.Printf("[WEBHOOK_CUSTOM] Signature verification failed for webhook %s (header: %s)", wh.ID(), headerName)
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "Signature verification failed"})
		}
		log.Printf("[WEBHOOK_CUSTOM] HMAC signature verified for webhook %s (%s)", wh.ID(), wh.Name())
	}

	return nil
}

// matchTriggers evaluates all triggers against a payload and returns the first matching trigger
func (c *WebhookCustomController) matchTriggers(
	triggers []entities.WebhookTrigger,
	payload map[string]interface{},
) *entities.WebhookTrigger {
	sorted := SortTriggersByPriority(triggers)

	for i := range sorted {
		trigger := &sorted[i]
		if !trigger.Enabled() {
			continue
		}

		if c.matchTrigger(trigger, payload) {
			return trigger
		}

		if trigger.StopOnMatch() {
			break
		}
	}

	return nil
}

// matchTrigger checks if a single trigger matches the payload using GoTemplate conditions
func (c *WebhookCustomController) matchTrigger(
	trigger *entities.WebhookTrigger,
	payload map[string]interface{},
) bool {
	goTemplateCondition := trigger.Conditions().GoTemplate()
	if goTemplateCondition == "" {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): no conditions defined", trigger.ID(), trigger.Name())
		return false
	}

	matched, err := c.gotemplateEvaluator.Evaluate(payload, goTemplateCondition)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): GoTemplate evaluation error: %v",
			trigger.ID(), trigger.Name(), err)
		return false
	}
	if !matched {
		log.Printf("[WEBHOOK_CUSTOM] Trigger %s (%s): GoTemplate condition not met",
			trigger.ID(), trigger.Name())
	}

	return matched
}

// handleParseError creates a session when payload parsing fails.
func (c *WebhookCustomController) handleParseError(
	ctx echo.Context,
	wh *entities.Webhook,
	parseErr error,
	rawBody []byte,
) error {
	sessionID := uuid.New().String()

	sessionConfig := wh.SessionConfig()

	env := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Environment() != nil {
		for k, v := range sessionConfig.Environment() {
			env[k] = v
		}
	}

	tags := make(map[string]string)
	if sessionConfig != nil && sessionConfig.Tags() != nil {
		for k, v := range sessionConfig.Tags() {
			tags[k] = v
		}
	}
	tags["webhook_id"] = wh.ID()
	tags["webhook_name"] = wh.Name()
	tags["webhook_type"] = string(wh.WebhookType())
	tags["parse_error"] = "true"

	initialMessage := fmt.Sprintf(`Custom webhook parse error

Webhook: %s
Error: %s

Raw payload (first 500 chars):
%s

Please ensure the webhook payload is valid JSON.
`, wh.Name(), parseErr.Error(), truncateString(string(rawBody), 500))

	req := &entities.RunServerRequest{
		UserID:         wh.UserID(),
		Environment:    env,
		Tags:           tags,
		Scope:          wh.Scope(),
		TeamID:         wh.TeamID(),
		InitialMessage: initialMessage,
	}

	if sessionConfig != nil && sessionConfig.Params() != nil {
		params := sessionConfig.Params()
		if params.GithubToken != "" {
			req.GithubToken = params.GithubToken
		}
		if params.AgentType != "" {
			req.AgentType = params.AgentType
		}
		req.Oneshot = params.Oneshot
	}

	// Check session limit
	if err := c.sessionService.checkSessionLimit(wh); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
	}

	session, err := c.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req, nil)
	if err != nil {
		log.Printf("[WEBHOOK_CUSTOM] Failed to create error session: %v", err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
	}

	c.sessionService.RecordDelivery(ctx.Request().Context(), wh.ID(), "", entities.DeliveryStatusFailed, nil, session.ID(), false, fmt.Errorf("JSON parse error: %v", parseErr))

	log.Printf("[WEBHOOK_CUSTOM] Session created with parse error: %s", session.ID())

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":    "Session created with parse error",
		"session_id": session.ID(),
		"webhook_id": wh.ID(),
	})
}

// Helper functions

func detectAlgorithm(signatureHeader string) string {
	switch {
	case strings.Contains(signatureHeader, "sha1="):
		return "sha1"
	case strings.Contains(signatureHeader, "sha512="):
		return "sha512"
	default:
		return "sha256"
	}
}

func buildCustomDefaultMessage(payload map[string]interface{}) string {
	eventType := ""
	if event, ok := payload["event"].(string); ok {
		eventType = event
	} else if eventMap, ok := payload["event"].(map[string]interface{}); ok {
		if eventTypeVal, ok := eventMap["type"].(string); ok {
			eventType = eventTypeVal
		}
	}

	if eventType != "" {
		return fmt.Sprintf("Custom webhook event received: %s\n\nPayload: %v", eventType, payload)
	}

	return fmt.Sprintf("Custom webhook event received\n\nPayload: %v", payload)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
