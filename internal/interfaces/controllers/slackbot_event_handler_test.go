package controllers

import (
	"context"
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
	existingSessions []entities.Session // pre-seeded sessions returned by ListSessions
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
	m.mu.Lock()
	m.createdSessions = append(m.createdSessions, sess)
	m.mu.Unlock()
	return sess, nil
}

func (m *mockSessionManager) GetSession(_ string) entities.Session { return nil }
func (m *mockSessionManager) DeleteSession(_ string) error         { return nil }
func (m *mockSessionManager) Shutdown(_ time.Duration) error       { return nil }

func (m *mockSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	if len(filter.Tags) == 0 {
		return m.existingSessions
	}
	var result []entities.Session
	for _, s := range m.existingSessions {
		match := true
		for k, v := range filter.Tags {
			if s.Tags()[k] != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, s)
		}
	}
	return result
}

func (m *mockSessionManager) SendMessage(_ context.Context, _ string, msg string) error {
	m.sentMessages = append(m.sentMessages, msg)
	return nil
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
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	// BotTokenSecretName = "" → uses default
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

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
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"})
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

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
	bot.SetSigningSecret("signing-secret")
	bot.SetAllowedChannelNames([]string{"dev"})
	bot.SetBotTokenSecretName("custom-k8s-secret") // has custom bot token → must be skipped
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with custom bot token must not be matched via default endpoint")
}

func TestResolveBotByChannel_NilResolver_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	// channelResolver = nil → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "secret-name", "bot-token", nil, "")

	resolved := handler.resolveBotByChannel(context.Background(), "C-some-channel")
	assert.Nil(t, resolved, "should return nil when channelResolver is nil")
}

func TestResolveBotByChannel_EmptyDefaultTokenSecret_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	fakeClient := fake.NewSimpleClientset()
	resolver := services.NewSlackChannelResolver(fakeClient, "test-ns")
	// defaultBotTokenSecretName = "" → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "", "bot-token", resolver, "")

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
	bot.SetSigningSecret("signing-secret")
	// AllowedChannelNames is empty → not identifiable via channel filter
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", secretName, "bot-token", resolver, "")

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with empty AllowedChannelNames cannot be identified via default endpoint")
}

// ---- Tests for ProcessEvent ----

// TestProcessEvent_NonEventCallbackIgnored verifies that non-event_callback payloads are silently ignored.
func TestProcessEvent_NonEventCallbackIgnored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

	payload := SlackPayload{Type: "url_verification", Event: nil}
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)
	assert.Equal(t, 0, sessionMgr.createdCount(), "url_verification should be ignored")
}

// TestProcessEvent_BasicEvent_CreatesSession verifies that a basic message event causes
// a session to be created asynchronously.
func TestProcessEvent_BasicEvent_CreatesSession(t *testing.T) {
	const channelID = "C-basic"

	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created asynchronously")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "default", createdTags["slackbot_id"])
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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", secretName, "bot-token", resolver, "")

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

// TestProcessEvent_DefaultID_NoBotMatch_FallsThrough verifies that when no registered bot
// matches the channel, the event falls through and creates a session with slackbot_id="default".
func TestProcessEvent_DefaultID_NoBotMatch_FallsThrough(t *testing.T) {
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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", secretName, "bot-token", resolver, "")

	payload := buildEventPayload(channelID, "hello")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created with default fallback")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "default", createdTags["slackbot_id"],
		"when no bot is matched, session should be tagged with 'default'")
}

// TestProcessEvent_ThreadTs_UsedAsThreadKey verifies that when thread_ts is present,
// it is used as the slack_thread_ts tag instead of ts.
func TestProcessEvent_ThreadTs_UsedAsThreadKey(t *testing.T) {
	const channelID = "C-thread"

	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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

	err := handler.ProcessEvent(context.Background(), "default", payload)
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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", "", nil, "")

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

