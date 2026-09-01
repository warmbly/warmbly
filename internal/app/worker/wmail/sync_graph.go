package wmail

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// graphBackfillPage is $top for one backfill listing. Messages come back
// fully hydrated, so the page is kept modest.
const graphBackfillPage = 50

// SyncGraph walks the Microsoft Graph delta stream for the mailbox, then
// advances the backfill under its budget. It is the Graph analogue of
// SyncGoogle: a critical MailError (auth/disabled) is returned so the sync
// loop stops and the account is flagged for re-auth; anything else is
// captured and swallowed so a transient blip doesn't tear down the account.
func (w *WMail) SyncGraph(ctx context.Context) *errx.MailError {
	w.beginTick()
	stats := &tickStats{}
	w.graphTick = stats

	if err := w.GraphData.Client.Sync(ctx); err != nil {
		var mailErr *errx.MailError
		if errors.As(err, &mailErr) {
			return mailErr
		}
		w.CaptureError(err)
		return nil
	}
	if !stats.aborted {
		if merr := w.graphBackfill(ctx, stats); merr != nil {
			return merr
		}
	}
	w.endTick(stats)
	return nil
}

// onGraphMessageSeen is the delta feed's offer of one live item. Known
// messages only relay their read state; unknown ones are classified,
// admitted and hydrated. false leaves the message on the server and pins the
// folder cursor before its page.
func (w *WMail) onGraphMessageSeen(ctx context.Context, folder, providerID string, seen bool) (bool, error) {
	stats := w.graphTick
	if stats == nil {
		stats = &tickStats{}
	}
	if stats.aborted {
		return false, msgraph.ErrStop
	}
	known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil {
		w.controlPlaneError(err, stats)
		return false, msgraph.ErrStop
	}
	if known != nil {
		return true, w.onGraphFlagsChange(ctx, providerID, seen)
	}
	if w.observeLive(ctx, []string{providerID}, stats) {
		return false, msgraph.ErrStop
	}

	lane, cached := w.laneCache.get(providerID)
	var msg *models.EmailMessageData
	if !cached {
		full, err := w.GraphData.Client.FetchMessage(ctx, folder, providerID)
		if err != nil {
			return false, err
		}
		if full == nil {
			return true, nil // gone between the delta item and now
		}
		msg = full.ToEmailData(folder)
		lane = w.laneOf(ctx, providerID, msg, false)
	}
	if !w.admit(ctx, lane, stats) {
		return false, nil
	}
	if msg == nil {
		full, err := w.GraphData.Client.FetchMessage(ctx, folder, providerID)
		if err != nil {
			return false, err
		}
		if full == nil {
			return true, nil
		}
		msg = full.ToEmailData(folder)
	}
	w.laneCache.forget(providerID)
	if err := w.graphStore(ctx, msg); err != nil {
		w.controlPlaneError(err, stats)
		return false, msgraph.ErrStop
	}
	return true, nil
}

// graphStore mirrors googleStore. The opaque Graph message id rides in
// GmailID (the provider-message-id field) and keys the map, since delta only
// ever reports that id on remove and read-state events.
func (w *WMail) graphStore(ctx context.Context, msg *models.EmailMessageData) error {
	msg.ID = uuid.New()
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
	return w.storeNew(ctx, msg, data, msg.GmailID)
}

// graphBackfill imports recent history from the backfill folders newest
// first, one listing page at a time, resuming from each folder's saved
// nextLink.
func (w *WMail) graphBackfill(ctx context.Context, stats *tickStats) *errx.MailError {
	st := &w.tracker.state
	if st.BackfillStatus == models.SyncBackfillComplete {
		return nil
	}
	policy := w.gov.Policy()
	w.tracker.startBackfill(time.Now(), policy.BackfillDays)
	since := *st.BackfillSince

	allDone := true
	for _, folder := range msgraph.BackfillFolders {
		if stats.aborted || stats.laneDenied(LaneBackfill) {
			return nil
		}
		cur := w.tracker.folder(folder)
		if cur.Done {
			continue
		}
		allDone = false
		for !stats.aborted && !stats.laneDenied(LaneBackfill) {
			if st.BackfillSynced >= policy.BackfillMessages {
				w.tracker.completeBackfill(time.Now())
				return nil
			}
			msgs, next, err := w.GraphData.Client.ListMessagesSince(ctx, folder, since, cur.Next, graphBackfillPage)
			if err != nil {
				var mailErr *errx.MailError
				if errors.As(err, &mailErr) {
					// Only Graph saying the folder is absent (archive on
					// some plans) skips it; a 503 ends the pass instead, or
					// one blip marks the folder complete forever.
					if mailErr.Code == errx.MailErrorCodeNotFound {
						log.Debug().
							Str("email_id", w.ID.String()).
							Str("folder", folder).
							Msg("backfill: folder absent on the tenant, skipped")
						w.tracker.setFolder(folder, models.SyncFolderCursor{Done: true})
						break
					}
					return mailErr
				}
				w.CaptureError(err)
				return nil
			}
			for _, full := range msgs {
				if st.BackfillSynced >= policy.BackfillMessages {
					w.tracker.completeBackfill(time.Now())
					return nil
				}
				known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, full.ID)
				if err != nil {
					return w.controlPlaneError(err, stats)
				}
				if known != nil {
					continue
				}
				if !w.admit(ctx, LaneBackfill, stats) {
					// Pacing: this page's link is kept; stored ids are skipped
					// as known when it is re-listed.
					return nil
				}
				if err := w.graphStore(ctx, full.ToEmailData(folder)); err != nil {
					return w.controlPlaneError(err, stats)
				}
				st.BackfillSynced++
				w.tracker.mark()
			}
			cur = models.SyncFolderCursor{Next: next, Done: next == ""}
			w.tracker.setFolder(folder, cur)
			if cur.Done {
				break
			}
		}
	}
	if allDone {
		w.tracker.completeBackfill(time.Now())
	}
	return nil
}
