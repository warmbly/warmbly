// Package companyenrich provides explicit, one-request CompanyEnrich actions.
package companyenrich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	apiHost = "https://api.companyenrich.com"
	// MaxRequestBytes bounds JSON accepted by both handlers and the client.
	MaxRequestBytes  = 256 << 10
	maxResponseBytes = 8 << 20
)

var (
	ErrKey         = errors.New("CompanyEnrich rejected this API key. Disconnect this integration, then connect CompanyEnrich again with a valid key")
	ErrForbidden   = errors.New("this CompanyEnrich account is not allowed to perform this operation")
	ErrCredits     = errors.New("this CompanyEnrich account has insufficient credits")
	ErrRateLimited = errors.New("CompanyEnrich is rate limiting this account. Wait before making another request")
	ErrUnavailable = errors.New("CompanyEnrich did not complete the request. Check the provider before trying again; a request may already have consumed credits")
	ErrResponse    = errors.New("CompanyEnrich returned an invalid or oversized response")
	ErrNotFound    = errors.New("CompanyEnrich did not find a matching record")
	ErrRejected    = errors.New("CompanyEnrich refused the request. Check the operation inputs and account capabilities")
)

// Client always targets the public HTTPS API and never retries or redirects.
type Client struct {
	key  string
	http *http.Client
}

// NewClient creates a client for one sealed workspace credential.
func NewClient(key string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A dedicated connection per call prevents net/http's transparent retry
	// of a GET when a previously pooled connection breaks before a response.
	transport.DisableKeepAlives = true
	return &Client{key: strings.TrimSpace(key), http: &http.Client{
		Timeout:       30 * time.Second,
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func validKey(key string) bool {
	return key != "" && len(key) <= 4096 && !strings.ContainsAny(key, "\r\n\x00")
}

// Query validates and makes exactly one request. Counts and arrays are valid
// successful results, as are objects with future, unknown response fields.
func (c *Client) Query(ctx context.Context, in models.CompanyEnrichQuery) (*models.CompanyEnrichResult, error) {
	op, body, query, err := prepare(in)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, op.Method, op.Path, body, query)
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, query url.Values) (*models.CompanyEnrichResult, error) {
	if !validKey(c.key) {
		return nil, ErrKey
	}
	if len(body) > MaxRequestBytes {
		return nil, invalid("request is too large")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := apiHost + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, ErrRejected
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrKey
	case http.StatusForbidden:
		return nil, ErrForbidden
	case http.StatusPaymentRequired:
		return nil, ErrCredits
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	}
	if resp.StatusCode >= 500 {
		return nil, ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrRejected
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, ErrResponse
	}
	var data any
	if err := decodeJSON(raw, &data); err != nil {
		return nil, ErrResponse
	}
	out := &models.CompanyEnrichResult{Data: safeNumbers(data)}
	// Only report an explicit provider value, never a guessed charge. Most
	// endpoints do not return one, so absence is meaningful.
	if obj, ok := data.(map[string]any); ok {
		if n, ok := obj["creditsConsumed"].(json.Number); ok {
			if v, e := n.Float64(); e == nil && !math.IsInf(v, 0) && v >= 0 {
				out.CreditsConsumed = &v
			}
		}
	}
	if out.CreditsConsumed == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(resp.Header.Get("x-credit-cost")), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 {
			out.CreditsConsumed = &v
		}
	}
	return out, nil
}

func decodeJSON(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return ErrResponse
	}
	return nil
}

func safeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, ok := new(big.Rat).SetString(x.String()); ok && n.IsInt() {
			limit := big.NewInt(9007199254740991)
			if new(big.Int).Abs(n.Num()).Cmp(limit) > 0 {
				return n.Num().String()
			}
		}
	case map[string]any:
		for k, item := range x {
			x[k] = safeNumbers(item)
		}
	case []any:
		for i, item := range x {
			x[i] = safeNumbers(item)
		}
	}
	return v
}
