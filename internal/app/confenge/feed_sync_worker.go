package confenge

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// FeedSyncWorker periodically pulls the configured extra-cli manifest when
// CONFENGE_FEED_SYNC_ENABLED=true. Single-flight is enforced inside SyncFeedManifest
// (process mutex + Postgres advisory lock when available).
type FeedSyncWorker struct {
	svc      Service
	orgID    uuid.UUID
	interval time.Duration
}

// NewFeedSyncWorker builds a background sync worker for one org (CONFENGE ops).
func NewFeedSyncWorker(svc Service, orgID uuid.UUID, interval time.Duration) *FeedSyncWorker {
	if interval < time.Minute {
		interval = 15 * time.Minute
	}
	return &FeedSyncWorker{svc: svc, orgID: orgID, interval: interval}
}

// Run blocks until ctx is cancelled. No-ops when feature flag off or URI empty.
func (w *FeedSyncWorker) Run(ctx context.Context) {
	if w == nil || w.svc == nil || !w.svc.Enabled() {
		return
	}
	cfg := w.svc.Config()
	if !cfg.FeedSyncEnabled {
		return
	}
	uri := strings.TrimSpace(cfg.ManifestURL)
	if uri == "" && strings.HasSuffix(strings.ToLower(cfg.FeedURL), "manifest.json") {
		uri = cfg.FeedURL
	}
	if uri == "" {
		log.Printf("confenge feed sync: enabled but no manifest URL configured; worker idle")
		return
	}
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
			t.Reset(w.interval)
		}
	}
}

func (w *FeedSyncWorker) tick(ctx context.Context) {
	res, xerr := w.svc.SyncFeedManifest(ctx, w.orgID, nil, "")
	if xerr != nil {
		log.Printf("confenge feed sync org=%s: %s", w.orgID, xerr.Message)
		return
	}
	if res != nil && res.Status != "noop" {
		log.Printf("confenge feed sync org=%s status=%s imported=%d", w.orgID, res.Status, res.ChunksImported)
	}
}
