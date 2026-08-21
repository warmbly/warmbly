package imap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

// ErrNoSentMailbox means the account has no folder that can be identified as
// Sent. Callers treat it as "nothing to do", not as a failure: the message was
// still delivered.
var ErrNoSentMailbox = errors.New("no sent mailbox")

// AppendToSent files a copy of an outbound message in the account's Sent
// folder, flagged \Seen and dated when it was sent.
//
// SMTP submission does not put anything in the sender's mailbox, so without
// this a message sent through Warmbly exists only in the recipient's inbox: it
// shows in neither the customer's own mail client nor the unibox, which can
// only display what the sync found in a folder.
//
// APPEND addresses its mailbox by argument and does not touch the selected
// mailbox, so this is safe to run while the sync loop is mid-fetch on the same
// connection.
func (c *Client) AppendToSent(ctx context.Context, raw []byte, sentAt time.Time) error {
	if len(raw) == 0 {
		return nil
	}

	mailbox, err := c.sentMailbox()
	if err != nil {
		return err
	}

	if sentAt.IsZero() {
		sentAt = time.Now()
	}
	cmd := c.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{
		// The sender has, by definition, read what they just sent.
		Flags: []imap.Flag{imap.FlagSeen},
		Time:  sentAt,
	})
	if _, werr := cmd.Write(raw); werr != nil {
		cmd.Close()
		return fmt.Errorf("append to %q: %w", mailbox, werr)
	}
	if cerr := cmd.Close(); cerr != nil {
		return fmt.Errorf("append to %q: %w", mailbox, cerr)
	}
	if _, werr := cmd.Wait(); werr != nil {
		return fmt.Errorf("append to %q: %w", mailbox, werr)
	}
	return nil
}

// sentMailbox resolves the account's Sent folder, preferring the RFC 6154
// \Sent special-use attribute over name matching, since the name is localized
// on plenty of servers ("Enviados", "Gesendet") while the attribute is not.
// The result is cached: it does not change for the life of a connection.
func (c *Client) sentMailbox() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sentMailboxName != "" {
		return c.sentMailboxName, nil
	}

	// RETURN (SPECIAL-USE) is only legal when the server advertises it; without
	// the capability the attributes may still arrive on an ordinary LIST.
	opts := &imap.ListOptions{}
	if c.client.Caps().Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}

	var byAttr, byName string
	list := c.client.List("", "*", opts)
	for f := list.Next(); f != nil; f = list.Next() {
		for _, attr := range f.Attrs {
			if attr == imap.MailboxAttrSent {
				byAttr = f.Mailbox
			}
		}
		if byName != "" {
			continue
		}
		for _, candidate := range ImapSent {
			// Match the leaf too: plenty of servers namespace folders as
			// "INBOX.Sent" or "INBOX/Sent".
			if strings.EqualFold(f.Mailbox, candidate) || strings.EqualFold(leaf(f.Mailbox), candidate) {
				byName = f.Mailbox
			}
		}
	}
	if err := list.Close(); err != nil {
		return "", fmt.Errorf("list mailboxes: %w", err)
	}

	switch {
	case byAttr != "":
		c.sentMailboxName = byAttr
	case byName != "":
		c.sentMailboxName = byName
	default:
		// Creating one would be presumptuous: a server with no Sent folder is
		// usually a relay-only account nobody reads through IMAP.
		return "", ErrNoSentMailbox
	}
	return c.sentMailboxName, nil
}

// leaf returns the last path component of a mailbox name under either of the
// two hierarchy delimiters servers use in practice.
func leaf(name string) string {
	if i := strings.LastIndexAny(name, "./"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}
