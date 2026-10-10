package imap

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
)

// changedSinceDialect answers the three forms of "what changed since n" the
// way one server does. A blank answer passes the line to the in-memory server.
type changedSinceDialect struct {
	fetch, esearch, search string
	// exists is the INBOX count its CONDSTORE EXAMINE reports.
	exists int
}

// advertising adds caps to every capability list the server sends, since the
// in-memory server cannot list CONDSTORE or ESEARCH itself.
func advertising(caps string) func(line string) string {
	return func(line string) string {
		for _, marker := range []string{"[CAPABILITY ", "* CAPABILITY "} {
			i := strings.Index(line, marker)
			if i < 0 {
				continue
			}
			end := strings.IndexAny(line[i:], "]\r\n")
			if end < 0 {
				return line
			}
			at := i + end
			return line[:at] + " " + caps + line[at:]
		}
		return line
	}
}

func (d changedSinceDialect) reply(line string) string {
	fields := strings.Fields(line)
	upper := strings.ToUpper(line)
	if len(fields) < 2 {
		return ""
	}
	tag := fields[0]
	var body string
	switch {
	case strings.Contains(upper, "EXAMINE") && strings.Contains(upper, "CONDSTORE"):
		body = fmt.Sprintf("* %d EXISTS\r\n* OK [UIDVALIDITY 1] ok\r\n* OK [UIDNEXT 8] ok\r\n* OK [HIGHESTMODSEQ 21] ok\r\n$TAG OK [READ-ONLY] EXAMINE completed", d.exists)
	case strings.Contains(upper, "CHANGEDSINCE"):
		body = d.fetch
	case strings.Contains(upper, "SEARCH") && strings.Contains(upper, "MODSEQ") && strings.Contains(upper, "RETURN"):
		body = d.esearch
	case strings.Contains(upper, "SEARCH") && strings.Contains(upper, "MODSEQ"):
		body = d.search
	}
	if body == "" {
		return ""
	}
	return strings.ReplaceAll(body, "$TAG", tag)
}

const (
	fetchOK         = "* 1 FETCH (UID 7 MODSEQ (21))\r\n* 2 FETCH (UID 5 MODSEQ (20))\r\n$TAG OK Fetch completed"
	fetchUnreadable = "* 1 FETCH (UID 7 MODSEQ 21)\r\n$TAG OK Fetch completed"
	esearchOK       = `* ESEARCH (TAG "$TAG") UID ALL 5,7 MODSEQ 21` + "\r\n$TAG OK Search completed"
	// Dovecot (OVH and most hosted IMAP) trails a parenthesised MODSEQ.
	esearchDovecot = `* ESEARCH (TAG "$TAG") UID ALL 5,7 (MODSEQ 21)` + "\r\n$TAG OK Search completed"
	searchOK       = "* SEARCH 5 7 (MODSEQ 21)\r\n$TAG OK Search completed"
	// Zoho answers an empty match with a double space.
	searchZohoEmpty = "* SEARCH  (MODSEQ 21)\r\n$TAG OK Search completed"
	// Refusals of the form itself, which leave the session usable.
	fetchRefused   = "$TAG BAD Unknown FETCH modifier CHANGEDSINCE"
	esearchRefused = "$TAG BAD Unknown search return option"
	searchRefusal  = "$TAG NO [CANNOT] MODSEQ search not supported"
	// Answers that say nothing about the form and must stay retryable.
	fetchBareFailure = "$TAG NO UID FETCH failed"
	fetchUnavailable = "$TAG NO [UNAVAILABLE] Try again later"
)

// runChangedSince is one sync step as the worker drives it: reconnect when the
// last step dropped the session, select, ask what changed.
func runChangedSince(t *testing.T, c *Client) ([]imap.UID, *errx.MailError) {
	t.Helper()
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}
	if _, err := c.SelectForSyncState("INBOX"); err != nil {
		t.Fatalf("SelectForSyncState: %v", err)
	}
	return c.SearchChangedSince(10)
}

// Every server dialect ends on a form it answers readably, within one failed
// step per form it cannot, and never on a mailbox that fails every pass.
func TestSearchChangedSinceFindsAReadableForm(t *testing.T) {
	cases := []struct {
		name      string
		dialect   changedSinceDialect
		esearch   bool
		mode      changedSinceMode
		failures  int
		condStore bool
	}{
		{"rfc", changedSinceDialect{fetch: fetchOK}, true, changedSinceFetch, 0, true},
		{"dovecot", changedSinceDialect{fetch: fetchOK, esearch: esearchDovecot, search: searchOK}, true, changedSinceFetch, 0, true},
		{"fetch unreadable, esearch fine", changedSinceDialect{fetch: fetchUnreadable, esearch: esearchOK, search: searchZohoEmpty}, true, changedSinceESearch, 1, true},
		{"fetch and esearch unreadable", changedSinceDialect{fetch: fetchUnreadable, esearch: esearchDovecot, search: searchOK}, true, changedSinceSearch, 2, true},
		{"no esearch skips that form", changedSinceDialect{fetch: fetchUnreadable, search: searchOK}, false, changedSinceSearch, 1, true},
		{"fetch refused, esearch asked in the same pass", changedSinceDialect{fetch: fetchRefused, esearch: esearchOK}, true, changedSinceESearch, 0, true},
		{"refused then unreadable", changedSinceDialect{fetch: fetchRefused, esearch: esearchDovecot, search: searchOK}, true, changedSinceSearch, 1, true},
		{"every form refused falls back to uidnext", changedSinceDialect{fetch: fetchRefused, esearch: esearchRefused, search: searchRefusal}, true, changedSinceNone, 1, false},
		{"nothing readable falls back to uidnext", changedSinceDialect{fetch: fetchUnreadable, esearch: esearchDovecot, search: searchZohoEmpty}, true, changedSinceNone, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := imap.CapSet{imap.CapIMAP4rev1: {}}
			tc.dialect.exists = 2
			extra := "CONDSTORE"
			if tc.esearch {
				extra += " ESEARCH"
			}
			c, _ := rewritingServer(t, caps, tc.dialect.reply, advertising(extra))
			c.IdleTimeout = 5 * time.Second

			failures := 0
			var uids []imap.UID
			for range 5 {
				got, err := runChangedSince(t, c)
				if err == nil {
					uids = got
					break
				}
				if err.Code != errx.MailErrorCodeServerUnreachable {
					t.Fatalf("unexpected error: %v", err)
				}
				failures++
				if !c.HasCondStore() {
					break
				}
			}
			if failures != tc.failures {
				t.Fatalf("failed steps = %d, want %d", failures, tc.failures)
			}
			if got := changedSinceMode(c.changedSince.Load()); got != tc.mode {
				t.Fatalf("mode = %v, want %v", got, tc.mode)
			}
			if c.HasCondStore() != tc.condStore {
				t.Fatalf("HasCondStore = %v, want %v", c.HasCondStore(), tc.condStore)
			}
			if tc.condStore && !slices.Equal(uids, []imap.UID{5, 7}) {
				t.Fatalf("uids = %v, want [5 7]", uids)
			}
			if !tc.condStore {
				// The fallback survives a reconnect, so the next session stays on UIDNEXT.
				if err := c.Connect(); err != nil {
					t.Fatalf("reconnect: %v", err)
				}
				if c.HasCondStore() {
					t.Fatal("HasCondStore came back after reconnect")
				}
			}
		})
	}
}

// Zoho answers an empty plain MODSEQ search with a double space; a server
// that reaches that form keeps working because the quiet pass never asks it.
func TestSearchChangedSinceEmptyViewSkipsTheQuery(t *testing.T) {
	caps := imap.CapSet{imap.CapIMAP4rev1: {}}
	c, wire := rewritingServer(t, caps, changedSinceDialect{fetch: fetchUnreadable, search: searchZohoEmpty}.reply, advertising("CONDSTORE ESEARCH"))
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	uids, err := runChangedSince(t, c)
	if err != nil {
		t.Fatalf("SearchChangedSince: %v", err)
	}
	if len(uids) != 0 {
		t.Fatalf("uids = %v, want none", uids)
	}
	if n := len(wire.commands("FETCH")); n != 0 {
		t.Fatalf("empty view sent %d FETCH commands", n)
	}
}

// A transient NO is about the moment, not the form: it retries on the next
// pass and never costs the server its CONDSTORE path.
func TestSearchChangedSinceKeepsTheFormOnTransientAnswers(t *testing.T) {
	for name, fetch := range map[string]string{"bare failure": fetchBareFailure, "unavailable": fetchUnavailable} {
		t.Run(name, func(t *testing.T) {
			d := changedSinceDialect{fetch: fetch, esearch: esearchOK, search: searchOK, exists: 2}
			c, _ := rewritingServer(t, imap.CapSet{imap.CapIMAP4rev1: {}}, d.reply, advertising("CONDSTORE ESEARCH"))
			for range 3 {
				_, err := runChangedSince(t, c)
				if err == nil || err.Code != errx.MailErrorCodeServerUnreachable {
					t.Fatalf("err = %v, want a retryable SERVER_UNREACHABLE", err)
				}
			}
			if got := changedSinceMode(c.changedSince.Load()); got != changedSinceFetch {
				t.Fatalf("mode = %v, want %v", got, changedSinceFetch)
			}
			if !c.HasCondStore() {
				t.Fatal("a transient answer turned CONDSTORE off")
			}
		})
	}
}

func TestUnreadableReplyIsNotTheNetwork(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		{`in response-data: imapwire: expected atom, got "("`, true},
		{`in search-sort-mod-seq: expected "MODSEQ", got "X"`, true},
		{"in response: cannot read tag: read tcp 1.2.3.4:993: connection reset by peer", false},
		{"in response-data: unexpected EOF", false},
		{"use of closed network connection", false},
	} {
		if got := unreadableReply(stringErr(tc.msg)); got != tc.want {
			t.Errorf("unreadableReply(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
	if unreadableReply(&imap.Error{Type: imap.StatusResponseTypeBad, Text: "imapwire: nope"}) {
		t.Error("a tagged BAD is the server refusing, not an unreadable reply")
	}
}

type stringErr string

func (e stringErr) Error() string { return string(e) }
