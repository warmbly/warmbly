package imap

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// putMessage appends one message to INBOX and returns its UID.
func putMessage(t *testing.T, c *Client) imap.UID {
	t.Helper()
	raw := "From: sender@test\r\n" +
		"To: warmbly@test\r\n" +
		"Subject: hello\r\n" +
		"Message-ID: <fetch-caps@test>\r\n" +
		"\r\n" +
		"body\r\n"
	cmd := c.client.Append("INBOX", int64(len(raw)), nil)
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatalf("append write: %v", err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatalf("append close: %v", err)
	}
	data, err := cmd.Wait()
	if err != nil {
		t.Fatalf("append wait: %v", err)
	}
	return data.UID
}

// Issue #405. MODSEQ is a CONDSTORE fetch item (RFC 7162), and this package
// asked for it on every FETCH regardless of what the server advertised. A
// lenient server takes it anyway, which is why every test here passed; IONOS
// answered `BAD expected fetch-att instead of "MODSEQ BODY.P"` and the
// mailbox synced nothing, every pass, for as long as it was connected.
//
// go-imap emits the fetch items from a map, so MODSEQ lands in a different
// position each time and the failure is not even reproducible in the same
// shape twice. Assert on the command, not on this server's tolerance.
func TestFetchEnvelopesOmitsModSeqWithoutCondStore(t *testing.T) {
	c, wire := recordingServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.HasCondStore() {
		t.Fatal("the test server must not advertise CONDSTORE")
	}
	uid := putMessage(t, c)
	if _, err := c.SelectForSync("INBOX"); err != nil {
		t.Fatalf("SelectForSync: %v", err)
	}

	got, xerr := c.FetchEnvelopes(context.Background(), []imap.UID{uid})
	if xerr != nil {
		t.Fatalf("FetchEnvelopes: %s", xerr.Message)
	}
	if len(got) != 1 {
		t.Fatalf("fetched %d messages, want 1", len(got))
	}

	fetches := wire.commands("FETCH")
	if len(fetches) == 0 {
		t.Fatal("no FETCH reached the server")
	}
	for _, f := range fetches {
		if strings.Contains(f, "MODSEQ") {
			t.Errorf("asked a server without CONDSTORE for MODSEQ: %s", f)
		}
	}
}

// The other half: gating it must not quietly cost the CONDSTORE path its
// mod-sequences, which are what the incremental sync keys on there. The
// in-process server cannot advertise CONDSTORE (go-imap's server relays a
// fixed allowlist that has no room for it), so the capability is set on the
// client the way a real server's post-auth CAPABILITY would.
func TestFetchEnvelopesAsksModSeqWithCondStore(t *testing.T) {
	c, wire := recordingServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	uid := putMessage(t, c)
	if _, err := c.SelectForSync("INBOX"); err != nil {
		t.Fatalf("SelectForSync: %v", err)
	}
	c.condStore.Store(true)
	selected := *c.selection.Load()
	selected.modSeq = 1
	c.selection.Store(&selected)
	c.syncView = &selected

	// The result is deliberately ignored: this server refuses MODSEQ when
	// CONDSTORE was not enabled, which is #405's failure reproduced in
	// miniature. What is asserted is the command we chose to send.
	_, _ = c.FetchEnvelopes(context.Background(), []imap.UID{uid})

	var asked bool
	for _, f := range wire.commands("FETCH") {
		if strings.Contains(f, "MODSEQ") {
			asked = true
		}
	}
	if !asked {
		t.Error("a CONDSTORE server was not asked for MODSEQ; the incremental sync loses its cursor")
	}
}

// Same class as #405, one command over: LIST RETURN options are LIST-EXTENDED
// grammar (RFC 5258), and SPECIAL-USE does not imply it. A server advertising
// the folder attributes without the extended LIST would be sent a RETURN it
// cannot parse, and a BAD there costs the account every folder.
func TestListOptionsNeedListExtended(t *testing.T) {
	status := &imap.StatusOptions{UIDValidity: true, UIDNext: true}

	for _, tc := range []struct {
		name        string
		caps        imap.CapSet
		wantStatus  bool
		wantSpecial bool
	}{
		{
			name: "plain RFC 3501 gets no RETURN at all",
			caps: imap.CapSet{imap.CapIMAP4rev1: {}},
		},
		{
			name: "SPECIAL-USE without LIST-EXTENDED is not enough",
			caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapSpecialUse: {}},
		},
		{
			name: "LIST-STATUS without LIST-EXTENDED is not enough",
			caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapListStatus: {}},
		},
		{
			name:        "LIST-EXTENDED unlocks what is also advertised",
			caps:        imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapListExtended: {}, imap.CapSpecialUse: {}},
			wantSpecial: true,
		},
		{
			name:       "LIST-EXTENDED with LIST-STATUS folds STATUS in",
			caps:       imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapListExtended: {}, imap.CapListStatus: {}},
			wantStatus: true,
		},
		{
			// IMAP4rev2 implies LIST-EXTENDED and LIST-STATUS.
			name:        "IMAP4rev2 implies the extended LIST",
			caps:        imap.CapSet{imap.CapIMAP4rev2: {}, imap.CapSpecialUse: {}},
			wantStatus:  true,
			wantSpecial: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, listStatus := listOptionsFor(tc.caps, status)
			if got := opts.ReturnStatus != nil; got != tc.wantStatus {
				t.Errorf("ReturnStatus = %v, want %v", got, tc.wantStatus)
			}
			if listStatus != tc.wantStatus {
				t.Errorf("listStatus = %v, want %v", listStatus, tc.wantStatus)
			}
			if opts.ReturnSpecialUse != tc.wantSpecial {
				t.Errorf("ReturnSpecialUse = %v, want %v", opts.ReturnSpecialUse, tc.wantSpecial)
			}
		})
	}
}

// End to end on the wire: a plain RFC 3501 server must be sent a bare LIST.
func TestFoldersSendsBareListWithoutListExtended(t *testing.T) {
	c, wire := recordingServer(t, nil, "Sent")
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	boxes, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders: %s", err.Message)
	}
	if len(boxes) != 2 {
		t.Fatalf("listed %d folders, want 2", len(boxes))
	}
	lists := wire.commands("LIST")
	if len(lists) == 0 {
		t.Fatal("no LIST reached the server")
	}
	for _, l := range lists {
		if strings.Contains(l, "RETURN") {
			t.Errorf("sent LIST-EXTENDED grammar to a server that never advertised it: %s", l)
		}
	}
}
