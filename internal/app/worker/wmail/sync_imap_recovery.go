package wmail

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const imapRecoveryPrefix = "imap-recovery-v1:"
const imapInitialRecoveryPrefix = "imap-initial-generation-v1:"

// Recovery metadata lives in the existing opaque folder continuation, not a new wire field.
type imapFolderRecovery struct {
	Generation uint32    `json:"generation"`
	Since      time.Time `json:"since"`
	Synced     int       `json:"synced"`
	Initial    bool      `json:"-"`
}

func imapRecoveryCursor(cur models.SyncFolderCursor) *imapFolderRecovery {
	raw, ok := strings.CutPrefix(cur.Next, imapRecoveryPrefix)
	initial := false
	if !ok {
		raw, ok = strings.CutPrefix(cur.Next, imapInitialRecoveryPrefix)
		initial = ok
	}
	if !ok {
		return nil
	}
	var recovery imapFolderRecovery
	if json.Unmarshal([]byte(raw), &recovery) != nil || recovery.Generation == 0 || recovery.Since.IsZero() || recovery.Synced < 0 {
		return nil
	}
	recovery.Initial = initial
	return &recovery
}

func (w *WMail) imapRecoverFolder(box *models.Mailbox) bool {
	if !imapBackfillEligible(box) {
		return true
	}
	cur := w.tracker.folder(box.Name)
	if recovery := imapRecoveryCursor(cur); recovery == nil || recovery.Generation != box.UIDValidity {
		since := time.Now().Add(-time.Duration(w.gov.Policy().BackfillDays) * 24 * time.Hour)
		initial := w.tracker.state.BackfillStatus != models.SyncBackfillComplete
		if initial {
			w.tracker.startBackfill(time.Now(), w.gov.Policy().BackfillDays)
			if w.tracker.state.BackfillSince == nil {
				return false
			}
			since = *w.tracker.state.BackfillSince
		}
		w.imapImportCursor(box.Name, 0, false, &imapFolderRecovery{Generation: box.UIDValidity, Since: since, Initial: initial})
	}
	// Publish replay state before a live baseline can make the folder look caught up on reload.
	w.tracker.flush(time.Now())
	return !w.tracker.dirty
}

func (w *WMail) imapRecoveryPending(folders []models.Mailbox) bool {
	for _, box := range folders {
		cur := w.tracker.folder(box.Name)
		if !cur.Done && imapRecoveryCursor(cur) != nil && imapBackfillEligible(&box) {
			return true
		}
	}
	return false
}

func (w *WMail) imapImportCount(recovery *imapFolderRecovery) int {
	if recovery != nil && !recovery.Initial {
		return recovery.Synced
	}
	return w.tracker.state.BackfillSynced
}

func (w *WMail) imapImportCursor(folder string, uid uint32, done bool, recovery *imapFolderRecovery) {
	cur := models.SyncFolderCursor{UID: uid, Done: done}
	if recovery != nil {
		raw, _ := json.Marshal(recovery)
		prefix := imapRecoveryPrefix
		if recovery.Initial {
			// Older workers must not interpret the global import cap as a per-folder recovery cap.
			prefix = imapInitialRecoveryPrefix
		}
		cur.Next = prefix + string(raw)
	}
	w.tracker.setFolder(folder, cur)
}

// Generation recovery uses its original window and the owning import's counters.
func (w *WMail) imapRecoverFolders(ctx context.Context, folders []models.Mailbox, stats *tickStats) *errx.MailError {
	client := w.SmtpImapData.ImapClient
	for i := range folders {
		box := &folders[i]
		cur := w.tracker.folder(box.Name)
		recovery := imapRecoveryCursor(cur)
		if recovery == nil || !imapBackfillEligible(box) {
			continue
		}
		if recovery.Generation != box.UIDValidity {
			if !w.imapRecoverFolder(box) {
				stats.aborted = true
				return nil
			}
			cur = w.tracker.folder(box.Name)
			recovery = imapRecoveryCursor(cur)
			if recovery == nil {
				continue
			}
		}
		if cur.Done {
			continue
		}
		if ctx.Err() != nil || stats.aborted || stats.laneDenied(LaneBackfill) {
			return nil
		}
		if !recovery.Initial && recovery.Synced >= w.gov.Policy().BackfillMessages {
			w.imapImportCursor(box.Name, cur.UID, true, recovery)
			continue
		}
		w.setWalking(box)
		view, err := client.SelectForSyncState(box.Name)
		if err != nil {
			return err
		}
		if view.UIDValidity != 0 && view.UIDValidity != recovery.Generation {
			return errx.ErrMailResourceNotFound
		}
		uids, err := client.SearchSince(recovery.Since)
		if err != nil {
			return err
		}
		remaining := uids[:0:0]
		for _, uid := range uids {
			if cur.UID == 0 || uid < goimap.UID(cur.UID) {
				remaining = append(remaining, uid)
			}
		}
		sort.Slice(remaining, func(i, j int) bool { return remaining[i] > remaining[j] })
		for lo := 0; lo < len(remaining); lo += config.ImapFetchBatchSize {
			hi := min(lo+config.ImapFetchBatchSize, len(remaining))
			fetched, err := client.FetchEnvelopes(ctx, remaining[lo:hi])
			if err != nil {
				return err
			}
			done, err := w.imapApply(ctx, fetched, true, stats, recovery)
			if err != nil {
				return err
			}
			if !done || ctx.Err() != nil {
				return nil
			}
			cur.UID = uint32(remaining[hi-1])
			w.imapImportCursor(box.Name, cur.UID, false, recovery)
		}
		w.imapImportCursor(box.Name, cur.UID, true, recovery)
	}
	return nil
}
