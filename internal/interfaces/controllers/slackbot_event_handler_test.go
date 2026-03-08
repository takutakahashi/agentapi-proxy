package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
	mu               sync.Mutex
	createdSessions  []*mockSession
	createErr        error
	sentMessages     []string
	stoppedSessions  []string           // IDs of sessions stopped via StopAgent
	existingSessions []entities.Session // pre-seeded sessions returned by ListSessions
	createDelay      time.Duration      // optional delay to simulate slow session creation
}

func (m *mockSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	if m.createDelay > 0 {
		time.Sleep(m.createDelay)
	}
	if m.createErr != nil {
		return nil, m.createErr
	}
	sess := &mockSession{
		id:             id,
		tags:           req.Tags,
		scope:          req.Scope,
		initialMessage: req.InitialMessage,
	}
	m.mu.Lock()
	m.createdSessions = append(m.createdSessions, sess)
	m.mu.Unlock()
	return sess, nil
}

func (m *mockSessionManager) GetSession(_ string) entities.Session { return nil }
func (m *mockSessionManager) DeleteSession(_ string) error         { return nil }
func (m *mockSessionManager) Shutdown(_ time.Duration) error       { return nil }

func (m *mockSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	var result []entities.Session
	for _, s := range m.existingSessions {
		if filter.Status != "" && s.Status() != filter.Status {
			continue
		}
		if len(filter.Tags) > 0 {
			match := true
			for k, v := range filter.Tags {
				if s.Tags()[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}
		result = append(result, s)
	}
	return result
}

func (m *mockSessionManager) SendMessage(_ context.Context, _ string, msg string) error {
	m.mu.Lock()
	m.sentMessages = append(m.sentMessages, msg)
	m.mu.Unlock()
	return nil
}

func (m *mockSessionManager) StopAgent(_ context.Context, id string) error {
	m.mu.Lock()
	m.stoppedSessions = append(m.stoppedSessions, id)
	m.mu.Unlock()
	return nil
}

// stoppedCount returns the number of sessions stopped so far (thread-safe).
func (m *mockSessionManager) stoppedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stoppedSessions)
}

// sentCount returns the number of messages sent so far (thread-safe).
func (m *mockSessionManager) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sentMessages)
}

// getSentMessage returns the sent message at index i (thread-safe).
func (m *mockSessionManager) getSentMessage(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentMessages[i]
}

func (m *mockSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}

// createdCount returns the number of sessions created so far (thread-safe).
func (m *mockSessionManager) createdCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.createdSessions)
}

// getCreatedSession returns the created session at index i (thread-safe).
func (m *mockSessionManager) getCreatedSession(i int) *mockSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createdSessions[i]
}

// mockSession implements entities.Session
type mockSession struct {
	id             string
	tags           map[string]string
	scope          entities.ResourceScope
	status         string // defaults to "active" when empty
	initialMessage string // captured from RunServerRequest.InitialMessage
}

func (s *mockSession) ID() string                    { return s.id }
func (s *mockSession) Addr() string                  { return "" }
func (s *mockSession) UserID() string                { return "" }
func (s *mockSession) Scope() entities.ResourceScope { return s.scope }
func (s *mockSession) TeamID() string                { return "" }
func (s *mockSession) Tags() map[string]string       { return s.tags }
func (s *mockSession) Status() string {
	if s.status == "" {
		return "active"
	}
	return s.status
}
func (s *mockSession) StartedAt() time.Time     { return time.Time{} }
func (s *mockSession) UpdatedAt() time.Time     { return time.Time{} }
func (s *mockSession) LastMessageAt() time.Time { return time.Time{} }
func (s *mockSession) Description() string      { return "" }
func (s *mockSession) Cancel()                  {}

// --- Helpers ---

// waitForCondition polls fn up to maxWait in pollInterval steps.
func waitForCondition(maxWait, pollInterval time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return fn()
}

// buildEventPayload builds a minimal SlackPayload for testing.
func buildEventPayload(channel, text string) SlackPayload {
	return SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			Text:    text,
			User:    "U1",
			Channel: channel,
			Ts:      "123.456",
		},
	}
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
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	// BotTokenSecretName = "" → uses default
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
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
	bot.SetAllowedChannelNames([]string{"dev"})
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
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
	bot.SetAllowedChannelNames([]string{"dev"})
	bot.SetBotTokenSecretName("custom-k8s-secret") // has custom bot token → must be skipped
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with custom bot token must not be matched via default endpoint")
}

func TestResolveBotByChannel_NilResolver_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	// channelResolver = nil → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "secret-name", "bot-token", nil, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), "C-some-channel")
	assert.Nil(t, resolved, "should return nil when channelResolver is nil")
}

func TestResolveBotByChannel_EmptyDefaultTokenSecret_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	fakeClient := fake.NewSimpleClientset()
	resolver := services.NewSlackChannelResolver(fakeClient, "test-ns")
	// defaultBotTokenSecretName = "" → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), "C-some-channel")
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
	// AllowedChannelNames is empty → not identifiable via channel filter
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with empty AllowedChannelNames cannot be identified via default endpoint")
}

// ---- Tests for ProcessEvent ----

// TestProcessEvent_NonEventCallbackIgnored verifies that non-event_callback payloads are silently ignored.
func TestProcessEvent_NonEventCallbackIgnored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{Type: "url_verification", Event: nil}
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)
	assert.Equal(t, 0, sessionMgr.createdCount(), "url_verification should be ignored")
}

// TestProcessEvent_BasicEvent_CreatesSession verifies that a basic message event causes
// a session to be created asynchronously.
func TestProcessEvent_BasicEvent_CreatesSession(t *testing.T) {
	const (
		botID     = "basic-bot-uuid"
		channelID = "C-basic"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Basic Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created asynchronously")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, botID, createdTags["slackbot_id"])
	assert.Equal(t, channelID, createdTags["slack_channel"])
}

// TestProcessEvent_PausedBot_Skipped verifies that a paused bot causes the event to be ignored.
func TestProcessEvent_PausedBot_Skipped(t *testing.T) {
	const botID = "paused-bot-uuid"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Paused Bot", "user-1")
	bot.SetStatus(entities.SlackBotStatusPaused)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-paused", "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	// Give goroutine a chance (should never fire for paused bot)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no session should be created for paused bot")
}

// TestProcessEvent_EventTypeNotAllowed verifies that events with disallowed types are ignored.
func TestProcessEvent_EventTypeNotAllowed(t *testing.T) {
	const botID = "event-filter-bot"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Event Filter Bot", "user-1")
	bot.SetAllowedEventTypes([]string{"app_mention"}) // only app_mention; "message" should be filtered
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-filter", "hello")
	// payload.Event.Type is "message", which is not in allowed list
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "disallowed event type should be filtered")
}

// TestProcessEvent_RegisteredBotTaggedCorrectly verifies that session tags contain
// the bot's UUID and proper scope/teamID from the registered bot.
func TestProcessEvent_RegisteredBotTaggedCorrectly(t *testing.T) {
	const (
		botID     = "registered-bot-uuid"
		channelID = "C-tagged"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Tagged Bot", "user-1")
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/dev-team")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, botID, sess.tags["slackbot_id"])
	assert.Equal(t, channelID, sess.tags["slack_channel"])
	assert.Equal(t, entities.ScopeTeam, sess.scope)
}

// TestProcessEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID verifies that when
// ProcessEvent is called with botID="default", it identifies the correct registered bot
// by channel name and tags the session with the bot's UUID.
func TestProcessEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev-ch"
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
	bot := entities.NewSlackBot("registered-bot-uuid", "Dev Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/dev-team")
	repo.bots["registered-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "registered-bot-uuid", createdTags["slackbot_id"],
		"session must be tagged with the registered bot's UUID, not 'default'")
}

// TestProcessEvent_DefaultID_NoBotMatch_DropsEvent verifies that when no registered bot
// matches the channel, the event is dropped (no session is created).
func TestProcessEvent_DefaultID_NoBotMatch_DropsEvent(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-other"
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
	bot.SetAllowedChannelNames([]string{"dev-alerts"})
	repo.bots["dev-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	// No session should be created when no registered bot matches the channel.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(),
		"event should be dropped when no registered bot matches the channel")
}

// TestProcessEvent_ThreadTs_UsedAsThreadKey verifies that when thread_ts is present,
// it is used as the slack_thread_ts tag instead of ts.
func TestProcessEvent_ThreadTs_UsedAsThreadKey(t *testing.T) {
	const (
		botID     = "thread-bot-uuid"
		channelID = "C-thread"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Thread Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "reply in thread",
			User:     "U1",
			Channel:  channelID,
			Ts:       "200.001",
			ThreadTs: "100.000", // reply to thread root
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "100.000", createdTags["slack_thread_ts"],
		"thread_ts should be used as the thread key when present")
}

// TestProcessEvent_SessionLimit_Reached verifies that when a bot has reached its session limit,
// ProcessEvent returns an error and does not create a new session.
func TestProcessEvent_SessionLimit_Reached(t *testing.T) {
	const botID = "limited-bot"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Limited Bot", "user-1")
	bot.SetMaxSessions(2)
	repo.bots[botID] = bot

	// Pre-seed 2 sessions (at the limit). Note: these sessions must NOT include
	// the slack_channel/slack_thread_ts tags so the duplicate-session check passes first.
	existingSessions := []entities.Session{
		&mockSession{id: "s1", tags: map[string]string{"slackbot_id": botID}},
		&mockSession{id: "s2", tags: map[string]string{"slackbot_id": botID}},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-limited", "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	assert.Error(t, err, "should return error when session limit is reached")
	assert.Contains(t, err.Error(), "session limit reached")
}

// TestProcessEvent_BotMessage_BotID_Ignored verifies that events with a non-empty bot_id
// (i.e. messages posted by bots) are silently ignored to prevent recursive session creation.
func TestProcessEvent_BotMessage_BotID_Ignored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			BotID:   "B-some-bot-id", // bot-posted message
			Text:    "セッションを作成しました :robot_face:\nhttp://example.com/sessions/abc",
			Channel: "C-channel",
			Ts:      "111.222",
		},
	}

	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "bot message (bot_id set) must be ignored to prevent recursion")
}

// TestProcessEvent_BotMessage_Subtype_Ignored verifies that events with subtype="bot_message"
// are silently ignored even when bot_id is empty.
func TestProcessEvent_BotMessage_Subtype_Ignored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			SubType: "bot_message", // bot_message subtype
			Text:    "some bot reply",
			Channel: "C-channel",
			Ts:      "111.333",
		},
	}

	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "bot_message subtype must be ignored to prevent recursion")
}

// TestProcessEvent_PausedBot_NeverResponds verifies that a paused bot never responds to any event,
// even when allow_bot_messages=true. The paused state is a hard guardrail.
func TestProcessEvent_PausedBot_NeverResponds(t *testing.T) {
	const botID = "paused-allow-bot-messages-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Paused Allow Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	bot.SetStatus(entities.SlackBotStatusPaused)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Test 1: bot message (allow_bot_messages=true, but paused → must NOT respond)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			BotID:   "B-some-bot-id",
			Text:    "<@U-bot> hello",
			Channel: "C-channel",
			Ts:      "300.001",
		},
	}
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "paused bot must never respond, even when allow_bot_messages=true")

	// Test 2: human message (paused → must NOT respond)
	payload2 := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			Text:    "<@U-bot> hello from human",
			User:    "U-human",
			Channel: "C-channel",
			Ts:      "300.002",
		},
	}
	err = handler.ProcessEvent(context.Background(), botID, payload2)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "paused bot must never respond to human messages either")
}

// TestProcessEvent_BotMessage_AllowBotMessages_BotID verifies that when allow_bot_messages=true,
// messages with a non-empty bot_id are processed and a session is created.
func TestProcessEvent_BotMessage_AllowBotMessages_BotID(t *testing.T) {
	const botID = "allow-bot-messages-bot-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Allow Bot Messages Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			BotID:   "B-some-bot-id", // bot-posted message
			Text:    "<@U-bot> こんにちは",
			Channel: "C-channel",
			Ts:      "200.001",
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sessionMgr.createdCount(), "bot message must be processed when allow_bot_messages=true")
}

// TestProcessEvent_BotMessage_AllowBotMessages_Subtype verifies that when allow_bot_messages=true,
// messages with subtype="bot_message" are processed and a session is created.
func TestProcessEvent_BotMessage_AllowBotMessages_Subtype(t *testing.T) {
	const botID = "allow-bot-messages-subtype-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Allow Bot Messages Subtype Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			SubType: "bot_message",
			Text:    "bot からのメッセージ",
			Channel: "C-channel",
			Ts:      "200.002",
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sessionMgr.createdCount(), "bot_message subtype must be processed when allow_bot_messages=true")
}

// TestProcessEvent_ReuseSession_RoutesToExistingSession verifies that a follow-up message in the
// same channel+thread is routed to the existing active session via SendMessage, not a new session.
func TestProcessEvent_ReuseSession_RoutesToExistingSession(t *testing.T) {
	const (
		botID     = "reuse-bot-uuid"
		channelID = "C-reuse"
		threadTS  = "600.000"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Reuse Bot", "user-1")
	repo.bots[botID] = bot

	// Pre-seed an existing active session for the same channel+thread
	existingSessions := []entities.Session{
		&mockSession{
			id:     "existing-session-id",
			status: "active",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
			},
		},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "follow-up message",
			User:     "U1",
			Channel:  channelID,
			Ts:       "600.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.sentCount() == 1
	})
	require.True(t, ok, "message should be routed to existing session")
	assert.Equal(t, "follow-up message", sessionMgr.getSentMessage(0))
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created when reusing")
}

// TestProcessEvent_ReuseSession_NewSessionWhenNoActive verifies that a new session is created
// when no active session exists for the channel+thread, even if terminated sessions exist.
func TestProcessEvent_ReuseSession_NewSessionWhenNoActive(t *testing.T) {
	const (
		botID     = "newthread-bot-uuid"
		channelID = "C-newthread"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "NewThread Bot", "user-1")
	repo.bots[botID] = bot

	// No pre-seeded sessions → should always create a new session
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := buildEventPayload(channelID, "first message")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "new session should be created when no active session exists")
	assert.Equal(t, 0, sessionMgr.sentCount(), "no message should be sent to existing session")
}

// TestProcessEvent_ChannelNameResolveError_ReturnsError verifies that when channel name
// resolution fails (e.g. the bot token is empty/invalid), ProcessEvent returns an error
// instead of allowing the event through as it did previously.
func TestProcessEvent_ChannelNameResolveError_ReturnsError(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		botID      = "channel-filter-bot"
		channelID  = "C-unknown"
	)

	// Secret exists but has an empty token value.
	// GetBotToken will succeed returning "", then fetchFromSlack fails with
	// "bot token is empty; cannot call Slack API" — no real HTTP call is made.
	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("")},
		},
		// No ConfigMap → cache miss → falls through to Slack API (which fails)
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Channel Filter Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"})
	// No custom bot token secret → uses defaultBotTokenSecretName
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), botID, payload)

	assert.Error(t, err, "ProcessEvent should return error when channel name cannot be resolved")
	assert.Contains(t, err.Error(), "failed to resolve channel name")

	// Ensure no session was created despite the resolve failure
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no session should be created when channel name resolution fails")
}

// TestProcessEvent_ConcurrentDuplicateEvents verifies that when Slack emits both "message"
// and "app_mention" for the same @mention (same channel+thread), only one session is created.
// This mirrors real Slack behaviour where both events arrive within milliseconds of each other,
// before any session exists, so the reuse-check alone cannot catch the duplicate.
func TestProcessEvent_ConcurrentDuplicateEvents(t *testing.T) {
	const (
		botID     = "concurrent-bot-uuid"
		channelID = "C-concurrent"
		threadTS  = "700.000"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Concurrent Bot", "user-1")
	repo.bots[botID] = bot

	// Use a createDelay so that the pendingThreads key is still present when the second
	// goroutine reaches LoadOrStore. Without the delay the mock CreateSession completes
	// instantly, the key gets deleted, and the second event slips through the dedup guard.
	sessionMgr := &mockSessionManager{createDelay: 50 * time.Millisecond}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	makePayload := func(eventType string) SlackPayload {
		return SlackPayload{
			Type:   "event_callback",
			TeamID: "T1",
			Event: &SlackEvent{
				Type:    eventType,
				Text:    "@bot hello",
				User:    "U1",
				Channel: channelID,
				Ts:      threadTS,
			},
		}
	}

	// Use a start barrier to ensure both goroutines reach ProcessEvent simultaneously,
	// before the first one's async session creation goroutine can complete.
	var ready, start sync.WaitGroup
	ready.Add(2)
	start.Add(1)

	var wg sync.WaitGroup
	wg.Add(2)

	// Fire "message" and "app_mention" concurrently, simulating Slack's duplicate delivery
	go func() {
		defer wg.Done()
		ready.Done()
		start.Wait()
		_ = handler.ProcessEvent(context.Background(), botID, makePayload("message"))
	}()
	go func() {
		defer wg.Done()
		ready.Done()
		start.Wait()
		_ = handler.ProcessEvent(context.Background(), botID, makePayload("app_mention"))
	}()

	ready.Wait() // wait until both goroutines are staged
	start.Done() // release both simultaneously

	wg.Wait()

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "exactly one session should be created even when two events fire concurrently")
}

// ---- Tests for /stop command ----

// TestIsStopCommand verifies that isStopCommand correctly identifies /stop messages
// with or without bot mention tokens.
func TestIsStopCommand(t *testing.T) {
	cases := []struct {
		text     string
		expected bool
	}{
		{"/stop", true},
		{"  /stop  ", true},
		{"<@U1234567> /stop", true},
		{"<@U1234567|botname> /stop", true},
		{"/stop extra", false},
		{"stop", false},
		{"hello /stop", false},
		{"", false},
		{"<@U1234567>", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			assert.Equal(t, tc.expected, isStopCommand(tc.text))
		})
	}
}

// TestProcessEvent_StopCommand_StopsExistingSession verifies that a /stop message
// calls StopAgent on the active session for the same channel+thread.
func TestProcessEvent_StopCommand_StopsExistingSession(t *testing.T) {
	const (
		botID     = "stop-bot-uuid"
		channelID = "C-stop"
		threadTS  = "800.000"
		sessionID = "existing-stop-session"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Bot", "user-1")
	repo.bots[botID] = bot

	existingSessions := []entities.Session{
		&mockSession{
			id:     sessionID,
			status: "active",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
			},
		},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "/stop",
			User:     "U1",
			Channel:  channelID,
			Ts:       "800.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.stoppedCount() == 1
	})
	require.True(t, ok, "StopAgent should be called for the active session")

	sessionMgr.mu.Lock()
	stoppedID := sessionMgr.stoppedSessions[0]
	sessionMgr.mu.Unlock()
	assert.Equal(t, sessionID, stoppedID, "the active session should be stopped")
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop")
	assert.Equal(t, 0, sessionMgr.sentCount(), "no message should be sent to existing session for /stop")
}

// TestProcessEvent_StopCommand_WithMention verifies that /stop with a bot mention is also handled.
func TestProcessEvent_StopCommand_WithMention(t *testing.T) {
	const (
		botID     = "stop-mention-bot-uuid"
		channelID = "C-stop-mention"
		threadTS  = "900.000"
		sessionID = "existing-mention-session"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Mention Bot", "user-1")
	repo.bots[botID] = bot

	existingSessions := []entities.Session{
		&mockSession{
			id:     sessionID,
			status: "active",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
			},
		},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "<@UBOTID> /stop",
			User:     "U1",
			Channel:  channelID,
			Ts:       "900.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.stoppedCount() == 1
	})
	require.True(t, ok, "StopAgent should be called even when message contains a bot mention")
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop with mention")
}

// TestProcessEvent_StopCommand_NoActiveSession verifies that /stop when there is no
// active session does not crash and does not create a new session.
func TestProcessEvent_StopCommand_NoActiveSession(t *testing.T) {
	const (
		botID     = "stop-empty-bot-uuid"
		channelID = "C-stop-empty"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Empty Bot", "user-1")
	repo.bots[botID] = bot

	// No existing sessions
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "/stop")

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop with no active session")
	assert.Equal(t, 0, sessionMgr.stoppedCount(), "StopAgent should not be called when there is no active session")
}

// ---- Tests for thread context feature ----

// buildThreadRepliesHandler returns an httptest handler that serves a mock conversations.replies response.
func buildThreadRepliesHandler(channel, threadTS string, messages []services.SlackMessage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.replies" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("channel") != channel || r.URL.Query().Get("ts") != threadTS {
			http.Error(w, "unexpected channel/ts", http.StatusBadRequest)
			return
		}
		resp := struct {
			OK       bool                    `json:"ok"`
			Messages []services.SlackMessage `json:"messages"`
		}{OK: true, Messages: messages}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}
}

// TestProcessEvent_ThreadContext_AvailableInPayload verifies that when the triggering
// message is already inside an existing Slack thread (thread_ts != ""), thread_messages
// is fetched and made available as a template variable.
func TestProcessEvent_ThreadContext_PrependedWhenNoTemplate(t *testing.T) {
	const (
		botID      = "thread-ctx-bot-uuid"
		channelID  = "C-thread-ctx"
		threadTS   = "1700000000.000001"
		namespace  = "test-ns"
		secretName = "bot-token-secret"
	)

	threadMessages := []services.SlackMessage{
		{User: "U-alice", Text: "Hey team, can someone look at this?", Ts: threadTS},
		{User: "U-bob", Text: "Sure, what's the issue?", Ts: "1700000001.000001"},
	}

	// Mux for the mock Slack API server: handle both conversations.replies and chat.postMessage
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", buildThreadRepliesHandler(channelID, threadTS, threadMessages))
	// Stub chat.postMessage to avoid noise
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	})
	// Stub conversations.info for channel name resolution (not needed here but prevent 404 noise)
	mux.HandleFunc("/conversations.info", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": true, "channel": map[string]string{"id": channelID, "name": "test-channel"}}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	})
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Thread Ctx Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	// Triggering message is a reply in an existing thread
	triggerTs := "1700000002.000001"
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "@bot please help",
			User:     "U-alice",
			Channel:  channelID,
			Ts:       triggerTs,
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	// Verify the initial message passed to CreateSession contains the full thread history.
	sessionMgr.mu.Lock()
	createdSession := sessionMgr.createdSessions[0]
	sessionMgr.mu.Unlock()

	assert.Equal(t, channelID, createdSession.tags["slack_channel"])
	assert.Equal(t, threadTS, createdSession.tags["slack_thread_ts"])

	// Without a template, the initial message is just the triggering event text.
	// thread_messages is available via payloadMap for templates that reference {{ .thread_messages }}.
	assert.Equal(t, "@bot please help", createdSession.initialMessage,
		"without a template, initial message should be the plain event text")
}

// TestProcessEvent_ThreadContext_NoResolver_DoesNotCrash verifies that when no channel
// resolver is configured, a message in a thread still creates a session normally (no crash,
// no thread context - just the plain message text).
func TestProcessEvent_ThreadContext_NoResolver_DoesNotCrash(t *testing.T) {
	const (
		botID     = "thread-no-resolver-bot"
		channelID = "C-no-resolver"
		threadTS  = "1700100000.000001"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Resolver Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	// No channel resolver → fetchAndFormatThreadContext returns "" immediately
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "@bot help me",
			User:     "U-user",
			Channel:  channelID,
			Ts:       "1700100001.000001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should still be created even when thread context cannot be fetched")
}

// TestFetchAndFormatThreadContext_FiltersMessagesAfterUntilTs verifies that messages with
// ts > untilTS are excluded from the formatted context string.
func TestFetchAndFormatThreadContext_FiltersMessagesAfterUntilTs(t *testing.T) {
	const (
		channelID  = "C-filter-ts"
		threadTS   = "1700200000.000001"
		namespace  = "test-ns"
		secretName = "bot-secret"
	)
	untilTS := "1700200001.000001" // only include messages up to this ts

	allMessages := []services.SlackMessage{
		{User: "U-a", Text: "first", Ts: "1700200000.000001"},
		{User: "U-b", Text: "second", Ts: "1700200001.000001"}, // = untilTS, should be included
		{User: "U-a", Text: "third", Ts: "1700200002.000001"},  // > untilTS, should be excluded
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", buildThreadRepliesHandler(channelID, threadTS, allMessages))
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-token")},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-id", "Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots["bot-id"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	result := handler.fetchAndFormatThreadContext(context.Background(), bot, channelID, threadTS, untilTS)

	assert.NotEmpty(t, result, "context should not be empty")
	assert.True(t, strings.Contains(result, "first"), "result should contain first message")
	assert.True(t, strings.Contains(result, "second"), "result should contain second message (ts == untilTS)")
	assert.False(t, strings.Contains(result, "third"), "result should NOT contain third message (ts > untilTS)")
}

// TestPostSessionURLToSlack_RepositoryInMessage verifies that when a repository is detected,
// the Slack notification message includes the repository name.
func TestPostSessionURLToSlack_RepositoryInMessage(t *testing.T) {
	const (
		botID      = "notify-repo-bot"
		channelID  = "C-notify"
		threadTS   = "1700300000.000001"
		sessionID  = "session-abc"
		namespace  = "test-ns"
		secretName = "bot-token-secret"
	)

	// Capture the posted Slack message (JSON body with "text" field).
	var capturedMessage string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			capturedMessage = body.Text
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Notify Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	baseURL := "https://example.com"
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, baseURL, false, nil)

	handler.postSessionURLToSlack(context.Background(), channelID, threadTS, sessionID, "myorg/myrepo", bot)

	assert.Contains(t, capturedMessage, "myorg/myrepo",
		"notification message should include the repository name")
	assert.Contains(t, capturedMessage, sessionID,
		"notification message should include the session ID")
}

// TestPostSessionURLToSlack_NoRepositoryDefaultMessage verifies that when no repository is set,
// the notification message uses the default format without repository info.
func TestPostSessionURLToSlack_NoRepositoryDefaultMessage(t *testing.T) {
	const (
		botID      = "notify-no-repo-bot"
		channelID  = "C-notify-noRepo"
		threadTS   = "1700300001.000001"
		sessionID  = "session-xyz"
		namespace  = "test-ns"
		secretName = "bot-token-secret2"
	)

	var capturedMessage string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			capturedMessage = body.Text
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
	)
	resolver := services.NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Repo Notify Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "https://example.com", false, nil)

	handler.postSessionURLToSlack(context.Background(), channelID, threadTS, sessionID, "", bot)

	assert.NotContains(t, capturedMessage, "repository",
		"notification message should NOT include 'repository' when none is detected")
	assert.Contains(t, capturedMessage, sessionID,
		"notification message should still include the session ID")
}

// TestProcessEvent_RepositoryTag_MultiLine verifies that when the first line of a Slack message
// is in "org/repo" format, the "repository" session tag is automatically set.
func TestProcessEvent_RepositoryTag_MultiLine(t *testing.T) {
	const (
		botID     = "repo-tag-bot"
		channelID = "C-repo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Repo Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// First line is "org/repo", rest is the task description.
	payload := buildEventPayload(channelID, "myorg/myrepo\nPlease fix the bug in the authentication module.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"repository tag should be automatically set from the first line")
}

// TestProcessEvent_RepositoryTag_SingleLine verifies that a single-line "org/repo" message
// also sets the repository tag correctly.
func TestProcessEvent_RepositoryTag_SingleLine(t *testing.T) {
	const (
		botID     = "repo-tag-single-bot"
		channelID = "C-single"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Single Line Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "myorg/myrepo")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"single-line org/repo message should set repository tag")
}

// TestProcessEvent_RepositoryTag_WithMention verifies that bot mentions are stripped
// before the "org/repo" check, so "@bot myorg/myrepo" still sets the repository tag.
func TestProcessEvent_RepositoryTag_WithMention(t *testing.T) {
	const (
		botID     = "repo-tag-mention-bot"
		channelID = "C-mention"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Mention Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Slack app_mention events include the mention token in the text.
	payload := buildEventPayload(channelID, "<@UBOTID> myorg/myrepo\nPlease fix the login bug.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"mention should be stripped and org/repo should be set as repository tag")
}

// TestProcessEvent_RepositoryTag_NotSet verifies that when the first line is not in
// "org/repo" format, the repository tag is NOT set.
func TestProcessEvent_RepositoryTag_NotSet(t *testing.T) {
	const (
		botID     = "repo-tag-none-bot"
		channelID = "C-noRepo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Repo Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "こんにちは！バグを直してほしいんですが")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	_, hasRepo := sess.tags["repository"]
	assert.False(t, hasRepo, "repository tag should NOT be set when first line is not org/repo format")
}

// TestParseRepository unit-tests the parseRepository helper directly.
func TestParseRepository(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"org/repo only", "myorg/myrepo", "myorg/myrepo"},
		{"org/repo with body", "myorg/myrepo\nPlease fix the bug.", "myorg/myrepo"},
		{"mention then org/repo", "<@UBOTID> myorg/myrepo\nFix it.", "myorg/myrepo"},
		{"mention only", "<@UBOTID> myorg/myrepo", "myorg/myrepo"},
		{"plain text", "こんにちは！", ""},
		{"plain english text", "Please help me", ""},
		{"only mention", "<@UBOTID>", ""},
		{"mention with plain text", "<@UBOTID> hello world", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRepository(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
