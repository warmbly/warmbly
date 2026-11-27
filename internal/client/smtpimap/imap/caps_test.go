package imap

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func capSet(caps ...imap.Cap) imap.CapSet {
	set := imap.CapSet{}
	for _, c := range caps {
		set[c] = struct{}{}
	}
	return set
}

func TestUsableCapsSpeaksRev1ToExchangeProxy(t *testing.T) {
	// What tryjustbadgecom157e.austin.inboxalways.com advertised on 25 Nov 2026.
	got := usableCaps(capSet(imap.CapIMAP4rev2, imap.CapIMAP4rev1, imap.CapSASLIR, imap.CapLiteralMinus, "AUTH=PLAIN"))
	for _, c := range []imap.Cap{imap.CapIMAP4rev2, imap.CapESearch, imap.CapListStatus, imap.CapListExtended, imap.CapStatusSize, imap.CapIdle, imap.CapMove} {
		if got.Has(c) {
			t.Errorf("proxy caps still report %s", c)
		}
	}
	if !got.Has(imap.CapSASLIR) || !got.Has(imap.CapLiteralMinus) {
		t.Errorf("listed capabilities were dropped: %v", got)
	}
}

func TestUsableCapsKeepsNamedExtensionsAndRev2Only(t *testing.T) {
	dovecot := usableCaps(capSet(imap.CapIMAP4rev1, imap.CapIMAP4rev2, imap.CapESearch, imap.CapIdle, imap.CapMove))
	if !dovecot.Has(imap.CapESearch) || !dovecot.Has(imap.CapIdle) || !dovecot.Has(imap.CapMove) {
		t.Errorf("named extensions were dropped: %v", dovecot)
	}
	rev2Only := usableCaps(capSet(imap.CapIMAP4rev2))
	if !rev2Only.Has(imap.CapESearch) {
		t.Errorf("a rev2-only server lost its implied extensions: %v", rev2Only)
	}
}

func TestPlainCapsDropsReshapingExtensionsOnly(t *testing.T) {
	// What a relay could still claim after sign-in.
	got := plainCaps(capSet(imap.CapIMAP4rev1, imap.CapListExtended, imap.CapListStatus, imap.CapSpecialUse, imap.CapESearch, imap.CapCondStore, imap.CapIdle, imap.CapMove, imap.CapUIDPlus))
	for _, c := range []imap.Cap{imap.CapListExtended, imap.CapListStatus, imap.CapSpecialUse, imap.CapESearch, imap.CapCondStore} {
		if got.Has(c) {
			t.Errorf("plain caps still report %s", c)
		}
	}
	for _, c := range []imap.Cap{imap.CapIMAP4rev1, imap.CapIdle, imap.CapMove, imap.CapUIDPlus} {
		if !got.Has(c) {
			t.Errorf("plain caps dropped %s", c)
		}
	}
	if opts, listStatus := listOptionsFor(got, &imap.StatusOptions{NumMessages: true}); listStatus || opts.ReturnStatus != nil || opts.ReturnSpecialUse {
		t.Errorf("plain caps still build an extended LIST: %+v", opts)
	}
}

func TestBadReplySwitchesMailboxToPlainCommands(t *testing.T) {
	c := &Client{}
	c.eSearch.Store(true)
	_ = c.handleError(&imap.Error{Type: imap.StatusResponseTypeNo, Text: "nope"})
	if c.plainCommands.Load() {
		t.Fatal("a NO switched the mailbox to plain commands")
	}
	_ = c.handleError(&imap.Error{Type: imap.StatusResponseTypeBad, Text: "Command Argument Error. 12"})
	if !c.plainCommands.Load() || c.eSearch.Load() {
		t.Fatal("a BAD did not switch the mailbox to plain commands")
	}
}
