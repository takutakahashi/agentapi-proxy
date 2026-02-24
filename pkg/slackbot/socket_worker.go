package slackbot

import (
	"context"
	"encoding/json"
	"log"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// SlackSocketWorker manages one Socket Mode WebSocket connection for a Slack App.
// For the "default" group (bots with no custom token), botID is "default".
// For custom bots, botID is the SlackBot entity ID.
type SlackSocketWorker struct {
	// botID is "default" or the SlackBot entity ID for custom-token bots
	botID string
	// appTokenSecretName is the K8s Secret name holding the App-level token (xapp-...)
	appTokenSecretName string
	// appTokenSecretKey is the key within the Secret for the App-level token (default: "app-token")
	appTokenSecretKey string
	// botTokenSecretName is the K8s Secret name holding the bot token (xoxb-...)
	botTokenSecretName string
	// botTokenSecretKey is the key within the Secret for the bot token (default: "bot-token")
	botTokenSecretKey string

	channelResolver *services.SlackChannelResolver
	eventHandler    *controllers.SlackBotEventHandler
}

// NewSlackSocketWorker creates a new SlackSocketWorker
func NewSlackSocketWorker(
	botID string,
	appTokenSecretName string,
	appTokenSecretKey string,
	botTokenSecretName string,
	botTokenSecretKey string,
	channelResolver *services.SlackChannelResolver,
	eventHandler *controllers.SlackBotEventHandler,
) *SlackSocketWorker {
	return &SlackSocketWorker{
		botID:              botID,
		appTokenSecretName: appTokenSecretName,
		appTokenSecretKey:  appTokenSecretKey,
		botTokenSecretName: botTokenSecretName,
		botTokenSecretKey:  botTokenSecretKey,
		channelResolver:    channelResolver,
		eventHandler:       eventHandler,
	}
}

// Run connects to Slack via Socket Mode and processes events until ctx is cancelled.
// The Slack SDK handles reconnection automatically.
func (w *SlackSocketWorker) Run(ctx context.Context) {
	log.Printf("[SOCKET_WORKER] Starting for botID=%s", w.botID)

	// Load App-level token (xapp-...)
	appToken, err := w.channelResolver.GetBotToken(ctx, w.appTokenSecretName, w.appTokenSecretKey)
	if err != nil {
		log.Printf("[SOCKET_WORKER] Failed to load app token for botID=%s: %v", w.botID, err)
		return
	}

	// Load bot token (xoxb-...)
	botToken, err := w.channelResolver.GetBotToken(ctx, w.botTokenSecretName, w.botTokenSecretKey)
	if err != nil {
		log.Printf("[SOCKET_WORKER] Failed to load bot token for botID=%s: %v", w.botID, err)
		return
	}

	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
	)
	client := socketmode.New(api)

	// Dispatch events in a separate goroutine
	go w.handleEvents(ctx, client)

	// RunContext blocks until ctx is cancelled or connection fails
	if err := client.RunContext(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[SOCKET_WORKER] Socket Mode client exited for botID=%s: %v", w.botID, err)
	}

	log.Printf("[SOCKET_WORKER] Stopped for botID=%s", w.botID)
}

// handleEvents dispatches Socket Mode events to the event handler
func (w *SlackSocketWorker) handleEvents(ctx context.Context, client *socketmode.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-client.Events:
			if !ok {
				return
			}
			w.dispatchEvent(ctx, client, evt)
		}
	}
}

// dispatchEvent handles a single Socket Mode event
func (w *SlackSocketWorker) dispatchEvent(ctx context.Context, client *socketmode.Client, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		log.Printf("[SOCKET_WORKER] Connecting to Slack: botID=%s", w.botID)

	case socketmode.EventTypeConnected:
		log.Printf("[SOCKET_WORKER] Connected to Slack: botID=%s", w.botID)

	case socketmode.EventTypeConnectionError:
		log.Printf("[SOCKET_WORKER] Connection error: botID=%s, data=%v", w.botID, evt.Data)

	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			log.Printf("[SOCKET_WORKER] Unexpected EventsAPI data type: %T", evt.Data)
			client.Ack(*evt.Request)
			return
		}
		// Acknowledge immediately to prevent Slack from retrying
		client.Ack(*evt.Request)
		// Process asynchronously so we don't block the event loop
		go w.processEventsAPIEvent(ctx, eventsAPIEvent)

	default:
		// Acknowledge unknown event types to prevent connection issues
		if evt.Request != nil {
			client.Ack(*evt.Request)
		}
	}
}

// processEventsAPIEvent converts a Slack SDK event to our internal payload format
// and delegates to the event handler
func (w *SlackSocketWorker) processEventsAPIEvent(ctx context.Context, eventsAPIEvent slackevents.EventsAPIEvent) {
	// Marshal the SDK event to JSON then unmarshal into our internal payload struct.
	// This decouples SDK types from our domain logic.
	raw, err := json.Marshal(eventsAPIEvent)
	if err != nil {
		log.Printf("[SOCKET_WORKER] Failed to marshal Slack event: botID=%s, err=%v", w.botID, err)
		return
	}

	var payload controllers.SlackPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[SOCKET_WORKER] Failed to unmarshal Slack event: botID=%s, err=%v", w.botID, err)
		return
	}

	if payload.Event == nil {
		log.Printf("[SOCKET_WORKER] No inner event found: botID=%s, type=%s", w.botID, payload.Type)
		return
	}

	log.Printf("[SOCKET_WORKER] Received event: botID=%s, type=%s, eventType=%s, channel=%s",
		w.botID, payload.Type, payload.Event.Type, payload.Event.Channel)

	if err := w.eventHandler.ProcessEvent(ctx, w.botID, payload); err != nil {
		log.Printf("[SOCKET_WORKER] Failed to process event: botID=%s, err=%v", w.botID, err)
	}
}
