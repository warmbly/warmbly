package wmail

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Sync is one IMAP pass: follow every folder's CONDSTORE mod-sequence for
// live changes, then advance the backfill under its budget.
//
// A folder seen for the first time is baselined, not walked: its current
// HIGHESTMODSEQ is recorded so live sync starts from now, and its history is
// left to the backfill, which imports newest first under the policy's window
// and cap. That replaces the old first sight, which fetched every message in
// every folder oldest first and ran straight into the rate limiter.
func (w *WMail) Sync(ctx context.Context) *errx.MailError {
	if w.SmtpImapData == nil || w.SmtpImapData.ImapClient == nil {
		return nil
	}
	w.beginTick()
	stats := &tickStats{}

	client := w.SmtpImapData.ImapClient
	folders, err := client.Folders()
	if err != nil {
		return err
	}

	for i := range folders {
		box := &folders[i]
		befBox := w.SmtpImapData.FindPair(box)
		if befBox == nil {
			// First sight: baseline. Live sync starts from this mod-sequence;
			// the backfill owns everything before it.
			saved := *box
			if err := w.mboxEvent(&saved); err != nil {
				return nil
			}
			w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &saved)
			continue
		}

		fullyProcessed := true
		if befBox.HighestModSeq != box.HighestModSeq && !stats.aborted {
			w.SmtpImapData.mailbox = box.UIDValidity
			done, err := w.imapIncremental(ctx, box, befBox.HighestModSeq, stats)
			if err != nil {
				return err
			}
			fullyProcessed = done
		} else if befBox.HighestModSeq != box.HighestModSeq {
			// The pass was aborted before this folder; hold its cursor too.
			fullyProcessed = false
		}

		if befBox.HighestModSeq != box.HighestModSeq || befBox.Name != box.Name || !slices.Equal(befBox.Attrs, box.Attrs) {
			// The stored mod-sequence only moves once every change up to it
			// was stored; a deferred message keeps the folder re-asked.
			next := *box
			if !fullyProcessed {
				next.HighestModSeq = befBox.HighestModSeq
			}
			if err := w.mboxEvent(&next); err != nil {
				return nil
			}
			for _, ibox := range w.SmtpImapData.Mailboxes {
				if ibox.UIDValidity == box.UIDValidity {
					ibox.HighestModSeq = next.HighestModSeq
					ibox.Name = next.Name
					ibox.Attrs = next.Attrs
				}
			}
		}
	}

	// Collect deletions first to avoid modifying the slice during iteration
	var deleted []uint32
outer:
	for _, box := range w.SmtpImapData.Mailboxes {
		for _, f := range folders {
			if box.UIDValidity == f.UIDValidity {
				continue outer
			}
		}

		if err := w.onEvent(models.JobEventTypeMailboxDelete, &models.JobEventMailboxDelete{
			UserID:      w.UserID,
			EmailID:     w.ID,
			UIDValidity: box.UIDValidity,
		}); err != nil {
			return nil
		}
		deleted = append(deleted, box.UIDValidity)
	}

	if len(deleted) > 0 {
		filtered := w.SmtpImapData.Mailboxes[:0]
		for _, b := range w.SmtpImapData.Mailboxes {
			if !slices.Contains(deleted, b.UIDValidity) {
				filtered = append(filtered, b)
			}
		}
		w.SmtpImapData.Mailboxes = filtered
	}

	if !stats.aborted {
		if err := w.imapBackfill(ctx, folders, stats); err != nil {
			return err
		}
	}

	w.endTick(stats)
	return nil
}

// imapIncremental stores what changed in one folder since modSeq. Known
// messages relay their flags unbudgeted; new ones are admitted newest first.
// It reports whether every change was stored, which is what lets the folder's
// mod-sequence advance.
func (w *WMail) imapIncremental(ctx context.Context, box *models.Mailbox, modSeq uint64, stats *tickStats) (bool, *errx.MailError) {
	client := w.SmtpImapData.ImapClient
	count, err := client.SelectForSync(box.Name)
	if err != nil {
		return false, err
	}
	if count == 0 {
		return true, nil
	}
	uids, err := client.SearchChangedSince(modSeq)
	if err != nil {
		return false, err
	}
	if len(uids) == 0 {
		return true, nil
	}
	// Newest first: when budget is short, the freshest mail lands first.
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })

	complete := true
	for lo := 0; lo < len(uids); lo += config.ImapFetchBatchSize {
		hi := min(lo+config.ImapFetchBatchSize, len(uids))
		fetched, err := client.FetchEnvelopes(ctx, uids[lo:hi])
		if err != nil {
			return false, err
		}
		done, err := w.imapApply(ctx, fetched, false, stats)
		if err != nil {
			return false, err
		}
		if !done {
			complete = false
		}
		if stats.aborted || ctx.Err() != nil {
			return false, nil
		}
	}
	return complete, nil
}

// imapApply routes one fetched batch: known messages get an UPDATE_EMAIL,
// unknown ones are stored if their lane admits them. backfill selects the
// backfill lane and skips flood accounting. Returns whether every unknown
// message in the batch was stored.
func (w *WMail) imapApply(ctx context.Context, fetched []*imap.Fetched, backfill bool, stats *tickStats) (bool, *errx.MailError) {
	sort.Slice(fetched, func(i, j int) bool { return fetched[i].Email.UID > fetched[j].Email.UID })

	var fresh []*imap.Fetched
	for _, f := range fetched {
		internal, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, f.Email.MessageID)
		if err != nil {
			return false, w.controlPlaneError(err, stats)
		}
		if internal == nil {
			fresh = append(fresh, f)
			continue
		}
		if backfill {
			// The backfill only cares about what it has not stored.
			continue
		}
		internalID, perr := uuid.Parse(internal.ID)
		if perr != nil {
			continue
		}
		if err := w.onEvent(models.JobEventTypeEmailUpdate, &models.JobEventEmailUpdate{
			UserID:  w.UserID,
			EmailID: w.ID,
			ID:      internalID,
			UID:     f.Email.UID,
			ModSeq:  f.Email.ModSeq,
			Mailbox: w.SmtpImapData.mailbox,
			Flags:   f.Email.Flags,
		}); err != nil {
			return false, w.controlPlaneError(err, stats)
		}
	}

	if !backfill && len(fresh) > 0 {
		ids := make([]string, 0, len(fresh))
		for _, f := range fresh {
			ids = append(ids, f.Email.MessageID)
		}
		if w.observeLive(ctx, ids, stats) {
			return false, nil
		}
	}

	policy := w.gov.Policy()
	all := true
	for _, f := range fresh {
		if stats.aborted {
			return false, nil
		}
		if backfill && w.tracker.state.BackfillSynced >= policy.BackfillMessages {
			return false, nil
		}
		if !w.admit(ctx, w.laneOf(ctx, f.Email.MessageID, f.Email, backfill), stats) {
			all = false
			if backfill {
				return false, nil
			}
			continue
		}
		w.SmtpImapData.ImapClient.FetchBody(f)
		if err := w.imapStore(ctx, f.Email); err != nil {
			return false, w.controlPlaneError(err, stats)
		}
		w.laneCache.forget(f.Email.MessageID)
		if backfill {
			w.tracker.state.BackfillSynced++
			w.tracker.mark()
			w.tracker.setFolder(strconv.FormatUint(uint64(w.SmtpImapData.mailbox), 10), models.SyncFolderCursor{UID: f.Email.UID})
		}
	}
	return all, nil
}

// imapStore threads a new message and hands it to storeNew.
func (w *WMail) imapStore(ctx context.Context, msg *models.EmailMessageData) error {
	msg.ID = uuid.New()
	now := time.Now()

	var threadID string
	var parentID string
	if len(msg.InReplyTo) > 0 {
		parentID = msg.InReplyTo[len(msg.InReplyTo)-1]
	} else if len(msg.ReplyTo) > 0 {
		parentID = msg.ReplyTo[len(msg.ReplyTo)-1]
	}

	if parentID != "" {
		internalParent, _ := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, parentID)
		if internalParent != nil {
			// Join the parent's thread; using the parent's internal id here
			// forked a new thread at every reply depth.
			threadID = internalParent.ThreadID
			if threadID == "" {
				threadID = parentID
			}
		} else {
			// Parent unknown (e.g. reply to a pre-connect message): root a
			// thread on the parent's RFC id so siblings still group.
			threadID = parentID
		}
	} else {
		threadID = msg.MessageID
	}

	data := &models.EmailMessageStoreData{
		ID:           msg.ID,
		EmailID:      w.ID,
		Mailbox:      w.SmtpImapData.mailbox,
		ThreadID:     threadID,
		MessageID:    msg.MessageID,
		GmailID:      msg.GmailID,
		ParentID:     parentID,
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
		Snippet:      GenerateSnippet(msg.BodyPlain, msg.BodyHTML),
		BodyText:     SearchText(msg.BodyPlain, msg.BodyHTML),
		Seen:         false,
		UpdatedAt:    now,
		CreatedAt:    now,
	}
	return w.storeNew(ctx, msg, data, msg.MessageID)
}

// imapBackfill advances the initial import: for every eligible folder, walk
// the UIDs inside the window newest first, from the saved floor downward,
// until the pacing budget or the cap says stop. Progress is relayed after
// every message, so a replaced worker resumes rather than restarts.
func (w *WMail) imapBackfill(ctx context.Context, folders []models.Mailbox, stats *tickStats) *errx.MailError {
	st := &w.tracker.state
	if st.BackfillStatus == models.SyncBackfillComplete {
		return nil
	}
	policy := w.gov.Policy()
	w.tracker.startBackfill(time.Now(), policy.BackfillDays)
	since := *st.BackfillSince
	client := w.SmtpImapData.ImapClient

	allDone := true
	for i := range folders {
		box := &folders[i]
		if !imapBackfillEligible(box) {
			continue
		}
		if stats.aborted || stats.laneDenied(LaneBackfill) {
			return nil
		}
		key := strconv.FormatUint(uint64(box.UIDValidity), 10)
		cur := w.tracker.folder(key)
		if cur.Done {
			continue
		}
		if st.BackfillSynced >= policy.BackfillMessages {
			w.tracker.completeBackfill(time.Now())
			return nil
		}
		w.SmtpImapData.mailbox = box.UIDValidity

		count, err := client.SelectForSync(box.Name)
		if err != nil {
			return err
		}
		if count == 0 {
			w.tracker.setFolder(key, models.SyncFolderCursor{Done: true})
			continue
		}
		uids, err := client.SearchSince(since)
		if err != nil {
			return err
		}
		remaining := uids[:0:0]
		for _, uid := range uids {
			if cur.UID == 0 || uid < goimap.UID(cur.UID) {
				remaining = append(remaining, uid)
			}
		}
		if len(remaining) == 0 {
			w.tracker.setFolder(key, models.SyncFolderCursor{UID: cur.UID, Done: true})
			continue
		}
		allDone = false
		sort.Slice(remaining, func(i, j int) bool { return remaining[i] > remaining[j] })

		for lo := 0; lo < len(remaining); lo += config.ImapFetchBatchSize {
			hi := min(lo+config.ImapFetchBatchSize, len(remaining))
			fetched, err := client.FetchEnvelopes(ctx, remaining[lo:hi])
			if err != nil {
				return err
			}
			done, err := w.imapApply(ctx, fetched, true, stats)
			if err != nil {
				return err
			}
			if st.BackfillSynced >= policy.BackfillMessages {
				w.tracker.completeBackfill(time.Now())
				return nil
			}
			if !done || ctx.Err() != nil {
				// Pacing stopped us mid-folder; the floor is already at the last
				// stored UID and the next tick continues below it.
				return nil
			}
			// A batch that was entirely known still moves the floor.
			w.tracker.setFolder(key, models.SyncFolderCursor{UID: uint32(remaining[hi-1])})
		}
		w.tracker.setFolder(key, models.SyncFolderCursor{UID: uint32(remaining[len(remaining)-1]), Done: true})
	}

	if allDone {
		w.tracker.completeBackfill(time.Now())
	}
	return nil
}

// imapBackfillEligible excludes folders whose history is not worth importing:
// trash, drafts, spam and Gmail's virtual "All Mail" (a duplicate of every
// other folder). Live sync still follows them for placement signals; only the
// import skips them. Special-use attributes are authoritative, with a name
// fallback for servers that do not advertise them.
func imapBackfillEligible(box *models.Mailbox) bool {
	for _, a := range box.Attrs {
		switch strings.ToLower(a) {
		case "\\noselect", "\\nonexistent", "\\trash", "\\junk", "\\drafts", "\\all":
			return false
		}
	}
	name := strings.ToLower(box.Name)
	if i := strings.LastIndexAny(name, "/."); i >= 0 {
		name = name[i+1:]
	}
	switch name {
	case "trash", "junk", "spam", "drafts", "draft", "deleted items", "deleted messages", "junk e-mail", "junk email", "bulk mail":
		return false
	}
	return true
}

// controlPlaneError handles a failed map lookup, body store or event publish
// the way the old loop did: log it, hold every cursor by ending the pass, and
// retry next tick. It is not a mailbox error, so nothing is relayed to the
// consumer and no error record is written for a control-plane hiccup.
func (w *WMail) controlPlaneError(err error, stats *tickStats) *errx.MailError {
	log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("sync: control-plane call failed; pass ended, cursors held")
	stats.aborted = true
	return nil
}

func (w *WMail) mboxEvent(box *models.Mailbox) error {
	return w.onEvent(models.JobEventTypeMailboxUpdate, &models.JobEventMailboxUpdate{
		UserID:  w.UserID,
		EmailID: w.ID,
		Data:    box,
	})
}

func (w *SmtpImapData) FindPair(m *models.Mailbox) *models.Mailbox {
	for _, f := range w.Mailboxes {
		if f.UIDValidity == m.UIDValidity {
			return f
		}
	}
	return nil
}
