package wmail

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
	"github.com/warmbly/warmbly/internal/repository"
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
	complete := false
	defer func() { w.endTick(ctx, stats, complete) }()
	if !w.retryUnmap(ctx) {
		return nil
	}

	client := w.SmtpImapData.ImapClient
	// A mailbox left selected by the previous pass freezes LIST-STATUS on this
	// connection, so release it before asking what changed.
	client.ReleaseMailbox()
	listing, err := client.ListFolders()
	if err != nil {
		return err
	}
	if listing.Present == nil {
		return errx.ErrMailServerUnreachable
	}
	folders := listing.Folders
	w.reportFolderOverflow()
	// ListFolders already drops Gmail's label views. Dropping them here too
	// costs nothing and keeps the pass correct against any listing: a view
	// that reached it would re-file known mail as archive under a second UID.
	folders = slices.DeleteFunc(folders, func(b models.Mailbox) bool { return imapVirtualFolder(&b) })

	// The folders the owner excluded leave the listing here, before renames
	// are followed and before the delete sweep: one already synced is retired
	// like a folder the server dropped, and one never seen is never
	// baselined. They are kept aside so mail that moves into one of them can
	// be recognised as filed rather than lost.
	var skipped []models.Mailbox
	skip := w.skipFolders()
	if len(skip) > 0 {
		folders = slices.DeleteFunc(folders, func(b models.Mailbox) bool {
			if !imap.SkipsFolder(b, skip) {
				return false
			}
			skipped = append(skipped, b)
			return true
		})
	}

	if len(skipped) > 0 {
		nodeevidence.Emit(nodeevidence.FolderSkipped, w.ID, 0, len(skipped))
	}
	// Before anything is matched by name, follow the folders whose name
	// changed. A rename read as a delete plus a first sighting would orphan
	// every message filed under the old name and re-import the folder's
	// history under the new one.
	selectionComplete := client.FolderOverflow() == 0 && client.FolderConflicts() == 0
	if selectionComplete {
		if err := w.imapFollowRenames(folders, listing.Present); err != nil {
			return nil
		}
	}

	// CONDSTORE is effective per folder; NOMODSEQ uses UIDNEXT and periodic flags.
	caughtUp := selectionComplete
	selectedNames := make(map[string]struct{}, len(folders))
	for _, box := range folders {
		selectedNames[box.Name] = struct{}{}
	}

	for i := range folders {
		box := &folders[i]
		condStore := client.HasCondStore() && box.HighestModSeq > 0
		// The listing is remembered before anything is decided from it, so
		// the departure check below reads the previous pass, never this one.
		prevListing, listedBefore := w.listed[box.Name]
		w.rememberListing(box)
		befBox := w.SmtpImapData.FindPair(box)
		if befBox == nil {
			// First sight: baseline. Live sync starts from this cursor; the
			// backfill owns everything before it.
			saved := *box
			if !w.imapRecoverFolder(box) {
				stats.aborted = true
				return nil
			}
			if err := w.mboxEvent(&saved); err != nil {
				return nil
			}
			w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &saved)
			continue
		}

		// A folder whose UIDVALIDITY moved is the server telling us every UID
		// we hold for it is void: the cursors address nothing and the flag
		// snapshot is about messages that may no longer be there. Re-baseline
		// it exactly like a first sighting, and drop its backfill floor so an
		// import still running walks it again (stored messages are matched by
		// Message-ID, so nothing is stored twice). Completed accounts schedule
		// a separate bounded folder recovery without resetting global history.
		if befBox.UIDValidity != box.UIDValidity {
			saved := *box
			if !w.imapRecoverFolder(box) {
				stats.aborted = true
				return nil
			}
			if err := w.mboxEvent(&saved); err != nil {
				return nil
			}
			*befBox = saved
			delete(w.flagScan, box.Name)
			continue
		}

		var selected *imap.Selected
		if condStore && !stats.aborted {
			view, err := client.SelectForSyncState(box.Name)
			if err != nil {
				return err
			}
			selected = &view
			condStore = view.HighestModSeq > 0
			if view.UIDValidity != 0 && view.UIDValidity != box.UIDValidity {
				caughtUp = false
				continue
			}
		}
		changed := imapFolderChanged(befBox, box, condStore)
		// A pass cut short by the search cap left rows unexamined, so the
		// next pass looks again whether or not the count moved.
		movedOut := len(skipped) > 0 && (w.skipPending[box.Name] || listedBefore && imapMovedOut(prevListing, box))
		fullyProcessed := true
		var touched map[string]struct{}
		var view imap.Selected
		if changed && !stats.aborted {
			w.setWalking(box)
			done, sel, ids, err := w.imapIncremental(ctx, box, befBox, condStore, stats, selected)
			if err != nil {
				return err
			}
			fullyProcessed = done
			view = sel
			condStore = condStore && view.HighestModSeq > 0
			touched = ids
		} else if changed {
			// The pass was aborted before this folder; hold its cursor too.
			fullyProcessed = false
		}
		if !fullyProcessed {
			caughtUp = false
		}

		if changed || !slices.Equal(befBox.Attrs, box.Attrs) {
			// The stored cursor only moves once every change up to it was
			// stored; a deferred message keeps the folder re-asked.
			next := *box
			if !fullyProcessed {
				next.HighestModSeq = befBox.HighestModSeq
				next.UIDNext = befBox.UIDNext
			} else if changed {
				advanceToView(&next, view)
			}
			if err := w.mboxEvent(&next); err != nil {
				return nil
			}
			// befBox is the stored copy itself, matched by name, so there is
			// nothing else in the list to keep in step with it.
			befBox.HighestModSeq = next.HighestModSeq
			befBox.UIDNext = next.UIDNext
			befBox.Attrs = next.Attrs
		}

		// Drafts are the one folder where the server dropping a UID means the
		// row is gone, so they are reconciled every pass, changed or not: a
		// full pass has to clean up rows older workers left behind.
		if imapCanonicalFolder(box) == models.FolderDrafts && !stats.aborted {
			if err := w.imapReconcileDrafts(ctx, box, touched, stats); err != nil {
				return err
			}
		} else if movedOut && !stats.aborted {
			if err := w.imapReconcileSkipped(ctx, box, skipped, touched, stats); err != nil {
				return err
			}
		}

		// Without CONDSTORE a message marked read elsewhere moves no cursor,
		// so read state is mirrored by a periodic scan instead. It runs after
		// the arrivals above so a message stored this pass is already known.
		if !condStore && !stats.aborted {
			w.setWalking(box)
			if _, err := w.SmtpImapData.ImapClient.SelectForSync(box.Name); err != nil {
				return err
			}
			if err := w.imapScanFlags(ctx, box, stats); err != nil {
				return err
			}
		}
	}

	// Only complete LIST presence or an explicit exclusion can retire state.
	var deleted []string
	var gone []*models.Mailbox
	for _, box := range w.SmtpImapData.Mailboxes {
		if _, present := listing.Present[box.Name]; present && !imapVirtualFolder(box) && !imap.SkipsFolder(*box, skip) {
			if _, selected := selectedNames[box.Name]; !selected {
				caughtUp = false
			}
			continue
		}
		gone = append(gone, box)
	}
	for _, box := range gone {
		if err := w.onEvent(models.JobEventTypeMailboxDelete, &models.JobEventMailboxDelete{
			UserID:      w.UserID,
			EmailID:     w.ID,
			Mailbox:     box.Name,
			UIDValidity: box.UIDValidity,
			// A folder that is still on the server but now excluded takes
			// the mail already stored from it along.
			Skipped: imap.SkipsFolder(*box, skip) || imapRetiredIntoSkipped(box, gone, skipped),
		}); err != nil {
			return nil
		}
		deleted = append(deleted, box.Name)
	}

	if len(deleted) > 0 {
		for _, name := range deleted {
			delete(w.flagScan, name)
			delete(w.listed, name)
			delete(w.skipPending, name)
			// The backfill floor goes with the folder. A name is reusable,
			// and a floor left behind would be inherited by whatever is
			// created under it next.
			w.tracker.clearFolder(name)
		}
		filtered := w.SmtpImapData.Mailboxes[:0]
		for _, b := range w.SmtpImapData.Mailboxes {
			if !slices.Contains(deleted, b.Name) {
				filtered = append(filtered, b)
			}
		}
		w.SmtpImapData.Mailboxes = filtered
	}

	if !stats.aborted {
		if err := w.imapRecoverFolders(ctx, folders, stats); err != nil {
			return err
		}
		if err := w.imapBackfill(ctx, folders, stats); err != nil {
			return err
		}
	}

	complete = caughtUp && !w.imapRecoveryPending(folders)
	return nil
}

// imapRetiredIntoSkipped reports whether a folder leaving the listing is one
// the owner excluded: listed under the same name in the skipped set, or
// renamed into the skipped subtree, which the rename matcher could not see
// because skipped folders leave the listing before it runs. The rename is
// claimed on the matcher's own terms: exactly one folder gone and exactly
// one skipped folder carrying its UIDVALIDITY, so a server that stamps a
// whole tree from one creation time cannot make an unrelated deletion look
// like a move.
func imapRetiredIntoSkipped(box *models.Mailbox, gone []*models.Mailbox, skipped []models.Mailbox) bool {
	if slices.ContainsFunc(skipped, func(s models.Mailbox) bool { return s.Name == box.Name }) {
		return true
	}
	// A folder without a UIDVALIDITY cannot be matched on it, as in
	// imapFollowRenames.
	if box.UIDValidity == 0 {
		return false
	}
	sameGone, sameSkipped := 0, 0
	for _, g := range gone {
		if g.UIDValidity == box.UIDValidity {
			sameGone++
		}
	}
	for i := range skipped {
		if skipped[i].UIDValidity == box.UIDValidity {
			sameSkipped++
		}
	}
	return sameGone == 1 && sameSkipped == 1
}

// skipFolders is the owner's exclusion list as the policy in force carries
// it; a republished ADD_EMAIL changes it between passes.
func (w *WMail) skipFolders() []string {
	if w.gov == nil {
		return nil
	}
	return w.gov.Policy().SkipFolders
}

// imapListed is what one listing said about a folder: the two numbers the
// departure check compares between passes.
type imapListed struct {
	Messages uint32
	UIDNext  uint32
}

func (w *WMail) rememberListing(box *models.Mailbox) {
	if w.listed == nil {
		w.listed = make(map[string]imapListed)
	}
	w.listed[box.Name] = imapListed{Messages: box.Messages, UIDNext: box.UIDNext}
}

// imapMovedOut reports whether messages left the folder between two
// listings: the count is below the previous count plus the arrivals the
// UIDNEXT advance accounts for. An expunge moves neither cursor on every
// server, so the count is the one signal that always carries it. Both
// numbers come from the listing, never from the SELECT view a walked
// folder's cursor advances to.
func imapMovedOut(before imapListed, now *models.Mailbox) bool {
	if now.UIDNext < before.UIDNext {
		return false
	}
	arrivals := now.UIDNext - before.UIDNext
	return now.Messages < before.Messages+arrivals
}

// imapReconcileSkipped retires the platform's rows for mail that left this
// folder for one the owner excluded from sync. A row goes only when its
// Message-ID is found in a skipped folder: mail can leave for somewhere the
// sync does not follow (Gmail's All Mail) and still be wanted. Bounded per
// pass by imapSkipSearchesPerPass searches; what is left is continued next
// pass. A row looked for and found nowhere is remembered for the session; a
// lookup that failed is not, so the row is looked for again.
func (w *WMail) imapReconcileSkipped(ctx context.Context, box *models.Mailbox, skipped []models.Mailbox, touched map[string]struct{}, stats *tickStats) *errx.MailError {
	if w.SyncContext == nil || len(skipped) == 0 {
		return nil
	}
	if w.skipPending == nil {
		w.skipPending = make(map[string]bool)
	}
	delete(w.skipPending, box.Name)
	stored, err := w.SyncContext.ListFolderMessages(ctx, w.UserID, w.ID, box.Name, box.UIDValidity)
	if err != nil {
		return w.controlPlaneError(err, stats)
	}
	if len(stored) == 0 {
		return nil
	}
	client := w.SmtpImapData.ImapClient
	_, gen, serr := client.SelectForSyncGen(box.Name)
	if serr != nil {
		return serr
	}
	// UIDs only mean anything inside one generation.
	if gen != box.UIDValidity {
		return nil
	}
	present, aerr := client.SearchAll()
	if aerr != nil {
		return aerr
	}
	live := make(map[uint32]struct{}, len(present))
	for _, uid := range present {
		live[uint32(uid)] = struct{}{}
	}
	if w.skipChecked == nil || len(w.skipChecked) > imapSkipCheckedMax {
		w.skipChecked = make(map[string]struct{})
	}

	// The rows worth a lookup: gone from the folder, not re-fetched this
	// pass under a new UID, not settled earlier this session, and with a
	// Message-ID that was ever on the wire.
	var candidates []repository.StoredFolderMessage
	for _, m := range stored {
		if _, ok := live[m.UID]; ok {
			continue
		}
		if _, refiled := touched[m.MessageID]; refiled {
			continue
		}
		if _, done := w.skipChecked[w.skipKey(box, m.UID)]; done {
			continue
		}
		if m.MessageID == "" || strings.HasPrefix(m.MessageID, "no-msgid/") {
			w.skipChecked[w.skipKey(box, m.UID)] = struct{}{}
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return nil
	}
	// One SEARCH per row per skipped folder is the cost; the cap is on that.
	batch := max(1, imapSkipSearchesPerPass/len(skipped))
	if len(candidates) > batch {
		w.skipPending[box.Name] = true
		candidates = candidates[:batch]
	}
	ids := make([]string, 0, len(candidates))
	for _, m := range candidates {
		ids = append(ids, m.MessageID)
	}

	foundIn := make(map[string]string, len(ids))
	complete := true
	for i := range skipped {
		if ctx.Err() != nil {
			w.skipPending[box.Name] = true
			return nil
		}
		found, ferr := client.FindUIDsByMessageIDs(ctx, skipped[i].Name, ids)
		if ferr != nil {
			nodeevidence.Emit(nodeevidence.SearchSkipped, w.ID, 0, 1)
			log.Debug().Err(ferr).Str("email_id", w.ID.String()).Str("folder", skipped[i].Name).Msg("sync: search in skipped folder failed")
			complete = false
			continue
		}
		for id := range found {
			if _, ok := foundIn[id]; !ok {
				foundIn[id] = skipped[i].Name
			}
		}
	}

	for _, m := range candidates {
		folder, ok := foundIn[m.MessageID]
		if !ok {
			// Settled only when every skipped folder answered.
			if complete {
				w.skipChecked[w.skipKey(box, m.UID)] = struct{}{}
			} else {
				w.skipPending[box.Name] = true
			}
			continue
		}
		if err := w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
			UserID:        w.UserID,
			EmailID:       w.ID,
			ID:            m.ID,
			SkippedFolder: folder,
		}); err != nil {
			return w.controlPlaneError(err, stats)
		}
		// The map entry goes with the row, or the message could never be
		// imported again after moving back into a synced folder.
		if err := w.EmailMessageMapRepository.Del(ctx, w.UserID, w.ID, m.MessageID, m.ID); err != nil {
			return w.controlPlaneError(err, stats)
		}
		w.skipChecked[w.skipKey(box, m.UID)] = struct{}{}
	}
	return nil
}

// skipKey identifies one stored row for the session memory: folder,
// generation and UID.
func (w *WMail) skipKey(box *models.Mailbox, uid uint32) string {
	return fmt.Sprintf("%s\x00%d\x00%d", box.Name, box.UIDValidity, uid)
}

// imapSkipCheckedMax bounds the per-session memory of rows already looked
// for in the skipped folders; past it the memory starts over.
const imapSkipCheckedMax = 250_000

// imapSkipSearchesPerPass caps the searches one folder's reconciliation
// spends in one pass, across every skipped folder.
const imapSkipSearchesPerPass = 50

// imapFolderChanged reports whether a folder has anything new since the
// cursor we hold for it. With CONDSTORE the mod-sequence answers for new mail
// AND flag changes; without it only arrivals are visible here, and flag
// changes are picked up by the periodic scan in imapIncremental.
func imapFolderChanged(before, now *models.Mailbox, condStore bool) bool {
	if condStore && before.HighestModSeq > 0 && now.HighestModSeq > 0 {
		return before.HighestModSeq != now.HighestModSeq
	}
	return before.UIDNext != now.UIDNext
}

// imapIncremental stores what changed in one folder since the held cursor.
// Known messages relay their flags unbudgeted; new ones are admitted newest
// first. It reports whether every change was stored, which is what lets the
// folder's cursor advance, the selected view the search ran against, which is
// where it advances to, and the Message-IDs it fetched so the drafts
// reconciliation can tell a re-appended draft from an expunged one.
func (w *WMail) imapIncremental(ctx context.Context, box, before *models.Mailbox, condStore bool, stats *tickStats, selected *imap.Selected) (bool, imap.Selected, map[string]struct{}, *errx.MailError) {
	client := w.SmtpImapData.ImapClient
	var view imap.Selected
	var err *errx.MailError
	if selected != nil {
		view = *selected
	} else {
		view, err = client.SelectForSyncState(box.Name)
		if err != nil {
			return false, view, nil, err
		}
	}
	// The listing and this view name different generations, so the search
	// would answer about UIDs the cursor does not; the next pass re-baselines.
	if view.UIDValidity != 0 && view.UIDValidity != box.UIDValidity {
		return false, view, nil, nil
	}
	if view.Count == 0 {
		return true, view, nil, nil
	}
	var uids []goimap.UID
	if condStore && before.HighestModSeq > 0 && view.HighestModSeq > 0 {
		uids, err = client.SearchChangedSince(before.HighestModSeq)
	} else {
		uids, err = client.SearchNewSince(before.UIDNext)
	}
	if err != nil {
		return false, view, nil, err
	}
	if len(uids) == 0 {
		return true, view, nil, nil
	}
	// Newest first: when budget is short, the freshest mail lands first.
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })

	// Only the two reconciliations need the fetched ids back (drafts, and
	// mail that left for a skipped folder), so nothing else pays for the set.
	var touched map[string]struct{}
	if imapCanonicalFolder(box) == models.FolderDrafts || len(w.skipFolders()) > 0 {
		touched = make(map[string]struct{}, len(uids))
	}

	for lo := 0; lo < len(uids); lo += config.ImapFetchBatchSize {
		hi := min(lo+config.ImapFetchBatchSize, len(uids))
		fetched, err := client.FetchEnvelopes(ctx, uids[lo:hi])
		if err != nil {
			return false, view, touched, err
		}
		done, err := w.imapApply(ctx, fetched, false, stats, nil)
		if err != nil {
			return false, view, touched, err
		}
		for _, f := range fetched {
			if touched != nil {
				touched[f.Email.MessageID] = struct{}{}
			}
		}
		// A denied lane means no later batch can be stored either, and every
		// extra batch still counts new mail toward flood detection: a mailbox
		// unfrozen on a long backlog would deactivate itself walking mail it
		// cannot keep. Stop here; the held mod-sequence re-offers the rest.
		if !done || stats.aborted || ctx.Err() != nil {
			return false, view, touched, nil
		}
	}
	return true, view, touched, nil
}

// advanceToView moves a fully walked folder's cursor to the view its search
// ran against. The listing's STATUS is taken before the SELECT, and a server
// whose selected view lags it (a session snapshot, an APPEND from the send
// path in between) would otherwise record a cursor past mail the search never
// returned, and that mail would never be synced. A view that reports no
// cursor keeps the listing's.
func advanceToView(next *models.Mailbox, view imap.Selected) {
	if view.UIDNext != 0 {
		next.UIDNext = view.UIDNext
	}
	if view.HighestModSeq != 0 {
		next.HighestModSeq = view.HighestModSeq
	}
}

// imapReconcileDrafts removes the platform's rows for drafts the server no
// longer reports.
//
// A draft is the one message where a UID going away means the row is gone:
// Gmail replaces the previous autosave under a new UID and a new Message-ID,
// and a draft is never filed anywhere else. Every other folder can lose a
// message to a move the sync does not follow (Gmail's All Mail is dropped as a
// virtual label view), so reconciling there would silently drop mail the user
// still expects to see. The folder's complete UID set is the presence side;
// touched is the Message-IDs the pass fetched, which keeps a draft re-appended
// under a new UID from reading as expunged.
func (w *WMail) imapReconcileDrafts(ctx context.Context, box *models.Mailbox, touched map[string]struct{}, stats *tickStats) *errx.MailError {
	if w.SyncContext == nil {
		return nil
	}
	stored, err := w.SyncContext.ListFolderMessages(ctx, w.UserID, w.ID, box.Name, box.UIDValidity)
	if err != nil {
		return w.controlPlaneError(err, stats)
	}
	if len(stored) == 0 {
		return nil
	}
	_, gen, serr := w.SmtpImapData.ImapClient.SelectForSyncGen(box.Name)
	if serr != nil {
		return serr
	}
	// A UID only means anything inside one generation. If the folder was
	// recreated between the listing that produced box and this SELECT, the
	// live UIDs and the stored rows describe different folders, so diffing
	// them would remove rows never actually compared. The next pass reads the
	// new generation from the listing and reconciles it properly.
	if gen != box.UIDValidity {
		return nil
	}
	present, aerr := w.SmtpImapData.ImapClient.SearchAll()
	if aerr != nil {
		return aerr
	}
	live := make(map[uint32]struct{}, len(present))
	for _, uid := range present {
		live[uint32(uid)] = struct{}{}
	}
	for _, m := range stored {
		if _, ok := live[m.UID]; ok {
			continue
		}
		if _, refiled := touched[m.MessageID]; refiled {
			continue
		}
		if err := w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
			UserID:  w.UserID,
			EmailID: w.ID,
			ID:      m.ID,
		}); err != nil {
			return w.controlPlaneError(err, stats)
		}
	}
	return nil
}

// imapApply routes one fetched batch: known messages get an UPDATE_EMAIL,
// unknown ones are stored if their lane admits them. backfill selects the
// backfill lane and skips flood accounting. Returns whether every unknown
// message in the batch was stored.
func (w *WMail) imapApply(ctx context.Context, fetched []*imap.Fetched, backfill bool, stats *tickStats, recovery *imapFolderRecovery) (bool, *errx.MailError) {
	sort.Slice(fetched, func(i, j int) bool { return fetched[i].Email.UID > fetched[j].Email.UID })

	var fresh []*imap.Fetched
	for _, f := range fetched {
		if ctx.Err() != nil || stats.aborted {
			return false, nil
		}
		w.ensureMessageKey(f.Email)
		internal, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, f.Email.MessageID)
		if err != nil {
			return false, w.controlPlaneError(err, stats)
		}
		if internal == nil {
			if recovery != nil && recovery.KnownOnly {
				w.imapImportCursor(w.SmtpImapData.folderPath, f.Email.UID, false, recovery)
				continue
			}
			if recovery == nil {
				fresh = append(fresh, f)
				continue
			}
			done, err := w.imapStoreFetched(ctx, f, backfill, stats, recovery)
			if err != nil || !done {
				return false, err
			}
			w.imapImportCursor(w.SmtpImapData.folderPath, f.Email.UID, false, recovery)
			continue
		}
		if backfill && recovery == nil {
			// Initial history skips known maps; generation recovery also reconciles them.
			continue
		}
		internalID, perr := uuid.Parse(internal.ID)
		if perr != nil {
			continue
		}
		if err := w.onEvent(models.JobEventTypeEmailUpdate, &models.JobEventEmailUpdate{
			UserID:     w.UserID,
			EmailID:    w.ID,
			ID:         internalID,
			UID:        f.Email.UID,
			ModSeq:     f.Email.ModSeq,
			Mailbox:    w.SmtpImapData.mailbox,
			FolderPath: w.SmtpImapData.folderPath,
			Folder:     w.SmtpImapData.folder,
			Flags:      f.Email.Flags,
		}); err != nil {
			return false, w.controlPlaneError(err, stats)
		}
		if recovery != nil {
			w.imapImportCursor(w.SmtpImapData.folderPath, f.Email.UID, false, recovery)
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

	all := true
	for _, f := range fresh {
		done, err := w.imapStoreFetched(ctx, f, backfill, stats, recovery)
		if err != nil {
			return false, err
		}
		if !done {
			all = false
			if backfill || stats.aborted || ctx.Err() != nil {
				return false, nil
			}
			continue
		}
		if backfill {
			w.imapImportCursor(w.SmtpImapData.folderPath, f.Email.UID, false, recovery)
		}
	}
	return all, nil
}

func (w *WMail) imapStoreFetched(ctx context.Context, f *imap.Fetched, backfill bool, stats *tickStats, recovery *imapFolderRecovery) (bool, *errx.MailError) {
	if stats.aborted || ctx.Err() != nil {
		return false, nil
	}
	if backfill && w.imapImportCount(recovery) >= w.gov.Policy().BackfillMessages {
		// A capped initial import still reconciles known handles, without admitting more history.
		return recovery != nil && recovery.Initial, nil
	}
	if !w.admit(ctx, w.laneOf(ctx, f.Email.MessageID, f.Email, backfill), stats) {
		return false, nil
	}
	if err := w.SmtpImapData.ImapClient.FetchBody(f); err != nil {
		if err.Code == errx.MailErrorCodeNotFound {
			return true, nil
		}
		return false, err
	}
	if err := w.imapStore(ctx, f.Email); err != nil {
		return false, w.controlPlaneError(err, stats)
	}
	w.laneCache.forget(f.Email.MessageID)
	if backfill {
		if recovery != nil && !recovery.Initial {
			recovery.Synced++
		} else {
			w.tracker.state.BackfillSynced++
			w.tracker.mark()
		}
	}
	return true, nil
}

// ensureMessageKey gives a message without a Message-ID header one that is
// stable for this mailbox, because the empty string is not a key: the map
// endpoint refuses it with 400 and the failed lookup ends the whole sync pass
// with its cursors held, so ONE legacy or malformed sender parked every later
// message on the account for good.
//
// Folder name, UIDVALIDITY and UID: RFC 9051 makes that triple the identity of
// a message on a server, which is what is left when the sender gave it none of
// its own. It re-derives to the same string on the next pass, so the message is
// recognised as known rather than stored again, and it cannot collide with a
// real Message-ID.
//
// The folder name is in it deliberately, even though a RENAME keeps UIDVALIDITY
// and would therefore change the key. Dropping it would key on a pair two
// folders can in principle share, and the failure there is a message silently
// treated as already stored. A rename re-importing the handful of messages that
// carried no Message-ID is the cheaper of the two.
//
// Threading is unaffected: a message with no Message-ID roots its own thread
// on this key, and nothing can ever reply to an id that was never on the wire.
func (w *WMail) ensureMessageKey(msg *models.EmailMessageData) {
	if msg == nil || strings.TrimSpace(msg.MessageID) != "" {
		return
	}
	msg.MessageID = fmt.Sprintf("no-msgid/%s/%d/%d", w.SmtpImapData.folderPath, w.SmtpImapData.mailbox, msg.UID)
}

// threadParentID is the message this one answers, and the key its thread is
// built on. Only In-Reply-To carries that.
//
// Reply-To must not be used here. It is an address header -- "send replies to
// this mailbox" -- not a message identifier, so keying a thread on it puts
// every message a sender ever sent into one strand. On a production instance
// that collapsed 484 of 1431 stored messages into 62 threads: 76 unrelated
// DMARC aggregate reports from one reporter arrived as a single 76-message
// conversation, and a mailbox's own test sends and live outreach merged
// together.
//
// A message that answers nothing has no parent, and the caller roots its
// thread on its own Message-ID.
//
// A blank entry is skipped rather than returned: an empty parent id is not a
// key either, and the map lookup it would cause ends the pass exactly as a
// missing Message-ID used to (see ensureMessageKey).
func threadParentID(msg *models.EmailMessageData) string {
	if msg == nil {
		return ""
	}
	for i := len(msg.InReplyTo) - 1; i >= 0; i-- {
		if id := strings.TrimSpace(msg.InReplyTo[i]); id != "" {
			return id
		}
	}
	return ""
}

// imapStore threads a new message and hands it to storeNew.
func (w *WMail) imapStore(ctx context.Context, msg *models.EmailMessageData) error {
	msg.ID = uuid.New()
	now := time.Now()

	var threadID string
	parentID := threadParentID(msg)

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
		FolderPath:   w.SmtpImapData.folderPath,
		Folder:       w.SmtpImapData.folder,
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
		Seen:         models.SeenFromFlags(msg.Flags),
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
		key := box.Name
		cur := w.tracker.folder(key)
		if imapRecoveryCursor(cur) != nil {
			allDone = allDone && cur.Done
			continue
		}
		if cur.Done {
			continue
		}
		if st.BackfillSynced >= policy.BackfillMessages {
			w.tracker.completeBackfill(time.Now())
			return nil
		}
		w.setWalking(box)

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
			done, err := w.imapApply(ctx, fetched, true, stats, nil)
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

// imapVirtualFolder, imapBackfillEligible and imapCanonicalFolder classify a
// folder. The rules live in the imap client package, next to the LIST that
// produces the attributes, so the sync loop and the Sent-folder resolver
// cannot drift apart.
func imapVirtualFolder(box *models.Mailbox) bool {
	return imap.IsVirtualFolder(*box)
}

func imapBackfillEligible(box *models.Mailbox) bool {
	return imap.BackfillEligible(*box)
}

func imapCanonicalFolder(box *models.Mailbox) string {
	return imap.CanonicalFolder(*box)
}

// controlPlaneError handles a failed map lookup, body store or event publish
// the way the old loop did: log it, hold every cursor by ending the pass, and
// retry next tick. It is not a mailbox error, so nothing is relayed to the
// consumer and no error record is written for a control-plane hiccup.
func (w *WMail) controlPlaneError(err error, stats *tickStats) *errx.MailError {
	nodeevidence.Emit(nodeevidence.ControlPlaneHeld, w.ID, evidenceHTTPStatus(err), 1)
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

// FindPair is the stored copy of a listed folder, matched on the folder's
// identity: its name.
func (w *SmtpImapData) FindPair(m *models.Mailbox) *models.Mailbox {
	for _, f := range w.Mailboxes {
		if f.Name == m.Name {
			return f
		}
	}
	return nil
}

// setWalking records which folder the pass is inside. Every message stored or
// updated from here is stamped with all three: the folder's name, which is
// its identity, the UIDVALIDITY generation its uid belongs to, and the
// canonical folder the dashboard files it under.
func (w *WMail) setWalking(box *models.Mailbox) {
	w.SmtpImapData.mailbox = box.UIDValidity
	w.SmtpImapData.folderPath = box.Name
	w.SmtpImapData.folder = imapCanonicalFolder(box)
}

// imapFollowRenames matches a folder that left the listing to one that
// arrived carrying its UIDVALIDITY, and relays the pair as a rename.
//
// That is what an IMAP RENAME looks like from a LIST: RENAME keeps
// UIDVALIDITY and every UID, so the cursor we hold is still good and the
// folder's history does not need re-importing. Read as a delete plus a first
// sighting it would be both, and the mail filed under the old name would be
// left pointing at a folder that no longer exists.
//
// A rename is only claimed when the UIDVALIDITY has exactly one folder on
// each side of it: one stored folder that is gone, and one listed folder that
// is new. Anything else is a guess. On a server that stamps UIDVALIDITY from
// a creation time a whole tree shares one number, so two stored folders can
// go missing while one arrives, and picking either would move the wrong
// folder's mail into it. Those fall through to the ordinary delete and
// first-sight paths, which lose nothing that was not already gone.
func (w *WMail) imapFollowRenames(folders []models.Mailbox, present map[string]struct{}) error {
	// Group both sides by UIDVALIDITY: the stored folders that are no longer
	// listed, and the listed folders that are not stored.
	gone := map[uint32][]*models.Mailbox{}
	for _, before := range w.SmtpImapData.Mailboxes {
		if _, still := present[before.Name]; still || before.UIDValidity == 0 {
			continue
		}
		gone[before.UIDValidity] = append(gone[before.UIDValidity], before)
	}
	if len(gone) == 0 {
		return nil
	}

	arrived := map[uint32][]*models.Mailbox{}
	for i := range folders {
		f := &folders[i]
		if f.UIDValidity == 0 || w.SmtpImapData.FindPair(f) != nil {
			continue
		}
		arrived[f.UIDValidity] = append(arrived[f.UIDValidity], f)
	}

	for uidValidity, before := range gone {
		to := arrived[uidValidity]
		if len(before) != 1 || len(to) != 1 {
			continue
		}
		from := before[0]

		if err := w.onEvent(models.JobEventTypeMailboxRename, &models.JobEventMailboxRename{
			UserID:  w.UserID,
			EmailID: w.ID,
			From:    from.Name,
			To:      to[0].Name,
		}); err != nil {
			return err
		}
		// The worker's own per-folder state is keyed by name too, so it moves
		// with the folder or the backfill restarts and the flag scan
		// re-baselines for a change of label.
		if scan, ok := w.flagScan[from.Name]; ok {
			delete(w.flagScan, from.Name)
			w.flagScan[to[0].Name] = scan
		}
		if l, ok := w.listed[from.Name]; ok {
			delete(w.listed, from.Name)
			w.listed[to[0].Name] = l
		}
		if w.skipPending[from.Name] {
			delete(w.skipPending, from.Name)
			w.skipPending[to[0].Name] = true
		}
		w.tracker.renameFolder(from.Name, to[0].Name)
		from.Name = to[0].Name
	}
	return nil
}
