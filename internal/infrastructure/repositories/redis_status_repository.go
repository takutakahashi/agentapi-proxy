package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// redisStatusKeyPrefix is the prefix for session status hash keys.
	redisStatusKeyPrefix = "agentapi:session:status:"
	// redisGlobalChannel is the Pub/Sub channel name for all status change events.
	redisGlobalChannel = "agentapi:session:events:global"
	// redisStatusTTL is the TTL applied to session status keys.
	// It is intentionally longer than any realistic session lifetime.
	redisStatusTTL = 48 * time.Hour

	// redisSessionListCachePrefix is the prefix for session-list cache keys.
	// Key format: agentapi:sessions:list:{namespace}:{labelSelectorHash}
	redisSessionListCachePrefix = "agentapi:sessions:list:"
)

// redisStatusFields are the hash field names used within a status key.
const (
	fieldStatus    = "status"
	fieldUpdatedAt = "updated_at"
	fieldPodID     = "pod_id"
)

// RedisStatusRepository implements StatusEventRepository using Redis.
// It stores session runtime status in Redis hashes and broadcasts changes
// via a single Redis Pub/Sub channel so all proxy pods stay in sync.
type RedisStatusRepository struct {
	client *redis.Client
	podID  string
}

// NewRedisStatusRepository creates a RedisStatusRepository connected to the
// given Redis addr.  podID should uniquely identify the caller within the
// cluster (e.g. the pod hostname).
func NewRedisStatusRepository(client *redis.Client, podID string) *RedisStatusRepository {
	return &RedisStatusRepository{
		client: client,
		podID:  podID,
	}
}

func statusKey(sessionID string) string {
	return redisStatusKeyPrefix + sessionID
}

// SetStatus persists status, updated_at and pod_id into a Redis hash and
// refreshes the key TTL.
func (r *RedisStatusRepository) SetStatus(ctx context.Context, sessionID, status, podID string) error {
	key := statusKey(sessionID)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key,
		fieldStatus, status,
		fieldUpdatedAt, now,
		fieldPodID, podID,
	)
	pipe.Expire(ctx, key, redisStatusTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis SetStatus %s: %w", sessionID, err)
	}
	return nil
}

// GetStatus retrieves the current status stored for sessionID.
// Returns ("", time.Time{}, nil) when the key does not exist.
func (r *RedisStatusRepository) GetStatus(ctx context.Context, sessionID string) (string, time.Time, error) {
	key := statusKey(sessionID)
	vals, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("redis GetStatus %s: %w", sessionID, err)
	}
	if len(vals) == 0 {
		return "", time.Time{}, nil
	}

	status := vals[fieldStatus]
	updatedAt := time.Time{}
	if ts, ok := vals[fieldUpdatedAt]; ok && ts != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, ts); parseErr == nil {
			updatedAt = t
		}
	}
	return status, updatedAt, nil
}

// PublishStatusChange serialises event to JSON and publishes it to the global
// Redis Pub/Sub channel.
func (r *RedisStatusRepository) PublishStatusChange(ctx context.Context, event portrepos.StatusChangeEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("redis PublishStatusChange marshal: %w", err)
	}
	if err := r.client.Publish(ctx, redisGlobalChannel, payload).Err(); err != nil {
		return fmt.Errorf("redis PublishStatusChange publish: %w", err)
	}
	return nil
}

// SubscribeGlobal subscribes to the global status-change channel and returns a
// Go channel that receives deserialized StatusChangeEvent values.
// The returned channel is closed when ctx is cancelled.
// Messages that cannot be deserialized are logged and skipped.
// Slow consumers will drop messages rather than block the publisher.
func (r *RedisStatusRepository) SubscribeGlobal(ctx context.Context) (<-chan portrepos.StatusChangeEvent, error) {
	pubsub := r.client.Subscribe(ctx, redisGlobalChannel)

	// Perform an initial ping to verify the subscription was established.
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("redis SubscribeGlobal subscribe: %w", err)
	}

	ch := make(chan portrepos.StatusChangeEvent, 256)
	go func() {
		defer func() {
			_ = pubsub.Close()
			close(ch)
		}()
		msgCh := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				var event portrepos.StatusChangeEvent
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					log.Printf("[REDIS_STATUS] failed to unmarshal event: %v", err)
					continue
				}
				select {
				case ch <- event:
				default:
					// Drop the event if the consumer is slow.
					log.Printf("[REDIS_STATUS] subscriber channel full, dropping event session=%s status=%s",
						event.SessionID, event.Status)
				}
			}
		}
	}()

	return ch, nil
}

// DeleteStatus removes the status key for sessionID from Redis.
func (r *RedisStatusRepository) DeleteStatus(ctx context.Context, sessionID string) error {
	if err := r.client.Del(ctx, statusKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("redis DeleteStatus %s: %w", sessionID, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// SessionListCacheRepository implementation
// --------------------------------------------------------------------------

func sessionListCachePattern(namespace string) string {
	return redisSessionListCachePrefix + namespace + ":*"
}

// SetSessionListCache serialises sessions to JSON and stores them in Redis
// under cacheKey with ttl expiry.
func (r *RedisStatusRepository) SetSessionListCache(ctx context.Context, cacheKey string, sessions []portrepos.CachedSessionDTO, ttl time.Duration) error {
	payload, err := json.Marshal(sessions)
	if err != nil {
		return fmt.Errorf("redis SetSessionListCache marshal: %w", err)
	}
	if err := r.client.Set(ctx, cacheKey, payload, ttl).Err(); err != nil {
		return fmt.Errorf("redis SetSessionListCache set %s: %w", cacheKey, err)
	}
	return nil
}

// GetSessionListCache retrieves DTOs stored under cacheKey.
// Returns (nil, nil) on a cache miss.
func (r *RedisStatusRepository) GetSessionListCache(ctx context.Context, cacheKey string) ([]portrepos.CachedSessionDTO, error) {
	payload, err := r.client.Get(ctx, cacheKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // cache miss
		}
		return nil, fmt.Errorf("redis GetSessionListCache get %s: %w", cacheKey, err)
	}
	var sessions []portrepos.CachedSessionDTO
	if err := json.Unmarshal(payload, &sessions); err != nil {
		return nil, fmt.Errorf("redis GetSessionListCache unmarshal: %w", err)
	}
	return sessions, nil
}

// InvalidateSessionListCache deletes all session-list cache entries whose key
// matches the agentapi:sessions:list:{namespace}:* pattern.  It uses SCAN to
// avoid blocking the Redis server on large key spaces.
func (r *RedisStatusRepository) InvalidateSessionListCache(ctx context.Context, namespace string) error {
	pattern := sessionListCachePattern(namespace)
	var cursor uint64
	var keysToDelete []string

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis InvalidateSessionListCache scan: %w", err)
		}
		keysToDelete = append(keysToDelete, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(keysToDelete) == 0 {
		return nil
	}
	if err := r.client.Del(ctx, keysToDelete...).Err(); err != nil {
		return fmt.Errorf("redis InvalidateSessionListCache del: %w", err)
	}
	log.Printf("[REDIS_CACHE] Invalidated %d session-list cache entries for namespace=%s", len(keysToDelete), namespace)
	return nil
}
