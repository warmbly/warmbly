package mailvendor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	defaultBodyLimit = 8 << 20
	// listBodyLimit is for vendors that return every mailbox in one unpaginated response.
	listBodyLimit = 32 << 20
	maxAttempts   = 3
	maxRetryWait  = 30 * time.Second
	// maxPages stops a vendor whose pagination never terminates.
	maxPages = 2000
)

// Error is what every failed call returns. It never carries a credential or a response body.
type Error struct {
	Vendor string
	// Status is the HTTP status, or 0 when no response arrived.
	Status int
	Reason string
	kind   error
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("mailvendor: %s: %s", e.Vendor, e.Reason)
	}
	return fmt.Sprintf("mailvendor: %s: HTTP %d: %s", e.Vendor, e.Status, e.Reason)
}

func (e *Error) Unwrap() error { return e.kind }

func vendorErr(vendor string, status int, reason string, kind error) error {
	return &Error{Vendor: vendor, Status: status, Reason: reason, kind: kind}
}

// statusErr maps a non-2xx status to an error kind.
func statusErr(vendor string, status int) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return vendorErr(vendor, status, "API key rejected", ErrUnauthorized)
	case status == http.StatusNotFound:
		return vendorErr(vendor, status, "not found", ErrNotFound)
	case status == http.StatusTooManyRequests:
		return vendorErr(vendor, status, "rate limited", ErrRateLimited)
	case status >= 500:
		return vendorErr(vendor, status, "vendor server error", nil)
	default:
		return vendorErr(vendor, status, "request refused", nil)
	}
}

// throttle spaces requests at least every apart.
type throttle struct {
	mu    sync.Mutex
	every time.Duration
	next  time.Time
}

func (t *throttle) wait(ctx context.Context, sleep func(context.Context, time.Duration) error) error {
	if t == nil || t.every <= 0 {
		return nil
	}
	t.mu.Lock()
	now := time.Now()
	at := t.next
	if at.Before(now) {
		at = now
	}
	t.next = at.Add(t.every)
	t.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		return sleep(ctx, d)
	}
	return nil
}

// transport is the shared HTTP half of every vendor client.
type transport struct {
	vendor   string
	base     string
	hc       *http.Client
	auth     func(http.Header)
	throttle *throttle
	sleep    func(context.Context, time.Duration) error
}

func newTransport(vendor, defaultBase string, o options, auth func(http.Header), perSecond int) *transport {
	base := defaultBase
	if o.baseURL != "" {
		base = o.baseURL
	}
	t := &transport{vendor: vendor, base: base, hc: o.httpClient, auth: auth, sleep: o.sleep}
	if perSecond > 0 {
		t.throttle = &throttle{every: time.Second / time.Duration(perSecond)}
	}
	return t
}

type call struct {
	method string
	path   string
	query  url.Values
	body   any
	header http.Header
	limit  int64
	// once answers a 429 at once, for a call the vendor counts against a budget.
	once bool
}

// do runs one call, retrying a 429 up to maxAttempts, and decodes a 2xx body into out.
func (t *transport) do(ctx context.Context, c call, out any) error {
	var payload []byte
	if c.body != nil {
		b, err := json.Marshal(c.body)
		if err != nil {
			return vendorErr(t.vendor, 0, "could not encode request", nil)
		}
		payload = b
	}
	limit := c.limit
	if limit <= 0 {
		limit = defaultBodyLimit
	}
	u := t.base + c.path
	if len(c.query) > 0 {
		u += "?" + c.query.Encode()
	}
	for attempt := 1; ; attempt++ {
		if err := t.throttle.wait(ctx, t.sleep); err != nil {
			return err
		}
		status, header, body, err := t.roundTrip(ctx, c, u, payload, limit)
		if err != nil {
			return err
		}
		if status == http.StatusTooManyRequests {
			wait, ok := retryAfter(header, attempt)
			if c.once || attempt >= maxAttempts || !ok {
				return statusErr(t.vendor, status)
			}
			if err := t.sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}
		if status < 200 || status > 299 {
			return statusErr(t.vendor, status)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return vendorErr(t.vendor, status, "malformed response", nil)
		}
		return nil
	}
}

func (t *transport) roundTrip(ctx context.Context, c call, u string, payload []byte, limit int64) (int, http.Header, []byte, error) {
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, c.method, u, rdr)
	if err != nil {
		return 0, nil, nil, vendorErr(t.vendor, 0, "could not build request", nil)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range c.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	t.auth(req.Header)
	resp, err := t.hc.Do(req)
	if err != nil {
		return 0, nil, nil, transportErr(ctx, t.vendor, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, nil, nil, transportErr(ctx, t.vendor, err)
	}
	if int64(len(body)) > limit {
		return 0, nil, nil, vendorErr(t.vendor, resp.StatusCode, "response too large", nil)
	}
	return resp.StatusCode, resp.Header, body, nil
}

// transportErr keeps the context error and drops the rest, since a url.Error quotes the URL.
func transportErr(ctx context.Context, vendor string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return vendorErr(vendor, 0, "request timed out", nil)
	}
	return vendorErr(vendor, 0, "request failed", nil)
}

// retryAfter returns how long to wait before the next attempt, and false when that is too long to wait.
func retryAfter(h http.Header, attempt int) (time.Duration, bool) {
	wait := time.Duration(attempt) * time.Second
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		} else if at, err := http.ParseTime(v); err == nil {
			wait = max(time.Until(at), 0)
		}
	}
	return wait, wait <= maxRetryWait
}

// credCache holds credentials a vendor's list already returned.
type credCache struct {
	mu sync.Mutex
	m  map[string]Credentials
}

func (c *credCache) put(id string, cr Credentials) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]Credentials)
	}
	c.m[id] = cr
}

func (c *credCache) get(id string) (Credentials, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cr, ok := c.m[id]
	return cr, ok
}

func bearer(key string) func(http.Header) {
	return func(h http.Header) { h.Set("Authorization", "Bearer "+key) }
}
