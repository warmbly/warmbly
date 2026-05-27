package imap

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
)

const WarmupFolderName = "Warmbly"

// MarkAsRead sets the \Seen flag on the given UID in mailboxName.
func (c *Client) MarkAsRead(ctx context.Context, mailboxName string, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.client.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %q: %w", mailboxName, err)
	}

	cmd := c.client.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}, nil)
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("store \\Seen: %w", err)
	}
	return nil
}

// MarkImportant sets the \Flagged flag on the given UID in mailboxName.
// Many IMAP UIs surface \Flagged as a star or important indicator.
func (c *Client) MarkImportant(ctx context.Context, mailboxName string, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.client.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %q: %w", mailboxName, err)
	}

	cmd := c.client.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagFlagged},
	}, nil)
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("store \\Flagged: %w", err)
	}
	return nil
}

// RemoveFromSpam moves the UID from sourceMailbox into inboxName.
// inboxName is usually "INBOX" but is provided so callers can supply
// the value resolved from the worker's mailbox list.
func (c *Client) RemoveFromSpam(ctx context.Context, sourceMailbox, inboxName string, uid uint32) error {
	if !IsSpamMailboxName(sourceMailbox) {
		return nil
	}
	return c.moveUID(ctx, sourceMailbox, inboxName, uid)
}

// MoveToFolder moves the UID from sourceMailbox into dstFolder, creating
// dstFolder if it does not exist. Use for the "Warmbly" sorting label.
func (c *Client) MoveToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureMailboxExists(dstFolder); err != nil {
		return err
	}

	return c.moveUIDLocked(sourceMailbox, dstFolder, uid)
}

func (c *Client) moveUID(ctx context.Context, src, dst string, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.moveUIDLocked(src, dst, uid)
}

func (c *Client) moveUIDLocked(src, dst string, uid uint32) error {
	if _, err := c.client.Select(src, nil).Wait(); err != nil {
		return fmt.Errorf("select %q: %w", src, err)
	}

	set := imap.UIDSetNum(imap.UID(uid))
	if c.client.Caps().Has(imap.CapMove) {
		if _, err := c.client.Move(set, dst).Wait(); err != nil {
			return fmt.Errorf("move uid %d %q→%q: %w", uid, src, dst, err)
		}
		return nil
	}

	// MOVE not supported — emulate via COPY + STORE \Deleted + EXPUNGE.
	if _, err := c.client.Copy(set, dst).Wait(); err != nil {
		return fmt.Errorf("copy uid %d %q→%q: %w", uid, src, dst, err)
	}
	storeCmd := c.client.Store(set, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("store \\Deleted on uid %d: %w", uid, err)
	}
	if err := c.client.Expunge().Close(); err != nil {
		return fmt.Errorf("expunge %q: %w", src, err)
	}
	return nil
}

func (c *Client) ensureMailboxExists(name string) error {
	list := c.client.List("", name, nil)
	found := false
	for f := list.Next(); f != nil; f = list.Next() {
		if f.Mailbox == name {
			found = true
		}
	}
	if err := list.Close(); err != nil {
		return fmt.Errorf("list mailbox %q: %w", name, err)
	}
	if found {
		return nil
	}
	if err := c.client.Create(name, nil).Wait(); err != nil {
		return fmt.Errorf("create mailbox %q: %w", name, err)
	}
	return nil
}

// IsSpamMailboxName returns true if the mailbox name looks like Junk/Spam.
// Used as a guard so we never accidentally MOVE a non-spam message.
func IsSpamMailboxName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, candidate := range ImapSpam {
		if strings.EqualFold(name, candidate) {
			return true
		}
		if strings.Contains(lower, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

// IsSpamMailbox returns true if the mailbox's attributes or name identify it
// as a Junk/Spam folder under RFC 6154 SPECIAL-USE or by name match.
func IsSpamMailbox(name string, attrs []string) bool {
	for _, a := range attrs {
		if strings.EqualFold(a, string(imap.MailboxAttrJunk)) {
			return true
		}
	}
	return IsSpamMailboxName(name)
}

// IsInboxMailbox returns true for the canonical INBOX (case-insensitive) or
// any mailbox flagged with the \Inbox special-use attribute.
func IsInboxMailbox(name string, attrs []string) bool {
	if strings.EqualFold(strings.TrimSpace(name), "INBOX") {
		return true
	}
	for _, a := range attrs {
		if strings.EqualFold(a, "\\Inbox") {
			return true
		}
	}
	return false
}
