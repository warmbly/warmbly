package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
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

	// sentMailboxName caches the resolved Sent folder for this connection.
	// Guarded by mu.
	sentMailboxName string

	// selected records whether a mailbox is currently SELECTed, so
	// ReleaseMailbox does not send UNSELECT in authenticated state, where a
	// strict server answers BAD. Atomic because the sync path selects without
	// holding mu while warmup actions select under it.
	selected atomic.Bool

	// nsPrefix caches this connection's personal-namespace prefix (nil until
	// resolved, "" when the server puts user folders at the root). Guarded by mu.
	nsPrefix *string

	// BindIP optionally pins outbound TCP to a specific local source address.
	// When nil, WORKER_BIND_IP is consulted; when still unset, the OS default
	// route is used.
	BindIP *net.TCPAddr

	// lifecycle guards the client field itself. A reconnect holds the write
	// lock through dial, auth and assignment; every command holds the read
	// lock for its duration, so a reconnect never swaps the session out from
	// under a command, and two paths that both see the drop dial once.
	// Lock order: mu before lifecycle, and never nest a read lock.
	lifecycle sync.RWMutex

	// conn is the transport under client, nil until the first Connect. It
	// is swapped under the lifecycle write lock and read under the read lock
	// like client itself.
	conn *idleConn

	// IdleTimeout bounds how long a command waits for the server to say
	// anything before the session is declared dead. Zero means
	// config.ImapCommandIdleTimeout.
	IdleTimeout time.Duration

	// plaintext dials without TLS at all. Only the tests set it, to talk to
	// the in-process server; no product path reaches it, because an IMAP
	// session in the clear would put the mailbox password on the wire.
	plaintext bool

	// condStore is whether the current session advertised CONDSTORE after
	// authentication, which decides between the mod-sequence and the UIDNEXT
	// incremental sync.
	condStore atomic.Bool

	// folderOverflow is what the last Folders call had to leave out for the
	// cap, folderConflicts what it left out for a duplicate UIDVALIDITY.
	folderOverflow  atomic.Int32
	folderConflicts atomic.Int32
}

// begin starts the idle clock for one command; call the result on exit.
// Callers hold the lifecycle read lock, so conn cannot change underneath.
func (c *Client) begin() func() {
	if c.conn == nil {
		return func() {}
	}
	return c.conn.arm()
}

// HasCondStore reports whether the session supports CONDSTORE, the
// mod-sequence path of the incremental sync. Without it the sync loop keys
// on UIDNEXT and mirrors flags with a periodic scan.
func (c *Client) HasCondStore() bool {
	return c.condStore.Load()
}

// ensureConnected re-dials after the server has dropped the session. go-imap
// parks a dead client in the Logout state and fails every later command with
// net.ErrClosed; nothing re-dialed, so one drop (Gmail closes sessions after a
// while) left the mailbox a zombie until the worker restarted: no sync, no
// sent copies. Every entry point that starts a command runs through here,
// before taking its own read lock.
func (c *Client) ensureConnected() *errx.MailError {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	// Only a session that got past auth is worth keeping: a failed Login
	// leaves go-imap in NotAuthenticated, which is just as unusable as Logout.
	if c.client != nil {
		switch c.client.State() {
		case imap.ConnStateAuthenticated, imap.ConnStateSelected:
			return nil
		}
	}
	return c.connectLocked()
}

func (c *Client) Connect() *errx.MailError {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	return c.connectLocked()
}

// connectLocked dials and authenticates a fresh session. lifecycle must be
// held for writing.
func (c *Client) connectLocked() *errx.MailError {
	var addr, host, security string
	var port int
	switch c.AuthType {
	case models.AuthPlain:
		host, port, security = c.Credentials.Host, c.Credentials.Port, c.Credentials.Security
	case models.AuthOAuth2:
		host, port = c.Oauth2.Host, c.Oauth2.Port
	}
	// Normalized before it is used anywhere: brackets belong to the address,
	// not to the host, and JoinHostPort is what puts them back for an IPv6
	// literal.
	host = models.NormalizeMailHost(host)
	addr = models.MailDialAddress(host, port)

	tlsConf := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: netbind.InsecureTLS(), //nolint:gosec // MAIL_TLS_INSECURE, local dev only
	}

	// Dial through netbind so both paths honour WORKER_BIND_IP, and wrap the
	// socket before TLS so the idle clock sits under the encryption.
	timeout := c.IdleTimeout
	if timeout <= 0 {
		timeout = config.ImapCommandIdleTimeout
	}
	// The unencrypted mode is checked before the dial and again against the
	// peer we actually got, because only the second one is a fact about this
	// socket rather than about what DNS said a moment ago.
	resolved := models.ResolveIMAPSecurity(security, port)
	if resolved == models.MailSecurityNone && !models.CleartextMailAllowed(host) {
		return errx.ErrMailInsecureRemoteHost
	}
	raw, err := netbind.Dialer(c.BindIP).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return errx.ErrMailServerUnreachable
	}
	if resolved == models.MailSecurityNone && !netbind.LoopbackPeer(raw) {
		_ = raw.Close()
		return errx.ErrMailInsecureRemoteHost
	}
	conn := &idleConn{Conn: raw, timeout: timeout}

	var client *imapclient.Client
	switch {
	case c.plaintext, resolved == models.MailSecurityNone:
		client = imapclient.New(conn, nil)
	case resolved == models.MailSecurityStartTLS:
		// Plaintext greeting, upgraded in-band. NewStartTLS closes conn
		// itself when the upgrade fails.
		client, err = imapclient.NewStartTLS(conn, &imapclient.Options{TLSConfig: tlsConf})
		if err != nil {
			return errx.ErrMailServerUnreachable
		}
	default:
		tconn := tls.Client(conn, tlsConf)
		hctx, cancel := context.WithTimeout(context.Background(), timeout)
		err = tconn.HandshakeContext(hctx)
		cancel()
		if err != nil {
			_ = tconn.Close()
			return errx.ErrMailServerUnreachable
		}
		client = imapclient.New(tconn, nil)
	}

	c.client = client
	c.conn = conn
	c.selected.Store(false)
	c.condStore.Store(false)
	done := conn.arm()
	defer done()

	var xerr *errx.MailError

	switch c.AuthType {
	case models.AuthPlain:
		xerr = c.plainAuth()
	case models.AuthOAuth2:
		xerr = c.oauth2Auth()
	}
	if xerr != nil {
		// Drop the half-open session so the next ensureConnected re-dials
		// instead of reusing an unauthenticated client.
		_ = client.Close()
		return xerr
	}

	// CONDSTORE backs the mod-sequence incremental sync. Servers (Gmail,
	// Dovecot, ...) typically advertise it only after authentication, so the
	// check must run post-auth. Without it (Outlook.com, Microsoft 365 over
	// IMAP, Yahoo, many hosted servers) the sync loop keys on UIDNEXT instead.
	c.condStore.Store(c.client.Caps().Has(imap.CapCondStore))

	return nil
}

func (c *Client) Close() error {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	if c.client == nil {
		return nil
	}
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

func (c *Client) Mailbox(mailbox string, uidvali, opts *imap.SelectOptions) error {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
	if _, err := c.selectMailbox(mailbox, opts); err != nil {
		return err
	}

	return nil
}

// selectMailbox is the single SELECT funnel: every path that changes the
// selected mailbox goes through it so ReleaseMailbox knows whether there is
// one to release. A failed SELECT leaves the session with no mailbox
// selected (RFC 3501 6.3.1). The caller holds the lifecycle read lock.
func (c *Client) selectMailbox(mailbox string, opts *imap.SelectOptions) (*imap.SelectData, error) {
	data, err := c.client.Select(mailbox, opts).Wait()
	c.selected.Store(err == nil)
	return data, err
}

// SelectForSync opens a mailbox read-only with CONDSTORE enabled and returns
// its message count. FETCH is only valid against a selected mailbox, so the
// sync loop must call this before FetchChanges; CONDSTORE on the SELECT is
// what arms ChangedSince. The count lets the caller skip the fetch entirely
// for an empty mailbox, where a 1:* set is a server error.
func (c *Client) SelectForSync(mailbox string) (uint32, *errx.MailError) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
	// (CONDSTORE) on a server without it is a BAD.
	data, err := c.selectMailbox(mailbox, &imap.SelectOptions{ReadOnly: true, CondStore: c.condStore.Load()})
	if err != nil {
		return 0, c.handleError(err)
	}
	return data.NumMessages, nil
}

// ReleaseMailbox drops the selected mailbox. Dovecot answers LIST-STATUS for
// the selected mailbox with the values it held at SELECT, so a loop that keeps
// INBOX selected never sees another change land. Servers without UNSELECT keep
// the previous behaviour.
//
// It takes mu because it changes selected state, which is exactly what mu
// exists to serialize against an in-flight warmup MOVE/STORE.
func (c *Client) ReleaseMailbox() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()

	if c.client == nil || !c.selected.Load() || !c.client.Caps().Has(imap.CapUnselect) {
		return
	}
	defer c.begin()()
	if err := c.client.Unselect().Wait(); err == nil {
		c.selected.Store(false)
	}
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

// SearchAll returns every UID in the selected mailbox, ascending. An expunge
// leaves no UID behind to report, so the drafts reconciliation diffs this set
// against the UIDs the platform holds for the folder.
func (c *Client) SearchAll() ([]imap.UID, *errx.MailError) {
	return c.uidSearch(&imap.SearchCriteria{})
}

// SearchNewSince returns the UIDs at or above uidNext: the mail that arrived
// since the folder's UIDNEXT was last recorded. It is the incremental set on
// a server without CONDSTORE. A "n:*" set with n past the end answers with
// the highest UID in the folder (RFC 3501 6.4.8), so the result is filtered.
func (c *Client) SearchNewSince(uidNext uint32) ([]imap.UID, *errx.MailError) {
	if uidNext == 0 {
		uidNext = 1
	}
	var set imap.UIDSet
	set.AddRange(imap.UID(uidNext), 0)
	uids, err := c.uidSearch(&imap.SearchCriteria{UID: []imap.UIDSet{set}})
	if err != nil {
		return nil, err
	}
	out := uids[:0]
	for _, uid := range uids {
		if uint32(uid) >= uidNext {
			out = append(out, uid)
		}
	}
	return out, nil
}

// FlagState is one message as the flag scan sees it: enough to find the
// platform's copy (the RFC Message-ID, which is the map key) and to compare
// its flags with the previous scan. Bodies and envelopes are not read.
type FlagState struct {
	MessageID string
	Flags     []string
}

// FetchFlags reads the flags and Message-ID of every message at or above
// uidFrom in the selected mailbox, in one round trip with no bodies. It is
// how flag and read-state changes are found on a server without CONDSTORE:
// the caller diffs it against the previous scan.
func (c *Client) FetchFlags(ctx context.Context, uidFrom uint32) (map[uint32]FlagState, *errx.MailError) {
	if uidFrom == 0 {
		uidFrom = 1
	}
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
	var set imap.UIDSet
	set.AddRange(imap.UID(uidFrom), 0)
	cmd := c.client.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, Envelope: true})
	out := map[uint32]FlagState{}
	for em := cmd.Next(); em != nil; em = cmd.Next() {
		var uid uint32
		var st FlagState
		for item := em.Next(); item != nil; item = em.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				uid = uint32(item.UID)
			case imapclient.FetchItemDataFlags:
				st.Flags = make([]string, 0, len(item.Flags))
				for _, f := range item.Flags {
					st.Flags = append(st.Flags, string(f))
				}
			case imapclient.FetchItemDataEnvelope:
				if item.Envelope != nil {
					st.MessageID = item.Envelope.MessageID
				}
			}
		}
		if uid >= uidFrom {
			out[uid] = st
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, c.handleError(err)
	}
	return out, nil
}

func (c *Client) uidSearch(criteria *imap.SearchCriteria) ([]imap.UID, *errx.MailError) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
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
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
	cmd := c.client.Fetch(set, &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		BodyStructure: &imap.FetchItemBodyStructure{
			Extended: true,
		},
		Flags: true,
		// MODSEQ is a CONDSTORE fetch item (RFC 7162). Asking a server that
		// never advertised it for one is a malformed fetch-att, and a strict
		// parser answers BAD and syncs nothing: issue #405, where IONOS said
		// `BAD expected fetch-att instead of "MODSEQ BODY.P"` on every pass.
		ModSeq:       c.condStore.Load(),
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
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	defer c.begin()()
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
