// Package api is the CLI's HTTP client for the public Warmbly REST API.
//
// It exists so every command speaks to the API the same way: one place that
// knows the /v1 prefix, the bearer header, the idempotency header, the error
// envelope and how to walk a cursor. Nothing here is Warmbly-specific beyond
// those; the typed commands are a table on top of it.
package api

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

// Client is one host's API surface.
type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
	// Debug prints the request line to stderr, which is the first thing
	// anyone wants when a call goes somewhere unexpected.
	Debug io.Writer
}

func New(baseURL, token, userAgent string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     token,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 120 * time.Second},
	}
}

// Error is a non-2xx response. Every field comes from the API's stable
// envelope, so a script can branch on Code without reading the prose.
type Error struct {
	Status     int
	Code       string
	Message    string
	RequestID  string
	RetryAfter string
	Method     string
	Path       string
	Body       string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	out := fmt.Sprintf("%s %s failed (HTTP %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		out += " " + e.Code
	}
	out += "): " + msg
	if e.RequestID != "" {
		out += " (request " + e.RequestID + ")"
	}
	if e.Status == http.StatusTooManyRequests && e.RetryAfter != "" {
		out += ". Rate limited; retry after " + e.RetryAfter + "s."
	}
	return out
}

// IsNotFound and IsUnauthorized are what command code branches on.
func (e *Error) IsNotFound() bool     { return e.Status == http.StatusNotFound }
func (e *Error) IsUnauthorized() bool { return e.Status == http.StatusUnauthorized }

// StatusOf returns the HTTP status of an API error, or 0.
func StatusOf(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// Request is one call. Path is relative to /v1 unless it already names a
// version, which is what makes `warmbly api get /campaigns` work.
type Request struct {
	Method  string
	Path    string
	Query   url.Values
	Body    []byte
	Headers map[string]string
	// IdempotencyKey rides the documented header, for retryable writes.
	IdempotencyKey string
	// Anonymous skips the bearer header. Only the sign-in handshake uses it:
	// the CLI has no credential yet, which is the whole point of the flow.
	Anonymous bool
}

// Response is a completed call. Body is the raw payload: JSON for every
// documented endpoint, but a few stream files, so it is not parsed here.
type Response struct {
	Status  int
	Header  http.Header
	Body    []byte
	Request *Request
}

// NormalizePath applies the /v1 rule. Exported because `warmbly api` prints
// the path it is about to call.
func NormalizePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "/v") && len(path) > 2 && path[2] >= '0' && path[2] <= '9' {
		return path
	}
	return "/v1" + path
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if c.Token == "" && !req.Anonymous {
		return nil, errors.New("no API token. Run `warmbly auth login`, or set WARMBLY_TOKEN.")
	}
	path := NormalizePath(req.Path)
	full := c.BaseURL + path
	if len(req.Query) > 0 {
		full += "?" + req.Query.Encode()
	}

	var reader io.Reader
	if len(req.Body) > 0 {
		reader = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(req.Method), full, reader)
	if err != nil {
		return nil, err
	}
	if !req.Anonymous {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpReq.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}
	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "* %s %s\n", httpReq.Method, full)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("could not reach the API at %s: %w\nCheck the host with `warmbly auth status`, or set WARMBLY_API_URL.", c.BaseURL, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the API response: %w", err)
	}
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "* HTTP %d (%d bytes)\n", resp.StatusCode, len(payload))
	}

	out := &Response{Status: resp.StatusCode, Header: resp.Header, Body: payload, Request: &req}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return out, nil
	}

	apiErr := &Error{
		Status:     resp.StatusCode,
		Method:     strings.ToUpper(req.Method),
		Path:       path,
		Body:       string(payload),
		RetryAfter: resp.Header.Get("Retry-After"),
	}
	var envelope struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(payload, &envelope) == nil {
		apiErr.Code = envelope.Code
		apiErr.RequestID = envelope.RequestID
		apiErr.Message = envelope.Message
		if apiErr.Message == "" {
			apiErr.Message = envelope.Error
		}
	}
	return out, apiErr
}

// JSON runs a request and decodes the body into v.
func (c *Client) JSON(ctx context.Context, req Request, v any) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	if v == nil || len(bytes.TrimSpace(resp.Body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Body, v); err != nil {
		return fmt.Errorf("the API returned something that is not JSON: %w", err)
	}
	return nil
}

// paginateRetries is how many rate-limited pages one walk will wait out
// before giving up. Three covers a walk that crosses a minute boundary or two;
// beyond that the budget is the problem, not the timing.
const paginateRetries = 3

// listEnvelope is the documented list shape: data plus pagination.
type listEnvelope struct {
	Data       json.RawMessage `json:"data"`
	Pagination struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
	} `json:"pagination"`
}

// Paginate walks every page of a list endpoint and returns one merged
// envelope: data holds every row, pagination reports no more pages. Endpoints
// that do not use the cursor envelope come back unchanged after one call.
func (c *Client) Paginate(ctx context.Context, req Request, maxPages int) ([]byte, error) {
	if maxPages <= 0 {
		maxPages = 100
	}
	var merged []json.RawMessage
	query := url.Values{}
	for k, v := range req.Query {
		query[k] = v
	}

	// Retries are budgeted across the whole walk, not per page: a limit that
	// never clears has to end as an error rather than looping until maxPages.
	retriesLeft := paginateRetries

	for page := 0; page < maxPages; page++ {
		req.Query = query
		resp, err := c.Do(ctx, req)
		if err != nil {
			// A long walk will meet the per-key minute budget. The response
			// says how long to wait, so waiting is strictly better than
			// handing back a partial list the caller cannot tell from a
			// complete one.
			wait, ok := retryAfter(err)
			if !ok || retriesLeft == 0 {
				return nil, err
			}
			retriesLeft--
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			page--
			continue
		}
		var env listEnvelope
		if jerr := json.Unmarshal(resp.Body, &env); jerr != nil || env.Data == nil {
			// Not a cursor list. One page is the whole answer.
			if page == 0 {
				return resp.Body, nil
			}
			break
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(env.Data, &rows); err != nil {
			if page == 0 {
				return resp.Body, nil
			}
			break
		}
		merged = append(merged, rows...)
		if !env.Pagination.HasMore || env.Pagination.NextCursor == nil || *env.Pagination.NextCursor == "" {
			break
		}
		query = cloneValues(query)
		query.Set("cursor", *env.Pagination.NextCursor)
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if merged == nil {
		data = []byte("[]")
	}
	return []byte(`{"data":` + string(data) + `,"pagination":{"has_more":false,"next_cursor":null,"total":` + fmt.Sprint(len(merged)) + `}}`), nil
}

// retryAfter reports how long a rate-limited response asked the caller to
// wait. The wait is capped so a hostile or misconfigured Retry-After cannot
// park the CLI for an hour.
func retryAfter(err error) (time.Duration, bool) {
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		return 0, false
	}
	seconds, perr := strconv.Atoi(strings.TrimSpace(apiErr.RetryAfter))
	if perr != nil || seconds <= 0 {
		seconds = 5
	}
	if seconds > 120 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second, true
}

func cloneValues(in url.Values) url.Values {
	out := url.Values{}
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
