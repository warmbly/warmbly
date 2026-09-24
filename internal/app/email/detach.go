package email

import (
	"context"
	"time"
)

// connectBudget bounds a single connect once it runs on its own: two silent workers, the save and the load.
const connectBudget = 2 * time.Minute

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

// afterSaveBudget bounds the steps that follow a saved mailbox: placement, events, webhook, load.
const afterSaveBudget = 30 * time.Second

// afterSave is the context those steps run on, so a caller whose deadline ran
// out during the check cannot leave a saved mailbox unplaced or unannounced.
func afterSave(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), afterSaveBudget)
}
