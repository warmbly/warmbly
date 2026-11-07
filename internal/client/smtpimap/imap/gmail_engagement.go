package imap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GmailHost and GmailPort are Gmail's IMAP endpoint; it only accepts implicit TLS.
const (
	GmailHost = "imap.gmail.com"
	GmailPort = 993
)

// AllFolders lists every selectable folder with its attributes, Gmail's label views included and no STATUS asked.
func (c *Client) AllFolders(ctx context.Context) ([]models.Mailbox, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()

	opts, _ := listOptionsFor(c.client.Caps(), nil)
	var out []models.Mailbox
	cmd := c.client.List("", "*", opts)
	for f := cmd.Next(); f != nil; f = cmd.Next() {
		attrs := make([]string, len(f.Attrs))
		for i := range f.Attrs {
			attrs[i] = string(f.Attrs[i])
		}
		if selectableFolder(attrs) {
			out = append(out, models.Mailbox{Name: f.Mailbox, Attrs: attrs, Delim: delimString(f.Delim)})
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}
	return out, nil
}

// CopyToFolder copies the UID into dstFolder; on Gmail that adds the folder's label and leaves every other one.
func (c *Client) CopyToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if merr := c.ensureConnected(); merr != nil {
		return merr
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
	src, dst := c.qualifyMailboxLocked(sourceMailbox), c.qualifyMailboxLocked(dstFolder)
	if _, err := c.selectMailbox(src, nil); err != nil {
		return fmt.Errorf("select %q: %w", src, err)
	}
	if _, err := c.client.Copy(imap.UIDSetNum(imap.UID(uid)), dst).Wait(); err != nil {
		return fmt.Errorf("copy uid %d %q→%q: %w", uid, src, dst, err)
	}
	return nil
}

// FindSpecialUse returns the first folder carrying the RFC 6154 attribute, or nil.
func FindSpecialUse(boxes []models.Mailbox, attr imap.MailboxAttr) *models.Mailbox {
	for i := range boxes {
		for _, a := range boxes[i].Attrs {
			if strings.EqualFold(a, string(attr)) {
				return &boxes[i]
			}
		}
	}
	return nil
}

// Refused reports a tagged NO or BAD: the server is up and declined this one command.
func Refused(err error) bool {
	var imapErr *imap.Error
	return errors.As(err, &imapErr)
}

// Gmail's [ALERT] texts for an account whose IMAP access is switched off.
var alertIMAPDisabled = []string{
	"imap access is disabled",
	"not enabled for imap",
}

// IMAPDisabled reports a sign-in refused because IMAP is switched off for the account or its domain.
func IMAPDisabled(err error) bool {
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		return false
	}
	if mailErr.Code == errx.MailErrorCodeAuthorizationFailed {
		return true
	}
	lower := strings.ToLower(mailErr.Message)
	for _, phrase := range alertIMAPDisabled {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
