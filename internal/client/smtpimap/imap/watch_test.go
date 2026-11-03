package imap

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

func TestWatchInboxReportsNewMailWithinIdle(t *testing.T) {
	addr := startMemServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}})
	changed := make(chan struct{}, 8)
	watcher := clientTo(t, addr)
	watcher.OnChange = func() { changed <- struct{}{} }
	idling := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		if err := watcher.WatchInbox(ctx, func() { idling <- struct{}{} }); err != nil {
			result <- err
			return
		}
		result <- nil
	}()
	select {
	case <-idling:
	case err := <-result:
		t.Fatalf("watch ended before IDLE: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("never entered IDLE")
	}

	poster := clientTo(t, addr)
	if err := poster.Connect(); err != nil {
		t.Fatalf("connect poster: %v", err)
	}
	appendMessage(t, poster, "INBOX", "<arrived@test>")
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("new INBOX mail did not reach OnChange")
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("a cancelled watch returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop on cancel")
	}
}

func TestSyncSessionOptionsUnchangedWithoutWatcher(t *testing.T) {
	if (&Client{}).options(nil) != nil {
		t.Fatal("a sync session without TLS config must keep go-imap's default options")
	}
}
