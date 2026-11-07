package imap

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/errx"
)

// ErrIdleUnsupported is a server that cannot hold a folder open with IDLE.
var ErrIdleUnsupported = errx.MError(errx.MailErrorWarning, errx.MailErrorCodeUnsupported, "the mail server does not support IDLE", errx.MailErrorResolveMethodNone)

// idleRestart re-issues IDLE well inside the 29 minutes RFC 2177 lets a server wait.
const idleRestart = 20 * time.Minute

// options keeps a sync session's options exactly as before and adds the change handler to a watch session.
func (c *Client) options(tlsConf *tls.Config) *imapclient.Options {
	if c.OnChange == nil {
		if tlsConf == nil {
			return nil
		}
		return &imapclient.Options{TLSConfig: tlsConf}
	}
	notify := c.OnChange
	return &imapclient.Options{
		TLSConfig: tlsConf,
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Expunge: func(uint32) { notify() },
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					notify()
				}
			},
			Fetch: func(msg *imapclient.FetchMessageData) {
				_, _ = msg.Collect()
				notify()
			},
		},
	}
}

// WatchInbox holds INBOX open read-only in IDLE, so OnChange fires within seconds of
// new, removed or re-flagged mail. It returns nil when ctx ends and an error when the
// session drops or the server cannot IDLE. ready runs each time IDLE is entered.
// The session must be dedicated to watching.
func (c *Client) WatchInbox(ctx context.Context, ready func()) *errx.MailError {
	if err := c.ensureConnected(); err != nil {
		return err
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	client := c.client
	caps := client.Caps()
	if !caps.Has(imap.CapIdle) && !caps.Has(imap.CapIMAP4rev2) {
		return ErrIdleUnsupported
	}
	done := c.begin()
	_, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	done()
	if err != nil {
		return c.handleError(err)
	}
	for {
		done = c.begin()
		idle, err := client.Idle()
		done()
		if err != nil {
			return c.handleError(err)
		}
		ready()
		ended := make(chan error, 1)
		go func() { ended <- idle.Wait() }()
		timer := time.NewTimer(idleRestart)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = idle.Close()
			return nil
		case <-ended:
			timer.Stop()
			return errx.ErrMailServerUnreachable
		case <-timer.C:
		}
		done = c.begin()
		closeErr := idle.Close()
		waitErr := <-ended
		done()
		if closeErr != nil {
			return c.handleError(closeErr)
		}
		if waitErr != nil {
			return c.handleError(waitErr)
		}
	}
}
