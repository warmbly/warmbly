package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"github.com/warmbly/warmbly/internal/client/netbind"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// headerFetchFields are the internet headers fetched alongside each changed
// message (BODY.PEEK[HEADER.FIELDS ...]) and surfaced into Flags as
// "Header:value" pseudo-flags: the warmup verification token plus the
// machine-reply/DSN markers the consumer's reply/bounce classifier reads.
// The IMAP ENVELOPE carries none of these.
var headerFetchFields = append([]string{config.WarmupVerifyHeader}, config.InboundClassificationHeaders...)

type Client struct {
	Email       string
	AuthType    models.AuthType
	Credentials *models.Service
	Oauth2      *models.Oauth2Service

	client *imapclient.Client

	// mu serializes commands that change SELECTed mailbox or mutate state.
	// Warmup actions (MOVE/STORE) run on a different code path than the sync
	// loop and must not interleave with FetchChanges.
	mu sync.Mutex

	// BindIP optionally pins outbound TCP to a specific local source address.
	// When nil, WORKER_BIND_IP is consulted; when still unset, the OS default
	// route is used.
	BindIP *net.TCPAddr
}

func (c *Client) Connect() *errx.MailError {
	var addr string
	switch c.AuthType {
	case models.AuthPlain:
		addr = fmt.Sprintf("%s:%d", c.Credentials.Host, c.Credentials.Port)
	case models.AuthOAuth2:
		addr = fmt.Sprintf("%s:%d", c.Oauth2.Host, c.Oauth2.Port)
	}

	dialer := netbind.TLSDialer(c.BindIP, &tls.Config{})
	conn, err := dialer.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return errx.ErrMailServerUnreachable
	}

	c.client = imapclient.New(conn, nil)

	var xerr *errx.MailError

	switch c.AuthType {
	case models.AuthPlain:
		xerr = c.plainAuth()
	case models.AuthOAuth2:
		xerr = c.oauth2Auth()
	}
	if xerr != nil {
		return xerr
	}

	// CONDSTORE backs the ChangedSince incremental sync. Servers (Gmail,
	// Dovecot, ...) typically advertise it only after authentication, so the
	// check must run post-auth.
	if !c.client.Caps().Has(imap.CapCondStore) {
		return errx.ErrMailCondStoreNotSupported
	}

	return nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) plainAuth() *errx.MailError {
	if err := c.client.Login(c.Credentials.Username, c.Credentials.Password).Wait(); err != nil {
		return c.handleError(err)
	}

	return nil
}

func (c *Client) oauth2Auth() *errx.MailError {
	tk, err := c.Oauth2.Token.Token()
	if err != nil {
		var rErr *oauth2.RetrieveError
		if errors.As(err, &rErr) {
			if rErr.Response.StatusCode >= 500 {
				return errx.ErrMailServerUnreachable
			}
		}
		return errx.ErrMailAuthenticationFailed
	}

	saslc := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
		Username: c.Email,
		Token:    tk.AccessToken,
		Port:     c.Oauth2.Port,
		Host:     c.Oauth2.Host,
	})

	if err := c.client.Authenticate(saslc); err != nil {
		return c.handleError(err)
	}

	return nil
}

func (c *Client) Folders() ([]models.Mailbox, *errx.MailError) {
	var resp []models.Mailbox

	// LIST-STATUS: without requesting these, f.Status is nil for every
	// folder and the sync loop sees an empty account.
	cmd := c.client.List("", "%", &imap.ListOptions{
		ReturnStatus: &imap.StatusOptions{
			UIDValidity:   true,
			HighestModSeq: true,
		},
	})

	for f := cmd.Next(); f != nil; f = cmd.Next() {
		if len(resp) >= config.MaxEmailFolders {
			return nil, errx.ErrMailFoldersMax
		}

		var attrs []string = make([]string, len(f.Attrs))

		for i := range f.Attrs {
			attrs[i] = string(f.Attrs[i])
		}

		if f.Status == nil {
			continue
		}

		resp = append(resp, models.Mailbox{
			Name:          f.Mailbox,
			Attrs:         attrs,
			UIDValidity:   f.Status.UIDValidity,
			HighestModSeq: f.Status.HighestModSeq,
		})
	}

	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}

	return resp, nil
}

func (c *Client) Mailbox(mailbox string, uidvali, opts *imap.SelectOptions) error {
	if _, err := c.client.Select(mailbox, opts).Wait(); err != nil {
		return err
	}

	return nil
}

// SelectForSync opens a mailbox read-only with CONDSTORE enabled and returns
// its message count. FETCH is only valid against a selected mailbox, so the
// sync loop must call this before FetchChanges; CONDSTORE on the SELECT is
// what arms ChangedSince. The count lets the caller skip the fetch entirely
// for an empty mailbox, where a 1:* set is a server error.
func (c *Client) SelectForSync(mailbox string) (uint32, *errx.MailError) {
	data, err := c.client.Select(mailbox, &imap.SelectOptions{ReadOnly: true, CondStore: true}).Wait()
	if err != nil {
		return 0, c.handleError(err)
	}
	return data.NumMessages, nil
}

// Fetched is one message's envelope as read by FetchEnvelopes, plus what
// FetchBody needs to read its text parts later. Bodies are deliberately a
// second step: the sync loop decides per message whether it is new and
// admitted before paying for the body.
type Fetched struct {
	Email *models.EmailMessageData
	uid   imap.UID
	body  imap.BodyStructure
}

// SearchSince returns the UIDs in the selected mailbox whose internal date is
// on or after since (IMAP SINCE has day granularity), ascending. It drives
// the backfill: the caller walks the set newest first under its cap.
func (c *Client) SearchSince(since time.Time) ([]imap.UID, *errx.MailError) {
	return c.uidSearch(&imap.SearchCriteria{Since: since})
}

// SearchChangedSince returns the UIDs whose mod-sequence is above modSeq: the
// CONDSTORE incremental set. Asking the server for the set first, instead of
// FETCHing every sequence window with CHANGEDSINCE, keeps a quiet 50,000
// message folder to one round trip per tick.
func (c *Client) SearchChangedSince(modSeq uint64) ([]imap.UID, *errx.MailError) {
	return c.uidSearch(&imap.SearchCriteria{ModSeq: &imap.SearchCriteriaModSeq{ModSeq: modSeq + 1}})
}

func (c *Client) uidSearch(criteria *imap.SearchCriteria) ([]imap.UID, *errx.MailError) {
	data, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, c.handleError(err)
	}
	return data.AllUIDs(), nil
}

// FetchEnvelopes reads envelope, flags, structure and the classification
// headers for the given UIDs (at most ImapFetchBatchSize per call is the
// caller's job) without bodies.
func (c *Client) FetchEnvelopes(ctx context.Context, uids []imap.UID) ([]*Fetched, *errx.MailError) {
	if len(uids) == 0 {
		return nil, nil
	}
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(uid)
	}
	cmd := c.client.Fetch(set, &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		BodyStructure: &imap.FetchItemBodyStructure{
			Extended: true,
		},
		Flags:        true,
		ModSeq:       true,
		InternalDate: true,
		RFC822Size:   true,
		BodySection: []*imap.FetchItemBodySection{{
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: headerFetchFields,
			Peek:         true,
		}},
	})
	var collected []*Fetched

	for em := cmd.Next(); em != nil; em = cmd.Next() {
		email := &models.EmailMessageData{}
		var euid imap.UID

		var bodyStructure imap.BodyStructure
		// Collected separately: the FLAGS item resets email.Flags and item
		// order is server-dependent, so appending inline could be wiped.
		var headerFlags []string

		for item := em.Next(); item != nil; item = em.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				email.UID = uint32(item.UID)
				euid = item.UID
			case imapclient.FetchItemDataFlags:
				email.Flags = make([]string, 0)
				for _, f := range item.Flags {
					email.Flags = append(email.Flags, string(f))
				}
			case imapclient.FetchItemDataEnvelope:
				email.BCC = GetAddressNames(item.Envelope.Bcc)
				email.CC = GetAddressNames(item.Envelope.Cc)
				email.Date = item.Envelope.Date
				email.From = GetAddressNames(item.Envelope.From)
				email.InReplyTo = item.Envelope.InReplyTo
				email.MessageID = item.Envelope.MessageID
				email.ReplyTo = GetAddressNames(item.Envelope.ReplyTo)
				email.Sender = GetAddressNames(item.Envelope.Sender)
				email.Subject = item.Envelope.Subject
				email.To = GetAddressNames(item.Envelope.To)
			case imapclient.FetchItemDataRFC822Size:
				email.Size = item.Size
			case imapclient.FetchItemDataInternalDate:
				email.InternalDate = item.Time
			case imapclient.FetchItemDataModSeq:
				email.ModSeq = item.ModSeq
			case imapclient.FetchItemDataBodyStructure:
				bodyStructure = item.BodyStructure
			case imapclient.FetchItemDataBodySection:
				headerFlags = parseHeaderFlags(item.Literal)
			}
		}

		email.Flags = append(email.Flags, headerFlags...)
		collected = append(collected, &Fetched{Email: email, uid: euid, body: bodyStructure})
		if ctx.Err() != nil {
			break
		}
	}

	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}
	return collected, nil
}

// FetchBody reads the text parts of one fetched message. It must run after
// the FetchEnvelopes command that produced it is closed: a nested FETCH on
// the same connection blocks until the outer one finishes, and the outer one
// cannot finish while we wait, which deadlocks the sync on the first message.
func (c *Client) FetchBody(f *Fetched) {
	if f == nil || f.Email == nil {
		return
	}
	f.Email.BodyPlain, f.Email.BodyHTML = fetchTextParts(c.client, f.uid, f.body)
}

// parseHeaderFlags reads a HEADER.FIELDS literal and renders the fetched
// headers as "Header:value" pseudo-flags, using the canonical names from
// headerFetchFields so the consumer's prefix matching always hits.
func parseHeaderFlags(lit io.Reader) []string {
	if lit == nil {
		return nil
	}
	tp := textproto.NewReader(bufio.NewReader(io.LimitReader(lit, 32*1024)))
	hdr, err := tp.ReadMIMEHeader()
	if len(hdr) == 0 && err != nil {
		return nil
	}
	var out []string
	for _, name := range headerFetchFields {
		if v := strings.TrimSpace(hdr.Get(name)); v != "" {
			out = append(out, name+":"+v)
		}
	}
	return out
}
