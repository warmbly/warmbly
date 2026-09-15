package wmail

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// googleBackfillPage is how many ids one messages.list call returns. Small
// enough that a page interrupted by pacing costs little to re-list.
const googleBackfillPage = 100

// SyncGoogle walks Gmail's history from this mailbox's checkpoint and advances
// it, then moves the backfill along under its budget. Runs serially from
// StartSyncWorker, so LastHistoryID needs no locking.
func (w *WMail) SyncGoogle(ctx context.Context) *errx.MailError {
	w.beginTick()
	stats := &tickStats{}
	w.googleTick = stats

	newHistoryID, err := w.GoogleData.Client.FetchHistory(ctx, w.GoogleData.LastHistoryID)
	if newHistoryID != 0 && newHistoryID != w.GoogleData.LastHistoryID {
		// Advance the in-memory cursor before persisting. The next tick reads
		// this field, so leaving it stale re-walks the window just processed,
		// and leaving it at zero re-bootstraps past everything that arrived.
		w.GoogleData.LastHistoryID = newHistoryID
		if perr := w.NewHistoryID(newHistoryID); perr != nil {
			w.CaptureError(perr)
		}
	}
	if err != nil {
		var errMail *errx.MailError
		if errors.As(err, &errMail) {
			return errMail
		}
		w.CaptureError(err)
		return nil
	}

	if !stats.aborted {
		if merr := w.googleBackfill(ctx, stats); merr != nil {
			return merr
		}
	}
	w.endTick(stats)
	return nil
}

// onGoogleMessageAdded is the history feed's offer of one added message. It
// dedupes by Gmail id (the only identifier remove and label events carry),
// classifies, and hydrates only what is admitted. false leaves the message
// on the server and pins the checkpoint before it.
func (w *WMail) onGoogleMessageAdded(ctx context.Context, id, threadID string) (bool, error) {
	stats := w.googleTick
	if stats == nil {
		stats = &tickStats{}
	}
	if stats.aborted {
		return false, goog.ErrStop
	}
	known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, id)
	if err != nil {
		w.controlPlaneError(err, stats)
		return false, goog.ErrStop
	}
	if known != nil {
		return true, nil
	}
	if w.observeLive(ctx, []string{id}, stats) {
		return false, goog.ErrStop
	}

	lane, cached := w.laneCache.get(id)
	var msg *models.EmailMessageData
	if !cached {
		// Classification needs the headers, so a first sight is hydrated in
		// full; the lane is remembered so a deferred message is not fetched
		// again on every pass while it waits.
		msg, err = w.GoogleData.Client.GetMessage(ctx, id)
		if err != nil {
			return false, err
		}
		if msg == nil {
			return true, nil // gone between the history event and now
		}
		if msg.ThreadID == "" {
			msg.ThreadID = threadID
		}
		lane = w.laneOf(ctx, id, msg, false)
	}
	if !w.admit(ctx, lane, stats) {
		return false, nil
	}
	if msg == nil {
		msg, err = w.GoogleData.Client.GetMessage(ctx, id)
		if err != nil {
			return false, err
		}
		if msg == nil {
			return true, nil
		}
	}
	w.laneCache.forget(id)
	if err := w.googleStore(ctx, msg); err != nil {
		w.controlPlaneError(err, stats)
		return false, goog.ErrStop
	}
	return true, nil
}

func (w *WMail) googleStore(ctx context.Context, msg *models.EmailMessageData) error {
	msg.ID = newMessageID()
	now := time.Now()
	data := &models.EmailMessageStoreData{
		ID:           msg.ID,
		EmailID:      w.ID,
		Mailbox:      0,
		Folder:       msg.Folder,
		ThreadID:     msg.ThreadID,
		MessageID:    msg.MessageID,
		GmailID:      msg.GmailID,
		ParentID:     msg.ParentID,
		UID:          msg.UID,
		ModSeq:       msg.ModSeq,
		Flags:        msg.Flags,
		BCC:          msg.BCC,
		CC:           msg.CC,
		FromAddr:     msg.From,
		InReplyTo:    msg.InReplyTo,
		ReplyTo:      msg.ReplyTo,
		ToAddr:       msg.To,
		Subject:      msg.Subject,
		Size:         msg.Size,
		InternalDate: msg.InternalDate,
		SentDate:     msg.Date,
		Snippet:      msg.Snippet,
		BodyText:     SearchText(msg.BodyPlain, msg.BodyHTML),
		Seen:         false,
		UpdatedAt:    now,
		CreatedAt:    now,
	}
	// The map is keyed by the Gmail message id: it is the only identifier the
	// history feed reports on remove/label events, so add must key the same
	// way for those lookups to ever match.
	return w.storeNew(ctx, msg, data, msg.GmailID)
}

// googleBackfill imports the mailbox's recent history newest first, one
// messages.list page at a time, resuming from the saved page token. The query
// excludes what the IMAP path also skips: trash and spam (their history would
// eat the message budget that belongs to real conversations; live sync still
// files new mail into those scopes) plus chats. Drafts are imported, matching
// IMAP, so the Drafts scope is not empty of everything written before connect.
func (w *WMail) googleBackfill(ctx context.Context, stats *tickStats) *errx.MailError {
	st := &w.tracker.state
	if st.BackfillStatus == models.SyncBackfillComplete {
		return nil
	}
	policy := w.gov.Policy()
	w.tracker.startBackfill(time.Now(), policy.BackfillDays)
	q := fmt.Sprintf("after:%d -in:trash -in:spam -in:chats", st.BackfillSince.Unix())

	for !stats.aborted && !stats.laneDenied(LaneBackfill) {
		if st.BackfillSynced >= policy.BackfillMessages {
			w.tracker.completeBackfill(time.Now())
			return nil
		}
		ids, next, err := w.GoogleData.Client.ListMessages(ctx, q, st.BackfillCursor.PageToken, googleBackfillPage)
		if err != nil {
			var errMail *errx.MailError
			if errors.As(err, &errMail) {
				return errMail
			}
			w.CaptureError(err)
			return nil
		}
		for _, id := range ids {
			if st.BackfillSynced >= policy.BackfillMessages {
				w.tracker.completeBackfill(time.Now())
				return nil
			}
			known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, id)
			if err != nil {
				return w.controlPlaneError(err, stats)
			}
			if known != nil {
				continue
			}
			if !w.admit(ctx, LaneBackfill, stats) {
				// Pacing: the page token is not advanced, and the ids already
				// stored are skipped as known when the page is re-listed.
				return nil
			}
			msg, err := w.GoogleData.Client.GetMessage(ctx, id)
			if err != nil {
				var errMail *errx.MailError
				if errors.As(err, &errMail) {
					return errMail
				}
				w.CaptureError(err)
				return nil
			}
			if msg == nil {
				continue
			}
			if err := w.googleStore(ctx, msg); err != nil {
				return w.controlPlaneError(err, stats)
			}
			st.BackfillSynced++
			w.tracker.mark()
		}
		st.BackfillCursor.PageToken = next
		w.tracker.mark()
		if next == "" {
			w.tracker.completeBackfill(time.Now())
			return nil
		}
	}
	return nil
}

// NewHistoryID persists the mailbox's Gmail history checkpoint. UserID and
// EmailID address the row: email_history_ids is keyed (user_id, email_id) with
// a foreign key to users, so omitting them sends a zero UUID and the write is
// rejected outright rather than landing on the wrong row.
func (w *WMail) NewHistoryID(historyID uint64) error {
	return w.onEvent(models.JobEventTypeHistoryIDUpdate, &models.JobEventHistoryIDUpdate{
		UserID:    w.UserID,
		EmailID:   w.ID,
		HistoryID: historyID,
	})
}
