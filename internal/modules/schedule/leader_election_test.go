package schedule

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection"
)

func TestLeaderElectorRunRetriesAfterLeadershipLoss(t *testing.T) {
	config := DefaultLeaderElectionConfig("test")
	config.RetryPeriod = time.Millisecond
	elector := NewLeaderElector(fake.NewSimpleClientset(), config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	retried := make(chan struct{})
	elector.run = func(ctx context.Context, config leaderelection.LeaderElectionConfig) {
		switch runs.Add(1) {
		case 1:
			config.Callbacks.OnStartedLeading(ctx)
			config.Callbacks.OnStoppedLeading()
		case 2:
			close(retried)
			<-ctx.Done()
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		elector.Run(ctx, func(context.Context) {}, func() {})
	}()

	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Fatal("leader election did not restart after leadership loss")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("leader election did not stop after context cancellation")
	}

	if got := runs.Load(); got != 2 {
		t.Fatalf("election runs = %d, want 2", got)
	}
}
