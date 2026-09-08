package jobs

import "context"

// stopContext derives a context that ends when the scheduler's Stop() closes
// stopCh, so a jobrun loop keeps the old Stop() semantics.
func stopContext(ctx context.Context, stopCh <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
