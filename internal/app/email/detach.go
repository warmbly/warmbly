package email

import (
	"context"
	"time"
)

// connectBudget bounds a single connect once it runs on its own: two silent workers, the save and the load.
const connectBudget = 2 * time.Minute

// bulkConnectBudget bounds a bulk batch, which checks its rows a few at a time.
const bulkConnectBudget = 5 * time.Minute

// detach keeps a connect running when the caller goes away (a refresh, a closed
// tab), so a checked mailbox is always saved, placed, announced and loaded, or
// not saved at all. An earlier deadline the caller set still applies.
func detach(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(budget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}
