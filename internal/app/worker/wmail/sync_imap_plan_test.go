package wmail

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestPlanMailboxSync_FirstSyncFull(t *testing.T) {
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 10, HighestModSeq: 5, UIDNext: 11}
	p := planMailboxSync(nil, cur)
	if !p.Fetch || !p.Full || p.LastModSeq != 0 {
		t.Fatalf("first sync want full Fetch lastMod=0, got %+v", p)
	}
}

func TestPlanMailboxSync_NoChange(t *testing.T) {
	bef := &models.Mailbox{UIDValidity: 1, NumMessages: 10, HighestModSeq: 5, UIDNext: 11}
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 10, HighestModSeq: 5, UIDNext: 11}
	p := planMailboxSync(bef, cur)
	if p.Fetch {
		t.Fatalf("unchanged STATUS must not fetch, got %+v", p)
	}
}

func TestPlanMailboxSync_ModSeqAdvanceCONDSTORE(t *testing.T) {
	bef := &models.Mailbox{UIDValidity: 1, NumMessages: 10, HighestModSeq: 5, UIDNext: 11}
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 12, HighestModSeq: 9, UIDNext: 13}
	p := planMailboxSync(bef, cur)
	if !p.Fetch || !p.Full || p.LastModSeq != 5 {
		t.Fatalf("modseq advance want full Fetch lastMod=5, got %+v", p)
	}
}

func TestPlanMailboxSync_HostingerCondstoreStallExistsGrew(t *testing.T) {
	// HighestModSeq flat, EXISTS grew: the Hostinger pilot failure mode.
	bef := &models.Mailbox{UIDValidity: 1, NumMessages: 200, HighestModSeq: 42, UIDNext: 201}
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 203, HighestModSeq: 42, UIDNext: 204}
	p := planMailboxSync(bef, cur)
	if !p.Fetch {
		t.Fatal("exists growth must fetch despite flat modseq")
	}
	if p.Full {
		t.Fatalf("exists-only growth should fetch new window only, got full %+v", p)
	}
	if p.LastModSeq != 0 {
		t.Fatalf("stall fallback must use ChangedSince 0, got lastMod=%d", p.LastModSeq)
	}
	if p.Lo != 201 || p.Hi != 203 {
		t.Fatalf("want seq window 201-203, got lo=%d hi=%d", p.Lo, p.Hi)
	}
}

func TestPlanMailboxSync_UIDNextOnlyFullFallback(t *testing.T) {
	// UIDNEXT advanced, EXISTS flat (e.g. expunge+append) → full walk.
	bef := &models.Mailbox{UIDValidity: 1, NumMessages: 50, HighestModSeq: 7, UIDNext: 100}
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 50, HighestModSeq: 7, UIDNext: 105}
	p := planMailboxSync(bef, cur)
	if !p.Fetch || !p.Full || p.LastModSeq != 0 {
		t.Fatalf("uidnext-only want full Fetch lastMod=0, got %+v", p)
	}
}

func TestPlanMailboxSync_ExistsDecreasedNoFetch(t *testing.T) {
	bef := &models.Mailbox{UIDValidity: 1, NumMessages: 50, HighestModSeq: 7, UIDNext: 100}
	cur := &models.Mailbox{UIDValidity: 1, NumMessages: 48, HighestModSeq: 7, UIDNext: 100}
	p := planMailboxSync(bef, cur)
	if p.Fetch {
		t.Fatalf("exists shrink alone must not fetch, got %+v", p)
	}
}
