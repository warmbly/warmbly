package jobs

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

func TestWorkerEventConcurrencyFollowsThePoolUnlessSet(t *testing.T) {
	for _, c := range []struct {
		env  string
		pool int32
		want int
	}{
		{"", 25, 25}, {"", 4, 4}, {"", 0, 1}, {"", 200, 64},
		{"1", 25, 1}, {"64", 4, 64}, {"0", 25, 25}, {"65", 25, 25}, {"many", 10, 10},
	} {
		t.Setenv("CONSUMER_EVENT_CONCURRENCY", c.env)
		if got := WorkerEventConcurrency(c.pool); got != c.want {
			t.Errorf("CONSUMER_EVENT_CONCURRENCY=%q pool=%d: got %d lanes, want %d", c.env, c.pool, got, c.want)
		}
	}
}

func TestWorkerEventsShareALaneOnlyWithTheirMailbox(t *testing.T) {
	a, _ := workerEventKey(context.Background(), eventbus.Message{Key: "mailbox-a"})
	b, _ := workerEventKey(context.Background(), eventbus.Message{Key: "mailbox-b"})
	again, _ := workerEventKey(context.Background(), eventbus.Message{Key: "mailbox-a"})
	if a != again || a == b {
		t.Fatalf("lanes: a=%q again=%q b=%q", a, again, b)
	}
}
