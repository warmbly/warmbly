package outreachsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultMaxBody bounds one response; a provider page is far smaller.
const defaultMaxBody = 16 << 20

// APIError is a refusal the provider explained.
type APIError struct {
	Label   string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s refused the request: %s", e.Label, e.Message)
	}
	return fmt.Sprintf("%s refused the request (HTTP %d)", e.Label, e.Status)
}

// Client is a small JSON client every provider shares: auth, retries on a
// rate limit or a server error, and errors written for a person.
type Client struct {
	// Label is the provider's name as errors show it.
	Label string
	Base  string
	// Auth sets the credential on each request.
	Auth func(r *http.Request)
	HTTP *http.Client
	// Sleep is replaced in tests so retries do not wait.
	Sleep func(ctx context.Context, d time.Duration) error
	// Attempts bounds retries of one request, 5 when zero.
	Attempts int
	// MaxBody bounds one response in bytes, 16 MiB when zero. Raise it only
	// for an endpoint that returns a whole export unpaged.
	MaxBody int64
}

// NewClient builds a client for one provider's base URL.
func NewClient(label, base string, auth func(*http.Request)) *Client {
	return &Client{Label: label, Base: strings.TrimRight(base, "/"), Auth: auth, HTTP: &http.Client{Timeout: 45 * time.Second}, Sleep: SleepCtx}
}

// SleepCtx waits d or until ctx ends.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Get decodes GET path?query into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, query, nil, out)
}

// Post decodes POST path with a JSON body into out.
func (c *Client) Post(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, query, body, out)
}

// Do sends one request. Only reads are ever sent: a provider is never written to.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = b
	}
	target := c.Base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	attempts := c.Attempts
	if attempts <= 0 {
		attempts = 5
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		var rd io.Reader
		if raw != nil {
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.Auth != nil {
			c.Auth(req)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = c.wrap(ErrUnavailable)
			if serr := c.Sleep(ctx, Backoff(attempt, "")); serr != nil {
				return serr
			}
			continue
		}
		limit := c.MaxBody
		if limit <= 0 {
			limit = defaultMaxBody
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		if int64(len(data)) > limit {
			return fmt.Errorf("%s: response to %s is larger than %d MiB", c.Label, path, limit>>20)
		}
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out == nil || len(bytes.TrimSpace(data)) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("%s: decode %s: %w", c.Label, path, err)
			}
			return nil
		case resp.StatusCode == http.StatusUnauthorized:
			return c.wrap(ErrKey)
		case resp.StatusCode == http.StatusForbidden:
			return c.wrap(ErrForbidden)
		case resp.StatusCode == http.StatusTooManyRequests:
			lastErr = c.wrap(ErrRateLimited)
		case resp.StatusCode >= 500:
			lastErr = c.wrap(ErrUnavailable)
		default:
			return &APIError{Label: c.Label, Status: resp.StatusCode, Message: apiMessage(data)}
		}
		if serr := c.Sleep(ctx, Backoff(attempt, resp.Header.Get("Retry-After"))); serr != nil {
			return serr
		}
	}
	return lastErr
}

// wrap names the provider while keeping errors.Is working.
func (c *Client) wrap(err error) error {
	return &labelled{label: c.Label, err: err}
}

type labelled struct {
	label string
	err   error
}

func (l *labelled) Error() string { return l.label + ": " + l.err.Error() }
func (l *labelled) Unwrap() error { return l.err }

// Backoff is the wait before retry attempt+1, honouring Retry-After seconds.
func Backoff(attempt int, retryAfter string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && s > 0 {
		if s > 60 {
			s = 60
		}
		return time.Duration(s) * time.Second
	}
	d := time.Second << attempt
	if d > 20*time.Second {
		d = 20 * time.Second
	}
	return d
}

// apiMessage pulls a short reason out of an error body without echoing it whole.
func apiMessage(data []byte) string {
	var m map[string]any
	if json.Unmarshal(data, &m) == nil {
		for _, k := range []string{"message", "error", "detail", "error_message", "msg"} {
			if s, ok := m[k].(string); ok && s != "" {
				return truncate(s, 200)
			}
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Plain turns any provider failure into text a person can act on, never a raw
// transport error.
func Plain(err error) string {
	var ae *APIError
	var lb *labelled
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ae):
		return ae.Error()
	case errors.As(err, &lb):
		return lb.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return ErrUnavailable.Error()
	}
	return ErrUnavailable.Error()
}
