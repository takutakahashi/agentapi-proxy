package controllers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// --- Mocks ---

// mockSessionManager implements repositories.SessionManager for testing
type mockSessionManager struct {
	createdSessions []*mockSession
	createErr       error
	sentMessages    []string
}

func (m *mockSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	sess := &mockSession{
		id:    id,
		tags:  req.Tags,
		scope: req.Scope,
	}
	m.createdSessions = append(m.createdSessions, sess)
	return sess, nil
}

func (m *mockSessionManager) GetSession(_ string) entities.Session { return nil }
func (m *mockSessionManager) DeleteSession(_ string) error         { return nil }
func (m *mockSessionManager) Shutdown(_ time.Duration) error       { return nil }

func (m *mockSessionManager) ListSessions(_ entities.SessionFilter) []entities.Session {
	return nil
}

func (m *mockSessionManager) SendMessage(_ context.Context, _ string, msg string) error {
	m.sentMessages = append(m.sentMessages, msg)
	return nil
}

func (m *mockSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}

// mockSession implements entities.Session
type mockSession struct {
	id    string
	tags  map[string]string
	scope entities.ResourceScope
}

func (s *mockSession) ID() string                    { return s.id }
func (s *mockSession) Addr() string                  { return "" }
func (s *mockSession) UserID() string                { return "" }
func (s *mockSession) Scope() entities.ResourceScope { return s.scope }
func (s *mockSession) TeamID() string                { return "" }
func (s *mockSession) Tags() map[string]string       { return s.tags }
func (s *mockSession) Status() string                { return "active" }
func (s *mockSession) StartedAt() time.Time          { return time.Time{} }
func (s *mockSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *mockSession) Description() string           { return "" }
func (s *mockSession) Cancel()                       {}

// --- Helpers ---

// computeSlackSig computes a valid Slack HMAC-SHA256 signature for tests
func computeSlackSig(signingSecret, timestamp, body string) string {
	baseStr := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseStr))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// buildSlackEventPayload builds a minimal event_callback JSON payload
func buildSlackEventPayload(channel, text string) string {
	return fmt.Sprintf(
		`{"type":"event_callback","team_id":"T1","event":{"type":"message","text":%q,"user":"U1","channel":%q,"ts":"123.456"}}`,
		text, channel,
	)
}

// buildSlackEventPayloadWithTs builds an event_callback payload with a specific event ts
func buildSlackEventPayloadWithTs(channel, text, ts string) string {
	return fmt.Sprintf(
		`{"type":"event_callback","team_id":"T1","event":{"type":"message","text":%q,"user":"U1","channel":%q,"ts":%q}}`,
		text, channel, ts,
	)
}

// newSlackEchoContext creates an echo.Context for a Slack event with proper HMAC signature.
func newSlackEchoContext(body, signingSecret, slackbotID string) (echo.Context, *httptest.ResponseRecorder) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeSlackSig(signingSecret, timestamp, body)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/"+slackbotID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues(slackbotID)
	return ctx, rec
}

// ---- Tests for resolveBotByChannel ----

func TestResolveBotByChannel_Success(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-test-channel"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	// Bot with matching AllowedChannelNames and no custom bot token
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "Dev Alerts Bot", "user-1")
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	// BotTokenSecretName = "" → uses default
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, channelID)
	require.NotNil(t, resolved, "should resolve bot by channel name")
	assert.Equal(t, "bot-uuid-1", resolved.ID())
}

func TestResolveBotByChannel_NoMatch(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-other-channel"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "general"}, // "dev" not in "general"
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "Dev Bot", "user-1")
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"})
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, channelID)
	assert.Nil(t, resolved, "should not match any bot when channel name doesn't match AllowedChannelNames")
}

func TestResolveBotByChannel_BotWithCustomToken_Skipped(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "Custom Token Bot", "user-1")
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"})
	bot.SetBotTokenSecretName("custom-k8s-secret") // has custom bot token → must be skipped
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, channelID)
	assert.Nil(t, resolved, "bot with custom bot token must not be matched via default endpoint")
}

func TestResolveBotByChannel_NilResolver_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	// channelResolver = nil → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "secret-name", "bot-token", nil, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, "C-some-channel")
	assert.Nil(t, resolved, "should return nil when channelResolver is nil")
}

func TestResolveBotByChannel_EmptyDefaultTokenSecret_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	fakeClient := fake.NewSimpleClientset()
	resolver := services.NewSlackChannelResolver(fakeClient, "test-ns")
	// defaultBotTokenSecretName = "" → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "", "bot-token", resolver, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, "C-some-channel")
	assert.Nil(t, resolved, "should return nil when defaultBotTokenSecretName is empty")
}

func TestResolveBotByChannel_EmptyAllowedChannelNames_Skipped(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "All Channels Bot", "user-1")
	bot.SetSigningSecret("signing-secret")
	// AllowedChannelNames is empty → not identifiable via channel filter
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/hooks/slack/default", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	resolved := handler.resolveBotByChannel(ctx, channelID)
	assert.Nil(t, resolved, "bot with empty AllowedChannelNames cannot be identified via default endpoint")
}

// ---- Tests for HandleSlackEvent with id="default" ----

// TestHandleSlackEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID verifies that when an event
// arrives at /hooks/slack/default, the handler identifies the correct registered bot by channel
// name and tags the session with the bot's UUID instead of "default".
func TestHandleSlackEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID(t *testing.T) {
	const (
		namespace     = "test-ns"
		secretName    = "bot-token-secret"
		signingSecret = "default-signing-secret"
		channelID     = "C-dev-ch"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	// Registered bot using default credentials with AllowedChannelNames
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("registered-bot-uuid", "Dev Bot", "user-1")
	bot.SetSigningSecret(signingSecret)
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/dev-team")
	repo.bots["registered-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, signingSecret, secretName, "bot-token", resolver, "")

	body := buildSlackEventPayload(channelID, "hello bot")
	ctx, rec := newSlackEchoContext(body, signingSecret, "default")

	err := handler.HandleSlackEvent(ctx)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify the session was created with the correct bot's UUID tag (not "default")
	require.Len(t, sessionMgr.createdSessions, 1, "should have created one session")
	createdTags := sessionMgr.createdSessions[0].tags
	assert.Equal(t, "registered-bot-uuid", createdTags["slackbot_id"],
		"session must be tagged with the registered bot's UUID, not 'default'")

	// Verify the response slackbot_id
	var respBody map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	assert.Equal(t, "registered-bot-uuid", respBody["slackbot_id"],
		"response slackbot_id must be the registered bot's UUID")
}

// TestHandleSlackEvent_DefaultID_BotIsPaused_Rejected verifies that when a matched bot is paused,
// the event is rejected with "bot paused" message and no session is created.
func TestHandleSlackEvent_DefaultID_BotIsPaused_Rejected(t *testing.T) {
	const (
		namespace     = "test-ns"
		secretName    = "bot-token-secret"
		signingSecret = "default-signing-secret"
		channelID     = "C-dev-ch"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-ch"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("paused-bot-uuid", "Paused Bot", "user-1")
	bot.SetSigningSecret(signingSecret)
	bot.SetAllowedChannelNames([]string{"dev"})
	bot.SetStatus(entities.SlackBotStatusPaused) // bot is paused
	repo.bots["paused-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, signingSecret, secretName, "bot-token", resolver, "")

	body := buildSlackEventPayload(channelID, "hello")
	ctx, rec := newSlackEchoContext(body, signingSecret, "default")

	err := handler.HandleSlackEvent(ctx)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var respBody map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	assert.Equal(t, "bot paused", respBody["message"],
		"paused bot should return 'bot paused' message even when accessed via default endpoint")
	assert.Empty(t, sessionMgr.createdSessions, "no session should be created for paused bot")
}

// ---- Tests for event deduplication ----

// TestEventDedupeCache_DuplicateWithinTTL verifies that the same key is recognized
// as a duplicate within the TTL window.
func TestEventDedupeCache_DuplicateWithinTTL(t *testing.T) {
	cache := newEventDedupeCache(5 * time.Minute)

	assert.False(t, cache.isDuplicate("C1:111.000"), "first occurrence should not be a duplicate")
	assert.True(t, cache.isDuplicate("C1:111.000"), "second occurrence within TTL should be a duplicate")
	assert.True(t, cache.isDuplicate("C1:111.000"), "third occurrence within TTL should be a duplicate")
}

// TestEventDedupeCache_DifferentKeys verifies that different keys are not deduplicated.
func TestEventDedupeCache_DifferentKeys(t *testing.T) {
	cache := newEventDedupeCache(5 * time.Minute)

	assert.False(t, cache.isDuplicate("C1:111.000"), "first key should not be a duplicate")
	assert.False(t, cache.isDuplicate("C2:111.000"), "different channel should not be a duplicate")
	assert.False(t, cache.isDuplicate("C1:222.000"), "different thread should not be a duplicate")
}

// TestEventDedupeCache_AfterTTLExpiry verifies that entries are not considered duplicates
// after the TTL has expired.
func TestEventDedupeCache_AfterTTLExpiry(t *testing.T) {
	cache := newEventDedupeCache(10 * time.Millisecond) // very short TTL

	assert.False(t, cache.isDuplicate("C1:111.000"), "first occurrence should not be a duplicate")
	time.Sleep(20 * time.Millisecond) // wait for TTL to expire
	assert.False(t, cache.isDuplicate("C1:111.000"), "occurrence after TTL expiry should not be a duplicate")
}

// TestHandleSlackEvent_Dedup_SecondEventIgnored verifies that a duplicate Slack event
// (same channel + thread key within TTL) is ignored with HTTP 200 and no new session.
func TestHandleSlackEvent_Dedup_SecondEventIgnored(t *testing.T) {
	const (
		signingSecret = "dedup-test-secret"
		channelID     = "C-dedup"
	)

	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, signingSecret, "", "", nil, "")

	body := buildSlackEventPayloadWithTs(channelID, "hello", "111.111")

	// First request: should create a session
	ctx1, rec1 := newSlackEchoContext(body, signingSecret, "default")
	require.NoError(t, handler.HandleSlackEvent(ctx1))
	assert.Equal(t, http.StatusOK, rec1.Code)
	require.Len(t, sessionMgr.createdSessions, 1, "first event should create a session")

	// Second request: same channel + ts (threadKey) → should be ignored
	ctx2, rec2 := newSlackEchoContext(body, signingSecret, "default")
	require.NoError(t, handler.HandleSlackEvent(ctx2))
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Len(t, sessionMgr.createdSessions, 1, "duplicate event should not create another session")

	var respBody map[string]string
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &respBody))
	assert.Equal(t, "duplicate event ignored", respBody["message"])
}

// TestHandleSlackEvent_Dedup_DifferentThreadsAllowed verifies that events from different
// threads in the same channel are each processed (not deduplicated against each other).
func TestHandleSlackEvent_Dedup_DifferentThreadsAllowed(t *testing.T) {
	const (
		signingSecret = "dedup-test-secret2"
		channelID     = "C-dedup2"
	)

	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, signingSecret, "", "", nil, "")

	// Two distinct messages (different ts) in the same channel
	body1 := buildSlackEventPayloadWithTs(channelID, "msg1", "111.111")
	body2 := buildSlackEventPayloadWithTs(channelID, "msg2", "222.222")

	ctx1, rec1 := newSlackEchoContext(body1, signingSecret, "default")
	require.NoError(t, handler.HandleSlackEvent(ctx1))
	assert.Equal(t, http.StatusOK, rec1.Code)

	ctx2, rec2 := newSlackEchoContext(body2, signingSecret, "default")
	require.NoError(t, handler.HandleSlackEvent(ctx2))
	assert.Equal(t, http.StatusOK, rec2.Code)

	assert.Len(t, sessionMgr.createdSessions, 2, "events with different thread keys should each create a session")
}

// TestHandleSlackEvent_DefaultID_NoBotMatch_FallsThrough verifies that when no registered bot
// matches the channel, the event is processed with id="default" (fallback behavior).
func TestHandleSlackEvent_DefaultID_NoBotMatch_FallsThrough(t *testing.T) {
	const (
		namespace     = "test-ns"
		secretName    = "bot-token-secret"
		signingSecret = "default-signing-secret"
		channelID     = "C-other"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "general"},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	// Bot only allows "dev-alerts", not "general" → no match
	bot := entities.NewSlackBot("dev-bot-uuid", "Dev Bot", "user-1")
	bot.SetSigningSecret(signingSecret)
	bot.SetAllowedChannelNames([]string{"dev-alerts"})
	repo.bots["dev-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, signingSecret, secretName, "bot-token", resolver, "")

	body := buildSlackEventPayload(channelID, "hello")
	ctx, rec := newSlackEchoContext(body, signingSecret, "default")

	err := handler.HandleSlackEvent(ctx)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// When no bot is matched, session is created with fallback slackbot_id="default"
	require.Len(t, sessionMgr.createdSessions, 1, "session should still be created with default fallback")
	createdTags := sessionMgr.createdSessions[0].tags
	assert.Equal(t, "default", createdTags["slackbot_id"],
		"when no bot is matched, session should be tagged with 'default'")
}
