package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestLeaderElectorAcquiresAndReleasesRedisLease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	config := DefaultLeaderElectionConfig("test")
	config.LeaseDuration = time.Second
	config.RenewDeadline = 20 * time.Millisecond
	elector := NewLeaderElector(client, config)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		elector.Run(ctx, func(leaderCtx context.Context) { close(started); <-leaderCtx.Done() }, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("lease was not acquired")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("elector did not stop")
	}
	if server.Exists("agentapi:leader:test:" + ScheduleWorkerLeaseName) {
		t.Fatal("lease was not released")
	}
}
