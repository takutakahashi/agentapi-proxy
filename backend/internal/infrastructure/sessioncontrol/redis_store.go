package sessioncontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
)

const (
	defaultMaxLen = int64(10000)
	streamTTL     = 30 * time.Minute
	connectionTTL = 75 * time.Second
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore { return &RedisStore{client: client} }

func commandKey(sessionID string) string { return "agentapi:session:{" + sessionID + "}:commands" }
func eventKey(sessionID string) string   { return "agentapi:session:{" + sessionID + "}:events" }
func commandAckKey(sessionID string) string {
	return "agentapi:session:{" + sessionID + "}:command-ack"
}
func connectionKey(sessionID string) string {
	return "agentapi:session:{" + sessionID + "}:control-connection"
}
func eventDedupKey(sessionID, eventID string) string {
	return "agentapi:session:{" + sessionID + "}:event:" + eventID
}

var appendEventScript = redis.NewScript(`
if redis.call('SET', KEYS[2], '1', 'NX', 'EX', ARGV[3]) then
  local id = redis.call('XADD', KEYS[1], 'MAXLEN', '~', ARGV[2], '*', 'event', ARGV[1])
  redis.call('EXPIRE', KEYS[1], ARGV[3])
  return id
end
return ''
`)

func (s *RedisStore) TouchConnection(ctx context.Context, sessionID string) error {
	if err := s.client.Set(ctx, connectionKey(sessionID), time.Now().UTC().Format(time.RFC3339Nano), connectionTTL).Err(); err != nil {
		return fmt.Errorf("touch session control connection: %w", err)
	}
	return nil
}

func (s *RedisStore) IsConnected(ctx context.Context, sessionID string) (bool, error) {
	exists, err := s.client.Exists(ctx, connectionKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("check session control connection: %w", err)
	}
	return exists > 0, nil
}

func (s *RedisStore) EnqueueCommand(ctx context.Context, sessionID string, command core.Command) (string, error) {
	payload, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("marshal session command: %w", err)
	}
	key := commandKey(sessionID)
	id, err := s.client.XAdd(ctx, &redis.XAddArgs{Stream: key, MaxLen: defaultMaxLen, Approx: true, Values: map[string]interface{}{"command": payload}}).Result()
	if err != nil {
		return "", fmt.Errorf("append session command: %w", err)
	}
	_ = s.client.Expire(ctx, key, streamTTL).Err()
	return id, nil
}

func (s *RedisStore) ReadCommands(ctx context.Context, sessionID, after string, wait time.Duration, count int64) ([]core.Command, error) {
	ack, err := s.client.Get(ctx, commandAckKey(sessionID)).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("read session command ack: %w", err)
	}
	if streamIDLess(after, ack) {
		after = ack
	}
	msgs, err := s.read(ctx, commandKey(sessionID), after, wait, count)
	if err != nil {
		return nil, err
	}
	commands := make([]core.Command, 0, len(msgs))
	for _, msg := range msgs {
		var command core.Command
		if err := decodeField(msg, "command", &command); err != nil {
			return nil, err
		}
		command.StreamID = msg.ID
		commands = append(commands, command)
	}
	return commands, nil
}

func (s *RedisStore) AckCommand(ctx context.Context, sessionID, streamID string) error {
	if streamID == "" {
		return nil
	}
	key := commandAckKey(sessionID)
	current, err := s.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("read session command ack: %w", err)
	}
	if !streamIDLess(current, streamID) {
		return nil
	}
	if err := s.client.Set(ctx, key, streamID, streamTTL).Err(); err != nil {
		return fmt.Errorf("write session command ack: %w", err)
	}
	return nil
}

func (s *RedisStore) AppendEvents(ctx context.Context, sessionID string, events []core.Event) (string, error) {
	key := eventKey(sessionID)
	lastID := ""
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return "", fmt.Errorf("marshal session event: %w", err)
		}
		id, err := appendEventScript.Run(ctx, s.client, []string{key, eventDedupKey(sessionID, event.ID)}, payload, defaultMaxLen, int64(streamTTL/time.Second)).Text()
		if err != nil {
			return "", fmt.Errorf("append session event: %w", err)
		}
		if id != "" {
			lastID = id
		}
	}
	return lastID, nil
}

func (s *RedisStore) ReadEvents(ctx context.Context, sessionID, after string, wait time.Duration, count int64) ([]core.Event, error) {
	msgs, err := s.read(ctx, eventKey(sessionID), after, wait, count)
	if err != nil {
		return nil, err
	}
	events := make([]core.Event, 0, len(msgs))
	for _, msg := range msgs {
		var event core.Event
		if err := decodeField(msg, "event", &event); err != nil {
			return nil, err
		}
		event.StreamID = msg.ID
		events = append(events, event)
	}
	return events, nil
}

func (s *RedisStore) read(ctx context.Context, key, after string, wait time.Duration, count int64) ([]redis.XMessage, error) {
	if after == "" {
		after = "0-0"
	}
	if count <= 0 || count > 100 {
		count = 100
	}
	result, err := s.client.XRead(ctx, &redis.XReadArgs{Streams: []string{key, after}, Count: count, Block: wait}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session control stream: %w", err)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result[0].Messages, nil
}

func decodeField(msg redis.XMessage, field string, target interface{}) error {
	raw, ok := msg.Values[field]
	if !ok {
		return fmt.Errorf("session control stream entry %s has no %s field", msg.ID, field)
	}
	var data []byte
	switch value := raw.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("session control stream entry %s has invalid %s field", msg.ID, field)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode session control stream entry %s: %w", msg.ID, err)
	}
	return nil
}

func streamIDLess(left, right string) bool {
	if right == "" {
		return false
	}
	if left == "" || left == "0-0" {
		return true
	}
	var leftMS, leftSeq, rightMS, rightSeq int64
	if _, err := fmt.Sscanf(left, "%d-%d", &leftMS, &leftSeq); err != nil {
		return true
	}
	if _, err := fmt.Sscanf(right, "%d-%d", &rightMS, &rightSeq); err != nil {
		return false
	}
	return leftMS < rightMS || (leftMS == rightMS && leftSeq < rightSeq)
}
