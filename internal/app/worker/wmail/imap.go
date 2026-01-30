package wmail

import (
	"context"
	"time"
)

func (w *WMail) StartImapWorker(ctx context.Context) {
	ticker := time.NewTicker(ImapCheckInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			return
		}
	}
}
