package wmail

import (
	"context"
	"strings"

	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// imapWakeFolder reports a folder a wake checks: the real INBOX, which IDLE
// watches, and junk, so a spam move is seen promptly.
func imapWakeFolder(box *models.Mailbox) bool {
	if strings.EqualFold(box.Name, "INBOX") {
		return true
	}
	return imapCanonicalFolder(box) == models.FolderSpam
}

// imapWakeCheck is a wake's narrow check: the wake folders already followed,
// each walked from its own cursor exactly as the full pass would. No listing,
// no other folder, no reconciliation, flag scan, recovery or backfill; a
// folder the server re-generated is left for the full pass to re-baseline.
func (w *WMail) imapWakeCheck(ctx context.Context) *errx.MailError {
	if w.SmtpImapData == nil || w.SmtpImapData.ImapClient == nil {
		return nil
	}
	w.beginTick()
	stats := &tickStats{}
	defer func() { w.endTick(ctx, stats, false) }()
	if !w.retryUnmap(ctx) {
		return nil
	}
	client := w.SmtpImapData.ImapClient
	skip := w.skipFolders()
	for _, before := range w.SmtpImapData.Mailboxes {
		if stats.aborted || ctx.Err() != nil {
			return nil
		}
		if !imapWakeFolder(before) || imapVirtualFolder(before) || imap.SkipsFolder(*before, skip) {
			continue
		}
		view, err := client.SelectForSyncState(before.Name)
		if err != nil {
			return err
		}
		if view.UIDValidity != 0 && view.UIDValidity != before.UIDValidity {
			w.narrowDeferred = true
			continue
		}
		condStore := client.HasCondStore() && view.HighestModSeq > 0
		now := *before
		now.UIDNext, now.HighestModSeq, now.Messages = view.UIDNext, view.HighestModSeq, view.Count
		if !imapFolderChanged(before, &now, condStore) {
			continue
		}
		w.setWalking(before)
		done, walked, _, err := w.imapIncremental(ctx, &now, before, condStore, stats, &view)
		if err != nil {
			return err
		}
		// A folder not fully stored keeps its cursor, so the next check or pass re-offers the rest.
		if !done {
			continue
		}
		next := *before
		next.Messages = view.Count
		advanceToView(&next, walked)
		if err := w.mboxEvent(&next); err != nil {
			return w.controlPlaneError(err, stats)
		}
		before.HighestModSeq = next.HighestModSeq
		before.UIDNext = next.UIDNext
	}
	return nil
}
