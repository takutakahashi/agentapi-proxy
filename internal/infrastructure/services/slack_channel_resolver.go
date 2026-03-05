package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// slackChannelCacheConfigMapName is the ConfigMap name for persisting channel ID→name mappings
	slackChannelCacheConfigMapName = "agentapi-slack-channel-cache"
	// slackChannelCacheLabel is the label applied to the cache ConfigMap
	slackChannelCacheLabel = "agentapi.proxy/type"
	// slackChannelCacheLabelValue is the label value for the cache ConfigMap
	slackChannelCacheLabelValue = "slack-channel-cache"
	// slackDefaultAPIBase is the default Slack API base URL
	slackDefaultAPIBase = "https://slack.com/api"
)

// SlackChannelResolver resolves Slack channel IDs to names using the Slack API,
// with a two-level cache: in-memory (sync.Map) and a Kubernetes ConfigMap for persistence.
type SlackChannelResolver struct {
	kubeClient   kubernetes.Interface
	namespace    string
	slackAPIBase string // base URL for the Slack API, e.g. "https://slack.com/api"
	// in-memory cache: channel ID → channel name (cleared on pod restart)
	cache sync.Map
}

// NewSlackChannelResolver creates a new SlackChannelResolver
func NewSlackChannelResolver(kubeClient kubernetes.Interface, namespace string) *SlackChannelResolver {
	return &SlackChannelResolver{
		kubeClient:   kubeClient,
		namespace:    namespace,
		slackAPIBase: slackDefaultAPIBase,
	}
}

// WithSlackAPIBase overrides the Slack API base URL. Intended for testing only;
// production callers should use the default (https://slack.com/api).
func (r *SlackChannelResolver) WithSlackAPIBase(base string) *SlackChannelResolver {
	r.slackAPIBase = base
	return r
}

// ResolveChannelName resolves a Slack channel ID to its name.
// Resolution order:
//  1. In-memory cache
//  2. Kubernetes ConfigMap (persistent)
//  3. Slack API conversations.info (requires bot token with channels:read / groups:read scope)
func (r *SlackChannelResolver) ResolveChannelName(ctx context.Context, channelID, botToken string) (string, error) {
	// 1. In-memory cache
	if v, ok := r.cache.Load(channelID); ok {
		return v.(string), nil
	}

	// 2. ConfigMap cache
	name, found, err := r.loadFromConfigMap(ctx, channelID)
	if err != nil {
		log.Printf("[CHANNEL_RESOLVER] ConfigMap read error: %v", err)
		// non-fatal: proceed to API call
	}
	if found {
		r.cache.Store(channelID, name)
		return name, nil
	}

	// 3. Slack API
	name, err = r.fetchFromSlack(ctx, channelID, botToken)
	if err != nil {
		return "", fmt.Errorf("failed to resolve channel %s from Slack API: %w", channelID, err)
	}

	// Update both caches
	r.cache.Store(channelID, name)
	if upsertErr := r.upsertConfigMap(ctx, channelID, name); upsertErr != nil {
		log.Printf("[CHANNEL_RESOLVER] Failed to update ConfigMap cache: %v", upsertErr)
		// non-fatal: return the resolved name anyway
	}

	return name, nil
}

// GetBotToken retrieves the Slack bot token from a Kubernetes Secret.
func (r *SlackChannelResolver) GetBotToken(ctx context.Context, secretName, secretKey string) (string, error) {
	if secretName == "" {
		return "", fmt.Errorf("bot token secret name is empty")
	}
	if secretKey == "" {
		secretKey = "bot-token"
	}

	secret, err := r.kubeClient.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	token, ok := secret.Data[secretKey]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s", secretKey, secretName)
	}

	return string(token), nil
}

// loadFromConfigMap looks up a channel ID in the persistent ConfigMap cache.
// Returns (name, true, nil) on hit, ("", false, nil) on miss, ("", false, err) on error.
func (r *SlackChannelResolver) loadFromConfigMap(ctx context.Context, channelID string) (string, bool, error) {
	cm, err := r.kubeClient.CoreV1().ConfigMaps(r.namespace).Get(ctx, slackChannelCacheConfigMapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}

	if cm.Data == nil {
		return "", false, nil
	}
	name, ok := cm.Data[channelID]
	return name, ok, nil
}

// upsertConfigMap creates or updates the cache ConfigMap with the given channel ID→name mapping.
func (r *SlackChannelResolver) upsertConfigMap(ctx context.Context, channelID, channelName string) error {
	cm, err := r.kubeClient.CoreV1().ConfigMaps(r.namespace).Get(ctx, slackChannelCacheConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get ConfigMap: %w", err)
		}
		// Create new ConfigMap
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      slackChannelCacheConfigMapName,
				Namespace: r.namespace,
				Labels: map[string]string{
					slackChannelCacheLabel: slackChannelCacheLabelValue,
				},
			},
			Data: map[string]string{
				channelID: channelName,
			},
		}
		_, err = r.kubeClient.CoreV1().ConfigMaps(r.namespace).Create(ctx, newCM, metav1.CreateOptions{})
		return err
	}

	// Update existing ConfigMap
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[channelID] = channelName
	_, err = r.kubeClient.CoreV1().ConfigMaps(r.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// slackConversationsInfoResponse represents the Slack API response for conversations.info
type slackConversationsInfoResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Channel struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
}

// fetchFromSlack calls the Slack API to get a channel's name by its ID.
func (r *SlackChannelResolver) fetchFromSlack(ctx context.Context, channelID, botToken string) (string, error) {
	if botToken == "" {
		return "", fmt.Errorf("bot token is empty; cannot call Slack API")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.slackAPIBase+"/conversations.info", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("channel", channelID)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack API request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[CHANNEL_RESOLVER] Failed to close response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Slack API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result slackConversationsInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse Slack API response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}

	if result.Channel.Name == "" {
		return "", fmt.Errorf("slack API returned empty channel name for ID %s", channelID)
	}

	log.Printf("[CHANNEL_RESOLVER] Resolved channel %s → %s", channelID, result.Channel.Name)
	return result.Channel.Name, nil
}

// SlackMessage represents a single Slack message returned by conversations.replies.
type SlackMessage struct {
	User    string `json:"user"`
	BotID   string `json:"bot_id,omitempty"`
	Text    string `json:"text"`
	Ts      string `json:"ts"`
	SubType string `json:"subtype,omitempty"`
}

// slackConversationsRepliesResponse represents the Slack API response for conversations.replies.
type slackConversationsRepliesResponse struct {
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Messages []SlackMessage `json:"messages"`
}

// FetchThreadReplies fetches all messages in a Slack thread using the conversations.replies API.
// channel is the Slack channel ID and threadTS is the root message timestamp.
// Returns messages sorted by timestamp (oldest first), including the root message.
// Requires a bot token with channels:history or groups:history scope.
func (r *SlackChannelResolver) FetchThreadReplies(ctx context.Context, channel, threadTS, botToken string) ([]SlackMessage, error) {
	if botToken == "" {
		return nil, fmt.Errorf("bot token is empty; cannot call Slack API")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.slackAPIBase+"/conversations.replies", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("channel", channel)
	q.Set("ts", threadTS)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack API request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[CHANNEL_RESOLVER] Failed to close FetchThreadReplies response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Slack API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result slackConversationsRepliesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Slack API response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("slack API error: %s", result.Error)
	}

	log.Printf("[CHANNEL_RESOLVER] Fetched %d messages from thread %s in channel %s", len(result.Messages), threadTS, channel)
	return result.Messages, nil
}

// postMessageRequest is the request body for chat.postMessage
type postMessageRequest struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

// postMessageResponse is the response from chat.postMessage
type postMessageResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// PostMessage posts a message to a Slack channel, optionally in a thread.
// If threadTS is non-empty, the message is posted as a thread reply.
// Requires a bot token with chat:write scope.
func (r *SlackChannelResolver) PostMessage(ctx context.Context, channel, threadTS, text, botToken string) error {
	if botToken == "" {
		return fmt.Errorf("bot token is empty; cannot post message to Slack")
	}

	payload := postMessageRequest{
		Channel:  channel,
		Text:     text,
		ThreadTS: threadTS,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.slackAPIBase+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack API request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[CHANNEL_RESOLVER] Failed to close PostMessage response body: %v", closeErr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Slack API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result postMessageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse Slack API response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	log.Printf("[CHANNEL_RESOLVER] Posted message to channel=%s thread=%s", channel, threadTS)
	return nil
}
