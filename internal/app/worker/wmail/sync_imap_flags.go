package wmail

import (
	"context"
	"hash/fnv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// reportFolderOverflow records what the folder listing could not follow, as
// state rather than as an error raised once.
//
// Both conditions are things the user can fix (get under the folder cap, or
// stop the server listing one name twice), and an error row is
// never withdrawn once written, so raising one meant a red "needs attention"
// that stayed after the problem was gone. Relaying the counts every pass
// makes the warning disappear on its own.
func (w *WMail) reportFolderOverflow() {
	client := w.SmtpImapData.ImapClient
	w.tracker.setFoldersSkipped(client.FolderOverflow(), client.FolderConflicts())
}

// imapScanFlags mirrors read state and flag changes on a server without
// CONDSTORE, which cannot say what changed: it re-reads the flags of the
// folder's newest window and relays only the messages whose flags actually
// moved since the previous scan.
//
// It is deliberately periodic (config.ImapFlagScanInterval) rather than every
// pass: it is one FETCH per folder over up to config.ImapFlagScanWindow UIDs,
// and read state is not worth a round trip a minute per folder. New mail does
// not wait for it; that arrives through UIDNEXT on every pass.
func (w *WMail) imapScanFlags(ctx context.Context, box *models.Mailbox, stats *tickStats) *errx.MailError {
	if w.flagScan == nil {
		w.flagScan = map[string]*folderFlagScan{}
	}
	scan := w.flagScan[box.Name]
	now := time.Now()
	if scan != nil && now.Sub(scan.at) < config.ImapFlagScanInterval {
		return nil
	}

	from := uint32(1)
	if box.UIDNext > config.ImapFlagScanWindow {
		from = box.UIDNext - config.ImapFlagScanWindow
	}
	flags, err := w.SmtpImapData.ImapClient.FetchFlags(ctx, from)
	if err != nil {
		return err
	}

	// The first scan of a folder only records the baseline: without a
	// previous scan every message would read as changed and the whole window
	// would be relayed for nothing.
	next := make(map[uint32]uint64, len(flags))
	for uid, state := range flags {
		next[uid] = flagFingerprint(state.Flags)
	}
	if scan != nil {
		for uid, state := range flags {
			before, ok := scan.flags[uid]
			// Not in the previous scan means it arrived since; the UIDNEXT
			// path owns it and will store it with its flags.
			if !ok || before == next[uid] {
				continue
			}
			if err := w.relayFlags(ctx, box, uid, state, stats); err != nil {
				return err
			}
			if stats.aborted {
				scan.at = now
				return nil
			}
		}
	}
	w.flagScan[box.Name] = &folderFlagScan{at: now, flags: next}
	return nil
}

// relayFlags sends an UPDATE_EMAIL for one message whose flags moved. A
// message the platform does not know is skipped: it is not ours to update,
// and the message paths admit it under a budget instead.
func (w *WMail) relayFlags(ctx context.Context, box *models.Mailbox, uid uint32, state imap.FlagState, stats *tickStats) *errx.MailError {
	if state.MessageID == "" {
		return nil
	}
	internal, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, state.MessageID)
	if err != nil {
		return w.controlPlaneError(err, stats)
	}
	if internal == nil {
		return nil
	}
	internalID, perr := uuid.Parse(internal.ID)
	if perr != nil {
		return nil
	}
	if err := w.onEvent(models.JobEventTypeEmailUpdate, &models.JobEventEmailUpdate{
		UserID:     w.UserID,
		EmailID:    w.ID,
		ID:         internalID,
		UID:        uid,
		Mailbox:    box.UIDValidity,
		FolderPath: box.Name,
		Folder:     imapCanonicalFolder(box),
		Flags:      state.Flags,
	}); err != nil {
		return w.controlPlaneError(err, stats)
	}
	return nil
}

// folderFlagScan is the previous scan of one folder, held in worker memory
// only: a replaced worker re-baselines on its first scan, which costs one
// FETCH and no wrong updates.
//
// A fingerprint per UID, not the flags themselves. The scan only has to
// answer "did this change", and the current fetch already carries the
// Message-ID for any UID that did, so keeping the strings cost 74 MB per
// mailbox at the window and folder limits against 14 MB for the digests, on
// a worker at its configured workload target.
type folderFlagScan struct {
	at    time.Time
	flags map[uint32]uint64
}

// flagFingerprint digests a message's flag set, order-independently: servers
// do not promise an order and a reordered set is not a change. XOR of the
// per-flag hashes gives that for free, and a duplicate flag cancelling itself
// out is not a case a server produces.
func flagFingerprint(flags []string) uint64 {
	var sum uint64
	for _, f := range flags {
		h := fnv.New64a()
		_, _ = h.Write([]byte(strings.ToLower(f)))
		sum ^= h.Sum64()
	}
	// Distinguish "no flags" from "never seen": a zero fingerprint is a
	// legitimate empty set, and the caller checks presence separately.
	return sum
}
