package jobs

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

func TestWorkerEventConcurrencyDefaultsAndBounds(t *testing.T) {
	for raw, want := range map[string]int{"": 8, "1": 1, "64": 64, "0": 8, "65": 8, "many": 8} {
		t.Setenv("CONSUMER_EVENT_CONCURRENCY", raw)
		if got := WorkerEventConcurrency(); got != want {
			t.Errorf("CONSUMER_EVENT_CONCURRENCY=%q: got %d lanes, want %d", raw, got, want)
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
