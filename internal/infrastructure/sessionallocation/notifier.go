package sessionallocation

import (
	"context"
	"sync"

	"github.com/redis/go-redis/v9"
)

const notifyTopic = "agentapi:session-allocation:notify"

type LocalNotifier struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewLocalNotifier() *LocalNotifier {
	return &LocalNotifier{subs: make(map[chan struct{}]struct{})}
}

func (n *LocalNotifier) Notify(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func (n *LocalNotifier) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	cancel := func() {
		n.mu.Lock()
		if _, ok := n.subs[ch]; ok {
			delete(n.subs, ch)
			close(ch)
		}
		n.mu.Unlock()
	}
	return ch, cancel, nil
}

type RedisNotifier struct {
	client *redis.Client
	local  *LocalNotifier
}

func NewRedisNotifier(client *redis.Client) *RedisNotifier {
	return &RedisNotifier{client: client, local: NewLocalNotifier()}
}

func (n *RedisNotifier) Notify(ctx context.Context) error {
	_ = n.local.Notify(ctx)
	if n.client == nil {
		return nil
	}
	return n.client.Publish(ctx, notifyTopic, "ping").Err()
}

func (n *RedisNotifier) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if n.client == nil {
		return n.local.Subscribe(ctx)
	}
	localCh, localCancel, _ := n.local.Subscribe(ctx)
	pubsub := n.client.Subscribe(ctx, notifyTopic)
	if _, err := pubsub.Receive(ctx); err != nil {
		localCancel()
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(out)
		redisCh := pubsub.Channel()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case _, ok := <-localCh:
				if !ok {
					localCh = nil
					continue
				}
				select {
				case out <- struct{}{}:
				default:
				}
			case _, ok := <-redisCh:
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	cancel := func() {
		close(done)
		localCancel()
		_ = pubsub.Close()
	}
	return out, cancel, nil
}
