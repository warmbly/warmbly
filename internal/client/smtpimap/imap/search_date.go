package imap

import (
	"errors"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// go-imap always writes a SEARCH date quoted, which RFC 3501 allows and some
// servers refuse (Seznam.cz: "Bad token (expecting date specification)").
// The encoder cannot be told otherwise and the socket sits under TLS, so on
// such a server the SINCE set is computed here from each message's
// INTERNALDATE, which is the same date SINCE compares against.

// selection is one successful SELECT.
type selection struct {
	name        string
	uidValidity uint32
	count       uint32
	modSeq      uint64
}

// dateScan is the INTERNALDATE read of one folder for one window. UIDs below
// next have been read; new mail only ever lands above it.
type dateScan struct {
	uidValidity uint32
	day         time.Time
	next        imap.UID
	uids        []imap.UID
}

// searchRefused reports whether a SEARCH failed because the server would not
// take the command, as opposed to the folder, the session or the account.
func searchRefused(err error) bool {
	var imapErr *imap.Error
	if !errors.As(err, &imapErr) {
		return false
	}
	switch imapErr.Type {
	case imap.StatusResponseTypeBad:
		return true
	case imap.StatusResponseTypeNo:
		switch imapErr.Code {
		case "", imap.ResponseCodeParse, imap.ResponseCodeClientBug, imap.ResponseCodeCannot:
			return true
		}
	}
	return false
}

// searchSinceByDate answers SearchSince without a dated SEARCH. The first call
// per folder reads every message's INTERNALDATE; later calls read only what
// arrived above the last UID seen, because an internal date never changes.
func (c *Client) searchSinceByDate(since time.Time) ([]imap.UID, *errx.MailError) {
	day := calendarDay(since)
	sel := c.selection.Load()
	if sel != nil && sel.count == 0 {
		return nil, nil
	}

	c.scanMu.Lock()
	defer c.scanMu.Unlock()

	// Without a UIDVALIDITY a recreated folder looks the same, so nothing is
	// kept for it.
	cacheable := sel != nil && sel.uidValidity != 0
	scan := &dateScan{day: day, next: 1}
	if cacheable {
		scan.uidValidity = sel.uidValidity
		if prev := c.dateScans[sel.name]; prev != nil && prev.uidValidity == sel.uidValidity && prev.day.Equal(day) {
			scan = prev
		}
	}

	found, top, err := c.fetchInternalDates(scan.next, day)
	if err != nil {
		return nil, err
	}
	// A SELECT that landed mid-scan means the answer may be another folder's.
	if !cacheable || c.selection.Load() != sel {
		return append(slices.Clone(scan.uids), found...), nil
	}
	scan.uids = append(scan.uids, found...)
	if top >= scan.next {
		scan.next = top + 1
	}
	if c.dateScans == nil {
		c.dateScans = map[string]*dateScan{}
	}
	c.dateScans[sel.name] = scan
	return slices.Clone(scan.uids), nil
}

// fetchInternalDates reads UID and INTERNALDATE for every message at or above
// from, and returns the UIDs dated on or after day, ascending, plus the
// highest UID seen.
func (c *Client) fetchInternalDates(from imap.UID, day time.Time) ([]imap.UID, imap.UID, *errx.MailError) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()

	var set imap.UIDSet
	set.AddRange(from, 0)
	cmd := c.client.Fetch(set, &imap.FetchOptions{UID: true, InternalDate: true})
	var found []imap.UID
	var top imap.UID
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		var uid imap.UID
		var at time.Time
		for item := msg.Next(); item != nil; item = msg.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				uid = item.UID
			case imapclient.FetchItemDataInternalDate:
				at = item.Time
			}
		}
		// "n:*" past the end answers with the last message (RFC 3501 6.4.8).
		if uid < from {
			continue
		}
		top = max(top, uid)
		// No date to compare is kept: the backfill's cap bounds an extra
		// message, a missed one is simply gone.
		if at.IsZero() || !calendarDay(at).Before(day) {
			found = append(found, uid)
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, 0, c.handleError(err)
	}
	slices.Sort(found)
	return found, top, nil
}

// calendarDay is t's date as SINCE reads it: the day in t's own zone, with
// the time and the zone dropped.
func calendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// host is the server this client dials, for logs.
func (c *Client) host() string {
	switch c.AuthType {
	case models.AuthPlain:
		if c.Credentials != nil {
			return c.Credentials.Host
		}
	case models.AuthOAuth2:
		if c.Oauth2 != nil {
			return c.Oauth2.Host
		}
	}
	return ""
}
