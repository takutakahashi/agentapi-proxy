package slackbot

import (
	"context"
	"encoding/json"
	"log"
	"strings"

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

	// botUserID is the Slack user ID of this bot (e.g. "U01234567").
	// Resolved via auth.test during Run() and used to filter incoming events:
	// only app_mention events or messages that explicitly mention <@botUserID>
	// are forwarded to the event handler.
	// If empty (auth.test failed), mention filtering is disabled as a fallback.
	botUserID string
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

	// Resolve our own Slack user ID so we can filter events to only those
	// that mention this bot, preventing it from reacting to every channel message.
	if authResp, err := api.AuthTestContext(ctx); err != nil {
		log.Printf("[SOCKET_WORKER] Failed to resolve bot user ID via auth.test for botID=%s: %v (mention filtering disabled)", w.botID, err)
	} else {
		w.botUserID = authResp.UserID
		log.Printf("[SOCKET_WORKER] Resolved bot user ID: botID=%s, userID=%s", w.botID, w.botUserID)
	}

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
			log.Printf("[SOCKET_WORKER] Raw event from Slack: botID=%s type=%s", w.botID, evt.Type)
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

	case socketmode.EventTypeHello:
		// Hello is sent by Slack after the WebSocket connection is established.
		// It is informational only and must NOT be acknowledged (no envelope_id).
		if evt.Request != nil {
			log.Printf("[SOCKET_WORKER] Hello from Slack: botID=%s num_connections=%d host=%s",
				w.botID, evt.Request.NumConnections, evt.Request.DebugInfo.Host)
		}

	case socketmode.EventTypeEventsAPI:
		log.Printf("[SOCKET_WORKER] EventsAPI event received: botID=%s", w.botID)
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
		// For unknown event types, only ack if there is a valid envelope ID.
		// Events like "hello" and "disconnect" have no envelope_id and must not be acked.
		if evt.Request != nil && evt.Request.EnvelopeID != "" {
			client.Ack(*evt.Request)
		}
	}
}

// processEventsAPIEvent converts a Slack SDK event to our internal payload format
// and delegates to the event handler
func (w *SlackSocketWorker) processEventsAPIEvent(ctx context.Context, eventsAPIEvent slackevents.EventsAPIEvent) {
	// Marshal eventsAPIEvent.Data (which is *EventsAPICallbackEvent for event_callback type)
	// rather than eventsAPIEvent itself.
	//
	// EventsAPIEvent has Data and InnerEvent fields with NO json tags, so they would
	// serialize as "Data" and "InnerEvent" (Go field names). EventsAPICallbackEvent,
	// on the other hand, has InnerEvent *json.RawMessage `json:"event"` which is
	// the "event" key that our SlackPayload struct expects.
	if eventsAPIEvent.Data == nil {
		log.Printf("[SOCKET_WORKER] EventsAPI event has no data: botID=%s, type=%s", w.botID, eventsAPIEvent.Type)
		return
	}
	raw, err := json.Marshal(eventsAPIEvent.Data)
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

	// Mention filter: only process events that are directed at this bot.
	// - app_mention: Slack already guarantees the bot was @-mentioned → always process.
	// - message: only process if the text contains <@botUserID> (explicit mention).
	// - other event types (e.g. reaction_added): pass through unchanged.
	// If botUserID is empty (auth.test failed at startup), skip the filter as a fallback.
	if w.botUserID != "" {
		evt := payload.Event
		if evt.Type == "message" && !strings.Contains(evt.Text, "<@"+w.botUserID+">") {
			log.Printf("[SOCKET_WORKER] Skipping message without mention: botID=%s userID=%s", w.botID, w.botUserID)
			return
		}
	}

	if err := w.eventHandler.ProcessEvent(ctx, w.botID, payload); err != nil {
		log.Printf("[SOCKET_WORKER] Failed to process event: botID=%s, err=%v", w.botID, err)
	}
}
