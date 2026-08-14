package wmail

import "github.com/warmbly/warmbly/internal/models"

// mailboxSyncPlan decides how to fetch when SELECT status reports a change.
// CONDSTORE HighestModSeq is preferred; when only EXISTS/UIDNEXT advances
// (Hostinger CONDSTORE stall), walk the new sequence window with ChangedSince 0.
type mailboxSyncPlan struct {
	Fetch      bool
	LastModSeq uint64
	// Full walks 1:selectCount after SELECT. Otherwise Lo/Hi inclusive seqs.
	Full bool
	Lo   uint32
	Hi   uint32
}

// planMailboxSync is pure so Hostinger stall cases stay unit-testable without IMAP.
func planMailboxSync(bef, cur *models.Mailbox) mailboxSyncPlan {
	if cur == nil {
		return mailboxSyncPlan{}
	}
	if bef == nil {
		return mailboxSyncPlan{Fetch: true, LastModSeq: 0, Full: true}
	}

	modseqAdv := cur.HighestModSeq != bef.HighestModSeq
	existsGrew := cur.NumMessages > bef.NumMessages
	uidNextGrew := cur.UIDNext > 0 && cur.UIDNext > bef.UIDNext

	if !modseqAdv && !existsGrew && !uidNextGrew {
		return mailboxSyncPlan{}
	}

	// Normal CONDSTORE path: full mailbox walk filtered by ChangedSince.
	if modseqAdv {
		return mailboxSyncPlan{
			Fetch:      true,
			LastModSeq: bef.HighestModSeq,
			Full:       true,
		}
	}

	// CONDSTORE stall: MODSEQ flat but EXISTS and/or UIDNEXT advanced.
	// Prefer the new sequence window only (cheap). Fall back to full walk
	// when we cannot bound the window (count missing or not grown).
	if existsGrew && cur.NumMessages > 0 {
		lo := bef.NumMessages + 1
		if lo < 1 {
			lo = 1
		}
		hi := cur.NumMessages
		if lo <= hi {
			return mailboxSyncPlan{
				Fetch:      true,
				LastModSeq: 0,
				Full:       false,
				Lo:         lo,
				Hi:         hi,
			}
		}
	}

	return mailboxSyncPlan{Fetch: true, LastModSeq: 0, Full: true}
}
