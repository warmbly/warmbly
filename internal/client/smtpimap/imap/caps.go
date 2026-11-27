package imap

import (
	"slices"

	"github.com/emersion/go-imap/v2"
)

// caps is the capability set this mailbox's sync acts on: usableCaps, and
// plainCaps once the server has refused a command with BAD (see handleError).
func (c *Client) caps() imap.CapSet {
	caps := usableCaps(c.client.Caps())
	if c.plainCommands.Load() {
		return plainCaps(caps)
	}
	return caps
}

// extendedCommandCaps change the shape of LIST, STATUS, SEARCH, SELECT and
// FETCH. sending.ac's relay keeps advertising LIST-STATUS after sign-in while
// Exchange behind it answers LIST ... RETURN (STATUS ...) with "BAD Command
// Argument Error. 12", so a mailbox whose server has refused a command once
// drops them all. IDLE, MOVE, UIDPLUS, NAMESPACE and UNSELECT stay: they add
// commands rather than reshape existing ones.
var extendedCommandCaps = []imap.Cap{
	imap.CapIMAP4rev2, imap.CapESearch, imap.CapSearchRes, imap.CapListExtended, imap.CapListStatus,
	imap.CapSpecialUse, imap.CapCondStore, imap.CapQResync, imap.CapStatusSize,
}

// plainCaps is caps without extendedCommandCaps.
func plainCaps(caps imap.CapSet) imap.CapSet {
	out := make(imap.CapSet, len(caps))
	for c := range caps {
		if !slices.Contains(extendedCommandCaps, c) {
			out[c] = struct{}{}
		}
	}
	return out
}

// usableCaps speaks IMAP4rev1 to a server that offers both IMAP4rev1 and
// IMAP4rev2. go-imap reads IMAP4rev2 as ESEARCH, LIST-STATUS, LIST-EXTENDED,
// STATUS=SIZE and more, and proxies in front of Microsoft Exchange (sending.ac's
// inboxalways.com hosts) advertise IMAP4rev2 while the Exchange server behind
// them rejects those commands with "BAD Command Argument Error. 12". Servers
// that really support these extensions (Dovecot, for one) also list them by
// name, so nothing they offer is lost.
func usableCaps(caps imap.CapSet) imap.CapSet {
	_, rev1 := caps[imap.CapIMAP4rev1]
	_, rev2 := caps[imap.CapIMAP4rev2]
	if !rev1 || !rev2 {
		return caps
	}
	out := make(imap.CapSet, len(caps))
	for c := range caps {
		if c != imap.CapIMAP4rev2 {
			out[c] = struct{}{}
		}
	}
	return out
}
