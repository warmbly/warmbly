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
	"github.com/warmbly/warmbly/internal/repository"
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
	complete := false
	defer func() { w.endTick(ctx, stats, complete) }()
	if !w.retryUnmap(ctx) {
		return nil
	}
	w.tracker.startBackfill(time.Now(), w.gov.Policy().BackfillDays)
	w.GraphData.Client.OnRecoveryStart = w.graphRecoveryStart
	w.GraphData.Client.OnRecoveryCheckpoint = w.graphRecoveryCheckpoint
	w.GraphData.Client.OnRecoveryBaseline = w.graphRecoveryBaseline
	w.GraphData.Client.PendingRecoveryLink = w.graphPendingRecoveryLink
	w.GraphData.Client.Recovering = w.graphRecovering
	if w.SyncContext != nil && w.GraphData.Client.ImmutableIDMode() {
		done, err := w.graphUpgradeIDs(ctx)
		if errors.Is(err, repository.ErrSyncContextUnsupported) {
			w.GraphData.Client.SetImmutableIDMode(false)
			w.CaptureError(err)
			done, err = true, nil
		}
		if err != nil {
			return w.graphSyncError(err)
		}
		if !done {
			return nil
		}
		if w.GraphData.Client.ImmutableIDMode() {
			w.GraphData.Client.OnFolderReconcile = w.graphReconcileFolder
		}
	}

	caughtUp, err := w.GraphData.Client.SyncPass(ctx)
	if err != nil {
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
	complete = caughtUp
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
	known, err := w.graphMessageMap(ctx, providerID)
	if err != nil {
		w.controlPlaneError(err, stats)
		return false, msgraph.ErrStop
	}
	if known != nil {
		if !w.GraphData.Client.ImmutableIDMode() {
			if err := w.onGraphFlagsChange(ctx, providerID, seen); err != nil {
				return false, err
			}
			id, err := uuid.Parse(known.ID)
			if err != nil {
				return false, err
			}
			return true, w.emitFolder(id, (&msgraph.GraphMessage{}).ToEmailData(folder).Folder)
		}
		return true, w.graphReconcileMessage(ctx, known.ID, providerID, folder)
	}

	lane, cached := w.laneCache.get(providerID)
	if (msgraph.IsRecovery(ctx) && lane == LaneLive) || (!msgraph.IsRecovery(ctx) && lane == LaneBackfill) {
		cached = false
	}
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
		lane = w.laneFor(ctx, msg, false)
		if msgraph.IsRecovery(ctx) && lane != LanePriority {
			recovery, err := w.graphRecovery(folder)
			if err != nil {
				return false, err
			}
			if (!msg.InternalDate.IsZero() && msg.InternalDate.Before(recovery.Since)) || recovery.Count >= w.gov.Policy().BackfillMessages {
				return true, nil
			}
			lane = LaneBackfill
		}
	}
	if lane == LaneBackfill && w.tracker.state.BackfillStatus != models.SyncBackfillComplete && w.tracker.state.BackfillSynced >= w.gov.Policy().BackfillMessages {
		return true, nil
	}
	if lane == LaneBackfill && msgraph.IsRecovery(ctx) {
		recovery, err := w.graphRecovery(folder)
		if err != nil {
			return false, err
		}
		if recovery.Count >= w.gov.Policy().BackfillMessages {
			return true, nil
		}
	}
	if lane == LaneLive && w.observeLive(ctx, []string{providerID}, stats) {
		return false, msgraph.ErrStop
	}
	w.laneCache.put(providerID, lane)
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
	if lane == LaneBackfill {
		if w.tracker.state.BackfillStatus != models.SyncBackfillComplete {
			w.tracker.state.BackfillSynced++
			w.tracker.mark()
		}
		recovery, err := w.graphRecovery(folder)
		if err != nil {
			return false, err
		}
		recovery.Count++
		w.setGraphRecovery(folder, recovery)
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
		Seen:         models.SeenFromFlags(msg.Flags),
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
				stats.aborted = true
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
