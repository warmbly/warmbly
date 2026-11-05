package jobs

import (
	"context"
	"os"
	"strconv"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

// WorkerEventConcurrency is how many mailboxes' worker events one consumer handles at once.
func WorkerEventConcurrency() int {
	if n, err := strconv.Atoi(os.Getenv("CONSUMER_EVENT_CONCURRENCY")); err == nil && n >= 1 && n <= 64 {
		return n
	}
	return 8
}

// workerEventKey keeps one mailbox's events in order: workers publish them under the mailbox id,
// the same key the broker partitions on.
func workerEventKey(_ context.Context, msg eventbus.Message) (string, error) {
	return msg.Key, nil
}
