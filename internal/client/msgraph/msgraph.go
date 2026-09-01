// Package msgraph is a thin Microsoft Graph mail client for Warmbly's
// Outlook/Microsoft 365 mailboxes. It mirrors the shape of internal/client/goog
// (the Gmail API client): a per-mailbox Client with OnMessage* callbacks, an
// OAuth2 token source that persists refreshes, RAW MIME sending, delta-based
// inbound sync, and warmup mailbox actions. It deliberately hand-rolls a small
// REST surface (~a dozen endpoints) instead of pulling in the very large
// official SDK, which would bloat the disposable worker binary and slow CI.
package msgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/pkg/mailauth"
	"github.com/warmbly/warmbly/internal/pkg/stoken"
	"golang.org/x/oauth2"
)

const (
	// graphBase is the Microsoft Graph v1.0 root. All mail calls are made
	// against the signed-in user (/me) using the delegated token.
	graphBase = "https://graph.microsoft.com/v1.0"

	// Well-known mail folder ids Graph accepts directly in a path or as a
	// move destinationId, so we never have to resolve them to opaque ids.
	FolderInbox   = "inbox"
	FolderJunk    = "junkemail"
	FolderSent    = "sentitems"
	FolderArchive = "archive"
	FolderDrafts  = "drafts"
)

// Client is a single Microsoft 365 mailbox reached over Graph.
type Client struct {
	Email     string
	FirstName string
	LastName  string

	hc    *http.Client
	Cache *cache.Cache

	// DeltaLinks holds the opaque per-folder delta cursor (well-known folder
	// name -> deltaLink URL). Seeded from persisted state on init and advanced
	// as sync runs; OnDelta persists each new value off the disposable worker.
	DeltaLinks map[string]string

	// folderIDs caches resolved folder ids (e.g. the created "Warmbly" folder)
	// so we don't re-list on every warmup action.
	folderIDs map[string]string
	mu        sync.Mutex

	// OnMessageSeen is offered every live delta item with only its id and
	// read state: the caller dedupes, hydrates (FetchMessage) and stores as
	// budget allows, and returns false for a message it left on the server,
	// which pins the folder cursor before that page.
	OnMessageSeen   func(ctx context.Context, folder, providerID string, seen bool) (stored bool, err error)
	OnMessageRemove func(ctx context.Context, providerID string) error
	OnDelta         func(ctx context.Context, folder, deltaLink string) error
	OnTokenRefresh  func(ctx context.Context, token *oauth2.Token) error
}

// Init builds the auto-refreshing OAuth2 HTTP client. It mirrors goog.Client.Init:
// the token source reuses the current token until expiry, then refreshes through
// cfg and persists the new token via OnTokenRefresh (the worker relays it back to
// the control plane, which owns the encrypted credential store).
func (c *Client) Init(ctx context.Context, token *oauth2.Token, cfg oauth2.Config) *errx.MailError {
	ts := cfg.TokenSource(ctx, token)
	ts = oauth2.ReuseTokenSource(token, ts)
	// Same guard as goog.Init: a nil OnTokenRefresh would panic per request.
	if c.OnTokenRefresh != nil {
		ts = stoken.New(ts, func(t *oauth2.Token) error {
			return c.OnTokenRefresh(context.Background(), t)
		})
	}
	return c.InitWithSource(ctx, ts)
}

// InitWithSource builds the client on a caller-owned token source (brokered
// tokens from Warmbly Cloud); nothing is persisted from it.
func (c *Client) InitWithSource(ctx context.Context, ts oauth2.TokenSource) *errx.MailError {
	c.hc = oauth2.NewClient(ctx, ts)
	if c.DeltaLinks == nil {
		c.DeltaLinks = map[string]string{}
	}
	c.folderIDs = map[string]string{}
	return nil
}

// do issues a single authenticated request. body may be nil.
func (c *Client) do(ctx context.Context, method, url, contentType string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.hc.Do(req)
}

// doJSON marshals in (if non-nil), issues the request, maps any non-2xx status to
// a MailError, and decodes the response into out (if non-nil).
func (c *Client) doJSON(ctx context.Context, method, url string, in, out any) error {
	var body []byte
	contentType := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
		contentType = "application/json"
	}

	resp, err := c.do(ctx, method, url, contentType, body)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HandleError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// transportError classifies a failure of the HTTP call itself. The client's
// token source runs inside it, so a grant the provider has revoked arrives
// here rather than as a status code, and reporting that as "the mail server
// could not be established" points the operator at the network and promises a
// retry that can never succeed.
func transportError(err error) *errx.MailError {
	f := mailauth.ClassifyTokenError(err)
	if f.Refused() {
		log.Debug().
			Str("oauth_error", f.ErrorCode).
			Str("oauth_description", f.Description).
			Int("status", f.Status).
			Bool("revoked", f.Revoked()).
			Msg("Graph token refresh refused")
	}
	if f.Revoked() {
		return errx.ErrMailAuthenticationFailed
	}
	return errx.ErrMailServerUnreachable
}
