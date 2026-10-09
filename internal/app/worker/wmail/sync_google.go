package wmail

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
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
	w.googleFolders = nil
	complete := false
	defer func() { w.endTick(ctx, stats, complete) }()
	if !w.retryUnmap(ctx) {
		return nil
	}
	if w.tracker.state.BackfillCursor.GoogleRecovery != nil {
		if err := w.googleRecoverHistory(ctx, stats); err != nil {
			return w.googleReconcileError(err)
		}
		return nil
	}

	newHistoryID, caughtUp, err := w.GoogleData.Client.FetchHistoryPass(ctx, w.GoogleData.LastHistoryID)
	if errors.Is(err, goog.ErrHistoryExpired) {
		baseline, berr := w.GoogleData.Client.HistoryBaseline(ctx)
		if berr != nil {
			return w.googleReconcileError(berr)
		}
		w.tracker.state.BackfillCursor.GoogleRecovery = &models.GoogleHistoryRecovery{
			HistoryID: baseline,
			Since:     time.Now().Add(-time.Duration(w.gov.Policy().BackfillDays) * 24 * time.Hour),
		}
		w.tracker.mark()
		if err := w.googleRecoverHistory(ctx, stats); err != nil {
			return w.googleReconcileError(err)
		}
		return nil
	}
	if newHistoryID != 0 && newHistoryID != w.GoogleData.LastHistoryID {
		// Keep the old cursor until publication succeeds, so a failed relay is retried.
		if perr := w.NewHistoryID(newHistoryID); perr != nil {
			w.CaptureError(perr)
			return nil
		}
		w.GoogleData.LastHistoryID = newHistoryID
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
	if !stats.aborted {
		// The pass's own work is done; only a refusal the owner has to act
		// on is worth failing it for.
		if merr := w.googleReconcileFolders(ctx, time.Now(), stats); merr != nil {
			return merr
		}
	}
	complete = caughtUp
	return nil
}

func (w *WMail) googleRecoverHistory(ctx context.Context, stats *tickStats) error {
	r := w.tracker.state.BackfillCursor.GoogleRecovery
	q := fmt.Sprintf("after:%d -in:chats", r.Since.Unix())
	for page := 0; !r.MessagesDone && page < 10; page++ {
		ids, next, err := w.GoogleData.Client.ListRecoveryMessages(ctx, q, r.PageToken, googleBackfillPage)
		if errors.Is(err, goog.ErrRecoveryPageExpired) {
			r.PageToken = ""
			w.tracker.mark()
			return nil
		}
		if err != nil {
			return err
		}
		for _, id := range ids {
			added, err := w.googleRecoverMessage(ctx, id, stats)
			if err != nil {
				return err
			}
			if !added {
				return nil
			}
		}
		r.PageToken, r.MessagesDone = next, next == ""
		w.tracker.mark()
	}
	if !r.MessagesDone {
		return nil
	}
	if w.SyncContext == nil {
		return errors.New("gmail history recovery requires sync context repository")
	}
	var after *uuid.UUID
	if r.StoredAfter != "" {
		id, err := uuid.Parse(r.StoredAfter)
		if err != nil {
			return err
		}
		after = &id
	}
	stored, err := w.SyncContext.ListProviderMessages(ctx, w.UserID, w.ID, after, googleBackfillPage)
	if err != nil {
		return err
	}
	for _, m := range stored {
		labels, found, err := w.GoogleData.Client.MessageLabels(ctx, m.ProviderID)
		if err != nil {
			return err
		}
		if !found {
			err = w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{UserID: w.UserID, EmailID: w.ID, ID: m.ID})
		} else {
			err = w.emitFolder(m.ID, goog.Folder(labels))
			if err == nil {
				err = w.googleRecoverFlags(m.ID, labels, m.Flags)
			}
		}
		if err != nil {
			return err
		}
		r.StoredAfter = m.ID.String()
		w.tracker.mark()
	}
	if len(stored) == googleBackfillPage {
		return nil
	}
	// Publish the baseline before clearing recovery, so a reload cannot skip unfinished work.
	if err := w.NewHistoryID(r.HistoryID); err != nil {
		return err
	}
	w.GoogleData.LastHistoryID = r.HistoryID
	w.tracker.state.BackfillCursor.GoogleRecovery = nil
	w.tracker.mark()
	return nil
}

func (w *WMail) googleRecoverMessage(ctx context.Context, id string, stats *tickStats) (bool, error) {
	known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, id)
	if err != nil {
		return false, err
	}
	if known != nil {
		return true, nil
	}
	// Recovered history is paced as backfill, not counted as a fresh inbound flood.
	if !w.admit(ctx, LaneBackfill, stats) {
		return false, nil
	}
	msg, err := w.GoogleData.Client.GetMessage(ctx, id)
	if err != nil {
		return false, err
	}
	if msg == nil {
		return true, nil
	}
	if err := w.googleStore(ctx, msg); err != nil {
		return false, err
	}
	return true, nil
}

func (w *WMail) googleRecoverFlags(id uuid.UUID, labels, stored []string) error {
	add, remove := translateGmailLabels(labels, true)
	for _, label := range []string{"UNREAD", "STARRED", "IMPORTANT", "DRAFT"} {
		if !slices.Contains(labels, label) {
			a, r := translateGmailLabels([]string{label}, false)
			add, remove = append(add, a...), append(remove, r...)
		}
	}
	for _, flag := range stored {
		if (strings.HasPrefix(flag, "Label_") || strings.HasPrefix(flag, "CATEGORY_") || slices.Contains([]string{goog.Inbox, goog.Sent, goog.Spam, goog.Trash}, flag)) && !slices.Contains(add, flag) {
			remove = append(remove, flag)
		}
	}
	for _, flags := range []struct {
		kind  models.JobEventType
		flags []string
	}{
		{models.JobEventTypeFlagsAdd, add}, {models.JobEventTypeFlagsRemove, remove},
	} {
		if len(flags.flags) == 0 {
			continue
		}
		if err := w.onEvent(flags.kind, &models.JobEventFlags{UserID: w.UserID, EmailID: w.ID, ID: id, Flags: flags.flags}); err != nil {
			return err
		}
	}
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
		Seen:         models.SeenFromFlags(msg.Flags),
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
			stats.aborted = true
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
				stats.aborted = true
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

// reconcileElsewhere is every folder a stored Gmail message can be moved back
// to the inbox from.
var reconcileElsewhere = []string{models.FolderArchive, models.FolderSpam, models.FolderTrash}

// googleReconcileFolders repairs stored mail the history feed did not move:
// changes from before the sync followed labels, and any a checkpoint Gmail
// expired skipped over. Inbox rows missing from Gmail's inbox are looked up;
// every other row only moves when Gmail lists it in the inbox. A pass that
// fails is tried again after GmailFolderReconcileRetry.
func (w *WMail) googleReconcileFolders(ctx context.Context, now time.Time, stats *tickStats) *errx.MailError {
	if w.SyncContext == nil || now.Sub(w.googleReconciledAt) < config.GmailFolderReconcileInterval {
		return nil
	}
	w.googleReconciledAt = now.Add(config.GmailFolderReconcileRetry - config.GmailFolderReconcileInterval)

	inboxRows, err := w.SyncContext.ListProviderFolderMessages(ctx, w.UserID, w.ID, []string{models.FolderInbox}, config.GmailFolderReconcileMessages)
	if err != nil {
		return w.controlPlaneError(err, stats)
	}
	otherRows, err := w.SyncContext.ListProviderFolderMessages(ctx, w.UserID, w.ID, reconcileElsewhere, config.GmailFolderReconcileMessages)
	if err != nil {
		return w.controlPlaneError(err, stats)
	}
	if len(inboxRows) == 0 && len(otherRows) == 0 {
		w.googleReconciledAt = now
		return nil
	}

	// Each set comes newest first, and the listing has to reach the oldest row
	// of either; a day of slack covers Gmail reading after: against its own
	// calendar.
	var oldest time.Time
	for _, rows := range [][]repository.ProviderFolderMessage{inboxRows, otherRows} {
		if len(rows) > 0 && (oldest.IsZero() || rows[len(rows)-1].InternalDate.Before(oldest)) {
			oldest = rows[len(rows)-1].InternalDate
		}
	}
	q := ""
	if after := oldest.Add(-24 * time.Hour); after.Unix() > 0 {
		q = fmt.Sprintf("after:%d", after.Unix())
	}
	inInbox := make(map[string]struct{})
	token := ""
	for page := 0; page < config.GmailFolderReconcilePages; page++ {
		ids, next, err := w.GoogleData.Client.ListLabelMessages(ctx, goog.Inbox, q, token, 500)
		if err != nil {
			stats.aborted = true
			return w.googleReconcileError(err)
		}
		for _, id := range ids {
			inInbox[id] = struct{}{}
		}
		if next == "" {
			break
		}
		token = next
	}

	for _, m := range otherRows {
		if _, ok := inInbox[m.ProviderID]; !ok {
			continue
		}
		if err := w.emitFolder(m.ID, models.FolderInbox); err != nil {
			return w.controlPlaneError(err, stats)
		}
	}

	if w.googleInboxChecked == nil {
		w.googleInboxChecked = make(map[string]time.Time)
	}
	for id, at := range w.googleInboxChecked {
		if now.Sub(at) >= config.GmailFolderReconcileRecheck {
			delete(w.googleInboxChecked, id)
		}
	}
	lookups := 0
	for _, m := range inboxRows {
		if _, ok := inInbox[m.ProviderID]; ok {
			continue
		}
		if _, checked := w.googleInboxChecked[m.ProviderID]; checked {
			continue
		}
		if lookups >= config.GmailFolderReconcileLookups {
			break
		}
		lookups++
		labels, found, err := w.GoogleData.Client.MessageLabels(ctx, m.ProviderID)
		if err != nil {
			if gmailRetryable(err) {
				stats.aborted = true
				return w.googleReconcileError(err)
			}
			// One message Gmail refuses must not hold up every row after it.
			log.Debug().Err(err).Str("email_id", w.ID.String()).Str("gmail_id", m.ProviderID).Msg("gmail folder reconciliation: lookup refused")
			w.googleInboxChecked[m.ProviderID] = now
			continue
		}
		if !found {
			// Gone from Gmail, which is what the live feed's delete reports.
			if err := w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
				UserID:  w.UserID,
				EmailID: w.ID,
				ID:      m.ID,
			}); err != nil {
				return w.controlPlaneError(err, stats)
			}
			w.googleInboxChecked[m.ProviderID] = now
			continue
		}
		folder := goog.Folder(labels)
		if folder == models.FolderInbox {
			w.googleInboxChecked[m.ProviderID] = now
			continue
		}
		if err := w.emitFolder(m.ID, folder); err != nil {
			return w.controlPlaneError(err, stats)
		}
	}
	w.googleReconciledAt = now
	return nil
}

func (w *WMail) emitFolder(id uuid.UUID, folder string) error {
	return w.onEvent(models.JobEventTypeFolderUpdate, &models.JobEventFolderUpdate{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      id,
		Folder:  folder,
	})
}

// googleReconcileError returns a Gmail failure to the caller, which fails the
// tick only when it is critical; the rest are captured unless transient.
func (w *WMail) googleReconcileError(err error) *errx.MailError {
	var errMail *errx.MailError
	if !errors.As(err, &errMail) {
		w.CaptureError(err)
		return nil
	}
	if errMail.Type != errx.MailErrorCritical && !gmailRetryable(err) {
		w.CaptureError(err)
	}
	return errMail
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
