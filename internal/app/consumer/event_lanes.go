package jobs

import (
	"context"
	"os"
	"strconv"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

// maxEventLanes is the most mailboxes one consumer handles at once.
const maxEventLanes = 64

// WorkerEventConcurrency is how many mailboxes' worker events one consumer handles at once:
// CONSUMER_EVENT_CONCURRENCY when set, otherwise one per pooled database connection.
func WorkerEventConcurrency(poolConns int32) int {
	if n, err := strconv.Atoi(os.Getenv("CONSUMER_EVENT_CONCURRENCY")); err == nil && n >= 1 && n <= maxEventLanes {
		return n
	}
	return min(max(int(poolConns), 1), maxEventLanes)
}

// workerEventKey keeps one mailbox's events in order: workers publish them under the mailbox id,
// the same key the broker partitions on.
func workerEventKey(_ context.Context, msg eventbus.Message) (string, error) {
	return msg.Key, nil
}
