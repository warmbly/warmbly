package graphkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is GraphKit's production API.
const DefaultBaseURL = "https://api.graphkit.com/v1/infrastructure"

// FakeURL selects the in-process simulator instead of the network.
const FakeURL = "fake"

// API is every call the partner contract offers. Writes take the
// Idempotency-Key GraphKit requires; a replay with the same key and body
// returns the original answer.
type API interface {
	UpsertAccount(ctx context.Context, externalID string, in AccountInput, key string) (*Account, error)
	GetAccount(ctx context.Context, externalID string) (*Account, error)
	ListAccounts(ctx context.Context, cursor string, limit int) (*Page[Account], error)

	Pricing(ctx context.Context) (*Pricing, error)
	Search(ctx context.Context, query string, tlds []string, limit int, exactOnly bool) (*SearchResult, error)
	Check(ctx context.Context, domains []string, key string) ([]DomainOffer, error)

	CreateQuote(ctx context.Context, externalID string, items []QuoteItem, key string) (*Quote, error)
	GetQuote(ctx context.Context, externalID, quoteID string) (*Quote, error)
	CreateOrder(ctx context.Context, externalID, quoteID, forwardingURL, key string) (*Order, error)
	GetOrder(ctx context.Context, externalID, orderID string) (*Order, error)
	ListOrders(ctx context.Context, externalID, cursor string, limit int) (*Page[Order], error)

	ListDomains(ctx context.Context, externalID, cursor string, limit int) (*Page[Domain], error)
	GetDomain(ctx context.Context, domainID string) (*Domain, error)
	UpdateDomain(ctx context.Context, domainID string, patch DomainPatch, key string) (*Domain, error)
	RenewDomain(ctx context.Context, domainID string, years int, expectedExpiresAt time.Time, key string) (*Domain, error)
	TransferOut(ctx context.Context, domainID, key string) (*TransferOut, error)
	ListDNS(ctx context.Context, domainID string) ([]DNSRecord, error)
	AddDNS(ctx context.Context, domainID string, rec DNSRecordInput, key string) (*DNSRecord, error)
	UpdateDNS(ctx context.Context, domainID, recordID string, patch DNSRecordPatch, key string) (*DNSRecord, error)
	DeleteDNS(ctx context.Context, domainID, recordID, key string) error
	CheckDNS(ctx context.Context, domainID, key string) (*DNSCheck, error)
	RepairDNS(ctx context.Context, domainID, key string) (*DNSCheck, error)

	// ConnectDomain adds a domain registered elsewhere; method is nameservers, cloudflare or manual.
	ConnectDomain(ctx context.Context, externalID, domain, method, key string) (*Domain, error)
	CheckConnection(ctx context.Context, domainID, key string) (*Domain, error)
	RegenerateConnection(ctx context.Context, domainID, key string) (*Domain, error)
	// RemoveDomain removes an existing domain; a registered one is refused.
	RemoveDomain(ctx context.Context, domainID, key string) error

	ListMailboxes(ctx context.Context, externalID, domainID, cursor string, limit int) (*Page[Mailbox], error)
	GetMailbox(ctx context.Context, mailboxID string) (*Mailbox, error)
	UpdateMailbox(ctx context.Context, mailboxID string, patch MailboxPatch, key string) (*Mailbox, error)
	SetMailboxPhoto(ctx context.Context, mailboxID, filename string, data []byte, key string) (*Mailbox, error)
	MailboxPhoto(ctx context.Context, mailboxID string) ([]byte, string, error)
	CancelMailbox(ctx context.Context, mailboxID, key string) (*Mailbox, error)
	ResumeMailbox(ctx context.Context, mailboxID, key string) (*Mailbox, error)
	MailboxCredentials(ctx context.Context, mailboxID string) (*Credentials, error)
	// MailboxAccessToken brokers a Google token for one token-method mailbox; key must be fresh per call.
	MailboxAccessToken(ctx context.Context, mailboxID string, scopes []string, key string) (*AccessToken, error)
	// PutMailboxSignInLink registers the single-use link the mail vendor opens to sign an oauth mailbox in.
	PutMailboxSignInLink(ctx context.Context, mailboxID, url string, expiresAt time.Time, key string) (*Mailbox, error)
	// MailboxSignedIn reports an oauth mailbox connected through its sign-in; it becomes ready.
	MailboxSignedIn(ctx context.Context, mailboxID, key string) (*Mailbox, error)

	PutDelegation(ctx context.Context, d Delegation, key string) error
	GetDelegation(ctx context.Context) (*Delegation, error)
	// PutAccountDelegation gives one account its own identities; a provider left out uses the partner's.
	PutAccountDelegation(ctx context.Context, externalID string, d Delegation, key string) (*Delegation, error)
	GetAccountDelegation(ctx context.Context, externalID string) (*Delegation, error)
	DeleteAccountDelegation(ctx context.Context, externalID, key string) error
	GetPartnerSettings(ctx context.Context) (*PartnerSettings, error)
	UpdatePartnerSettings(ctx context.Context, in PartnerSettings, key string) (*PartnerSettings, error)
	PutWebhook(ctx context.Context, endpoint string, events []string, key string) (*WebhookEndpoint, error)
	GetWebhook(ctx context.Context) (*WebhookEndpoint, error)
	RotateWebhook(ctx context.Context, key string) (*WebhookEndpoint, error)
	// DeleteWebhook removes the endpoint; the secret is kept for a later PUT. not_found when none is registered.
	DeleteWebhook(ctx context.Context, key string) error
	Events(ctx context.Context, after string, limit int) (*Page[Event], error)
	Balance(ctx context.Context) (*PartnerBalance, error)

	SocialAPI
}

// Client is the HTTP implementation of API.
type Client struct {
	base    string
	key     string
	http    *http.Client
	retries int
	// backoff is the first retry's wait; each retry doubles it.
	backoff  time.Duration
	maxWait  time.Duration
	sleep    func(ctx context.Context, d time.Duration) error
	jitterFn func(d time.Duration) time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the default client (20 s timeout).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithRetries sets how many times a retryable failure is retried, and the first wait.
func WithRetries(n int, backoff time.Duration) Option {
	return func(c *Client) { c.retries, c.backoff = n, backoff }
}

// New builds a client for baseURL ("" is DefaultBaseURL) with a partner key.
func New(baseURL, apiKey string, opts ...Option) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{
		base:    strings.TrimRight(baseURL, "/"),
		key:     apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
		retries: 3,
		backoff: 300 * time.Millisecond,
		maxWait: 15 * time.Second,
		sleep:   sleepCtx,
		jitterFn: func(d time.Duration) time.Duration {
			return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type request struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	key         string
}

// do sends one call, retrying 429 and 5xx (honouring Retry-After) and
// transport failures. Writes are safe to retry because they carry a key.
func (c *Client) do(ctx context.Context, r request, out any) error {
	if r.method != http.MethodGet && r.key == "" {
		return &Error{Code: CodeInvalidRequest, cause: errors.New("a write needs an Idempotency-Key")}
	}
	u := c.base + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	var last *Error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			wait := c.jitterFn(c.backoff << (attempt - 1))
			if last != nil && last.RetryAfter > 0 {
				wait = last.RetryAfter
			}
			if wait > c.maxWait {
				wait = c.maxWait
			}
			if err := c.sleep(ctx, wait); err != nil {
				return &Error{Code: CodeTransport, cause: err}
			}
		}
		err := c.once(ctx, u, r, out)
		if err == nil {
			return nil
		}
		gerr, ok := AsError(err)
		if !ok || !gerr.Retryable() || ctx.Err() != nil {
			return err
		}
		last = gerr
	}
	return last
}

func (c *Client) once(ctx context.Context, u string, r request, out any) error {
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return &Error{Code: CodeInvalidRequest, cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if _, raw := out.(*rawBody); raw {
		req.Header.Set("Accept", "*/*")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if r.body != nil {
		ct := r.contentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	if r.key != "" {
		req.Header.Set("Idempotency-Key", r.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: CodeTransport, cause: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return &Error{Code: CodeTransport, cause: err}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if rb, ok := out.(*rawBody); ok {
			rb.data, rb.contentType = raw, resp.Header.Get("Content-Type")
			return nil
		}
		if out == nil || len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return &Error{Status: resp.StatusCode, Code: CodeInternal, Message: "unreadable response", cause: err}
		}
		return nil
	}
	return decodeError(resp, raw)
}

func decodeError(resp *http.Response, raw []byte) *Error {
	var env struct {
		Error *Error `json:"error"`
	}
	e := &Error{}
	if json.Unmarshal(raw, &env) == nil && env.Error != nil {
		e = env.Error
	}
	e.Status = resp.StatusCode
	if e.Code == "" {
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			e.Code = CodeRateLimited
		case resp.StatusCode == http.StatusNotFound:
			e.Code = CodeNotFound
		case resp.StatusCode == http.StatusUnauthorized:
			e.Code = CodeInvalidRequest
		case resp.StatusCode >= 500:
			e.Code = CodeUpstreamUnavailable
		default:
			e.Code = CodeInvalidRequest
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	e.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), time.Now())
	return e
}

// retryAfter reads Retry-After as seconds or an HTTP date.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, request{method: http.MethodGet, path: path, query: q}, out)
}

func (c *Client) send(ctx context.Context, method, path string, in any, key string, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return &Error{Code: CodeInvalidRequest, cause: err}
		}
		body = b
	}
	return c.do(ctx, request{method: method, path: path, body: body, key: key}, out)
}

func esc(s string) string { return url.PathEscape(s) }

func pageQuery(cursor string, limit int) url.Values {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

func (c *Client) UpsertAccount(ctx context.Context, externalID string, in AccountInput, key string) (*Account, error) {
	var out Account
	return ret(&out, c.send(ctx, http.MethodPut, "/accounts/"+esc(externalID), in, key, &out))
}

func (c *Client) GetAccount(ctx context.Context, externalID string) (*Account, error) {
	var out Account
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID), nil, &out))
}

func (c *Client) ListAccounts(ctx context.Context, cursor string, limit int) (*Page[Account], error) {
	var out Page[Account]
	return ret(&out, c.get(ctx, "/accounts", pageQuery(cursor, limit), &out))
}

func (c *Client) GetQuote(ctx context.Context, externalID, quoteID string) (*Quote, error) {
	var out Quote
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/quotes/"+esc(quoteID), nil, &out))
}

func (c *Client) MailboxPhoto(ctx context.Context, mailboxID string) ([]byte, string, error) {
	var raw rawBody
	if err := c.do(ctx, request{method: http.MethodGet, path: "/mailboxes/" + esc(mailboxID) + "/photo"}, &raw); err != nil {
		return nil, "", err
	}
	return raw.data, raw.contentType, nil
}

func (c *Client) MailboxAccessToken(ctx context.Context, mailboxID string, scopes []string, key string) (*AccessToken, error) {
	body := map[string]any{}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	var out AccessToken
	return ret(&out, c.send(ctx, http.MethodPost, "/mailboxes/"+esc(mailboxID)+"/access-token", body, key, &out))
}

func (c *Client) PutMailboxSignInLink(ctx context.Context, mailboxID, url string, expiresAt time.Time, key string) (*Mailbox, error) {
	var out Mailbox
	body := map[string]any{"url": url, "expires_at": expiresAt.UTC().Format(time.RFC3339)}
	return ret(&out, c.send(ctx, http.MethodPut, "/mailboxes/"+esc(mailboxID)+"/sign-in-link", body, key, &out))
}

func (c *Client) MailboxSignedIn(ctx context.Context, mailboxID, key string) (*Mailbox, error) {
	var out Mailbox
	return ret(&out, c.send(ctx, http.MethodPost, "/mailboxes/"+esc(mailboxID)+"/signed-in", map[string]any{}, key, &out))
}

func (c *Client) GetDelegation(ctx context.Context) (*Delegation, error) {
	var out Delegation
	return ret(&out, c.get(ctx, "/partner/delegation", nil, &out))
}

func (c *Client) PutAccountDelegation(ctx context.Context, externalID string, d Delegation, key string) (*Delegation, error) {
	d.UpdatedAt = nil
	var out Delegation
	return ret(&out, c.send(ctx, http.MethodPut, "/accounts/"+esc(externalID)+"/delegation", d, key, &out))
}

func (c *Client) GetAccountDelegation(ctx context.Context, externalID string) (*Delegation, error) {
	var out Delegation
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/delegation", nil, &out))
}

func (c *Client) DeleteAccountDelegation(ctx context.Context, externalID, key string) error {
	return c.send(ctx, http.MethodDelete, "/accounts/"+esc(externalID)+"/delegation", nil, key, nil)
}

func (c *Client) GetPartnerSettings(ctx context.Context) (*PartnerSettings, error) {
	var out PartnerSettings
	return ret(&out, c.get(ctx, "/partner/settings", nil, &out))
}

func (c *Client) UpdatePartnerSettings(ctx context.Context, in PartnerSettings, key string) (*PartnerSettings, error) {
	body := map[string]any{}
	if in.RegistrantNotices != "" {
		body["registrant_notices"] = in.RegistrantNotices
	}
	if in.DMARCPolicyDefault != "" {
		body["dmarc_policy_default"] = in.DMARCPolicyDefault
	}
	var out PartnerSettings
	return ret(&out, c.send(ctx, http.MethodPatch, "/partner/settings", body, key, &out))
}

func (c *Client) GetWebhook(ctx context.Context) (*WebhookEndpoint, error) {
	var out WebhookEndpoint
	return ret(&out, c.get(ctx, "/partner/webhook", nil, &out))
}

func (c *Client) Pricing(ctx context.Context) (*Pricing, error) {
	var out Pricing
	return ret(&out, c.get(ctx, "/pricing", nil, &out))
}

// Search looks up domains for query; exactOnly skips suggestions, which answers much sooner.
func (c *Client) Search(ctx context.Context, query string, tlds []string, limit int, exactOnly bool) (*SearchResult, error) {
	q := url.Values{"q": {query}}
	if exactOnly {
		q.Set("suggestions", "false")
	}
	if len(tlds) > 0 {
		q.Set("tlds", strings.Join(tlds, ","))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out SearchResult
	return ret(&out, c.get(ctx, "/domains/search", q, &out))
}

func (c *Client) Check(ctx context.Context, domains []string, key string) ([]DomainOffer, error) {
	var out struct {
		Data []DomainOffer `json:"data"`
	}
	err := c.send(ctx, http.MethodPost, "/domains/check", map[string]any{"domains": domains}, key, &out)
	return out.Data, err
}

func (c *Client) CreateQuote(ctx context.Context, externalID string, items []QuoteItem, key string) (*Quote, error) {
	var out Quote
	return ret(&out, c.send(ctx, http.MethodPost, "/accounts/"+esc(externalID)+"/quotes", map[string]any{"items": items}, key, &out))
}

func (c *Client) CreateOrder(ctx context.Context, externalID, quoteID, forwardingURL, key string) (*Order, error) {
	body := map[string]any{"quote_id": quoteID}
	if forwardingURL != "" {
		body["forwarding_url"] = forwardingURL
	}
	var out Order
	return ret(&out, c.send(ctx, http.MethodPost, "/accounts/"+esc(externalID)+"/orders", body, key, &out))
}

func (c *Client) GetOrder(ctx context.Context, externalID, orderID string) (*Order, error) {
	var out Order
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/orders/"+esc(orderID), nil, &out))
}

func (c *Client) ListOrders(ctx context.Context, externalID, cursor string, limit int) (*Page[Order], error) {
	var out Page[Order]
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/orders", pageQuery(cursor, limit), &out))
}

func (c *Client) ListDomains(ctx context.Context, externalID, cursor string, limit int) (*Page[Domain], error) {
	var out Page[Domain]
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/domains", pageQuery(cursor, limit), &out))
}

func (c *Client) GetDomain(ctx context.Context, domainID string) (*Domain, error) {
	var out Domain
	return ret(&out, c.get(ctx, "/domains/"+esc(domainID), nil, &out))
}

func (c *Client) UpdateDomain(ctx context.Context, domainID string, patch DomainPatch, key string) (*Domain, error) {
	var out Domain
	return ret(&out, c.send(ctx, http.MethodPatch, "/domains/"+esc(domainID), patch, key, &out))
}

func (c *Client) RenewDomain(ctx context.Context, domainID string, years int, expectedExpiresAt time.Time, key string) (*Domain, error) {
	body := map[string]any{"years": years, "expected_expires_at": expectedExpiresAt.UTC().Format(time.RFC3339)}
	var out Domain
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/renew", body, key, &out))
}

func (c *Client) TransferOut(ctx context.Context, domainID, key string) (*TransferOut, error) {
	var out TransferOut
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/transfer-out", map[string]any{}, key, &out))
}

func (c *Client) ListDNS(ctx context.Context, domainID string) ([]DNSRecord, error) {
	var out struct {
		Data []DNSRecord `json:"data"`
	}
	err := c.get(ctx, "/domains/"+esc(domainID)+"/dns", nil, &out)
	return out.Data, err
}

func (c *Client) AddDNS(ctx context.Context, domainID string, rec DNSRecordInput, key string) (*DNSRecord, error) {
	var out DNSRecord
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/dns", rec, key, &out))
}

func (c *Client) UpdateDNS(ctx context.Context, domainID, recordID string, patch DNSRecordPatch, key string) (*DNSRecord, error) {
	var out DNSRecord
	return ret(&out, c.send(ctx, http.MethodPatch, "/domains/"+esc(domainID)+"/dns/"+esc(recordID), patch, key, &out))
}

func (c *Client) DeleteDNS(ctx context.Context, domainID, recordID, key string) error {
	return c.send(ctx, http.MethodDelete, "/domains/"+esc(domainID)+"/dns/"+esc(recordID), nil, key, nil)
}

func (c *Client) CheckDNS(ctx context.Context, domainID, key string) (*DNSCheck, error) {
	var out DNSCheck
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/dns/check", map[string]any{}, key, &out))
}

func (c *Client) RepairDNS(ctx context.Context, domainID, key string) (*DNSCheck, error) {
	var out DNSCheck
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/dns/repair", map[string]any{}, key, &out))
}

func (c *Client) ConnectDomain(ctx context.Context, externalID, domain, method, key string) (*Domain, error) {
	var out Domain
	body := map[string]any{"domain": domain, "method": method}
	return ret(&out, c.send(ctx, http.MethodPost, "/accounts/"+esc(externalID)+"/domains/connect", body, key, &out))
}

func (c *Client) CheckConnection(ctx context.Context, domainID, key string) (*Domain, error) {
	var out Domain
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/connection/check", map[string]any{}, key, &out))
}

func (c *Client) RegenerateConnection(ctx context.Context, domainID, key string) (*Domain, error) {
	var out Domain
	return ret(&out, c.send(ctx, http.MethodPost, "/domains/"+esc(domainID)+"/connection/regenerate", map[string]any{}, key, &out))
}

func (c *Client) RemoveDomain(ctx context.Context, domainID, key string) error {
	return c.send(ctx, http.MethodDelete, "/domains/"+esc(domainID), nil, key, nil)
}

func (c *Client) ListMailboxes(ctx context.Context, externalID, domainID, cursor string, limit int) (*Page[Mailbox], error) {
	q := pageQuery(cursor, limit)
	if domainID != "" {
		q.Set("domain_id", domainID)
	}
	var out Page[Mailbox]
	return ret(&out, c.get(ctx, "/accounts/"+esc(externalID)+"/mailboxes", q, &out))
}

func (c *Client) GetMailbox(ctx context.Context, mailboxID string) (*Mailbox, error) {
	var out Mailbox
	return ret(&out, c.get(ctx, "/mailboxes/"+esc(mailboxID), nil, &out))
}

func (c *Client) UpdateMailbox(ctx context.Context, mailboxID string, patch MailboxPatch, key string) (*Mailbox, error) {
	var out Mailbox
	return ret(&out, c.send(ctx, http.MethodPatch, "/mailboxes/"+esc(mailboxID), patch, key, &out))
}

func (c *Client) SetMailboxPhoto(ctx context.Context, mailboxID, filename string, data []byte, key string) (*Mailbox, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	if _, err := part.Write(data); err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	if err := w.Close(); err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	var out Mailbox
	err = c.do(ctx, request{method: http.MethodPut, path: "/mailboxes/" + esc(mailboxID) + "/photo",
		body: buf.Bytes(), contentType: w.FormDataContentType(), key: key}, &out)
	return ret(&out, err)
}

func (c *Client) CancelMailbox(ctx context.Context, mailboxID, key string) (*Mailbox, error) {
	var out Mailbox
	return ret(&out, c.send(ctx, http.MethodDelete, "/mailboxes/"+esc(mailboxID), nil, key, &out))
}

func (c *Client) ResumeMailbox(ctx context.Context, mailboxID, key string) (*Mailbox, error) {
	var out Mailbox
	return ret(&out, c.send(ctx, http.MethodPost, "/mailboxes/"+esc(mailboxID)+"/resume", map[string]any{}, key, &out))
}

func (c *Client) MailboxCredentials(ctx context.Context, mailboxID string) (*Credentials, error) {
	var out Credentials
	return ret(&out, c.get(ctx, "/mailboxes/"+esc(mailboxID)+"/credentials", nil, &out))
}

func (c *Client) PutDelegation(ctx context.Context, d Delegation, key string) error {
	d.UpdatedAt = nil
	return c.send(ctx, http.MethodPut, "/partner/delegation", d, key, nil)
}

func (c *Client) PutWebhook(ctx context.Context, endpoint string, events []string, key string) (*WebhookEndpoint, error) {
	if len(events) == 0 {
		events = []string{"*"}
	}
	var out WebhookEndpoint
	return ret(&out, c.send(ctx, http.MethodPut, "/partner/webhook", map[string]any{"url": endpoint, "events": events}, key, &out))
}

func (c *Client) RotateWebhook(ctx context.Context, key string) (*WebhookEndpoint, error) {
	var out WebhookEndpoint
	return ret(&out, c.send(ctx, http.MethodPost, "/partner/webhook/rotate", map[string]any{}, key, &out))
}

func (c *Client) DeleteWebhook(ctx context.Context, key string) error {
	return c.send(ctx, http.MethodDelete, "/partner/webhook", nil, key, nil)
}

func (c *Client) Events(ctx context.Context, after string, limit int) (*Page[Event], error) {
	q := url.Values{}
	if after != "" {
		q.Set("after", after)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out Page[Event]
	return ret(&out, c.get(ctx, "/events", q, &out))
}

func (c *Client) Balance(ctx context.Context) (*PartnerBalance, error) {
	var out PartnerBalance
	return ret(&out, c.get(ctx, "/partner/balance", nil, &out))
}

// String names the client without its key.
func (c *Client) String() string { return fmt.Sprintf("graphkit(%s)", c.base) }

// rawBody receives a non-JSON answer (a photo).
type rawBody struct {
	data        []byte
	contentType string
}

// ret drops the half-filled value of a failed call.
func ret[T any](v *T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return v, nil
}
