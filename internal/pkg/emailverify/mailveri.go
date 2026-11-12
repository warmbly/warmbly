package emailverify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MailVeri is the paid Verifier backed by MailVeri's single address API.
// Credits never expire and a check that ends UNKNOWN costs nothing; the
// customer's own key is stored on their integration connection.
type MailVeri struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

const (
	mailVeriAPI     = "https://api.mailveri.com"
	mailVeriTimeout = 30 * time.Second
	// mailVeriNoCredits is the API's error code for an account with no
	// credits left. It arrives with HTTP 200, not 402.
	mailVeriNoCredits = 2
	// mailVeriRateLimitAttempts and mailVeriRateLimitBackoff bound the retry
	// of a request the API refused with 429.
	mailVeriRateLimitAttempts = 3
	mailVeriRateLimitBackoff  = 250 * time.Millisecond
)

// NewMailVeri constructs the client. baseURL is overridable for tests.
func NewMailVeri(apiKey string, baseURL string) *MailVeri {
	if baseURL == "" {
		baseURL = mailVeriAPI
	}
	return &MailVeri{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: mailVeriTimeout, CheckRedirect: refuseInsecureRedirect},
	}
}

type mailVeriResponse struct {
	Error        int    `json:"error"`
	Message      string `json:"message"`
	Status       string `json:"status"`
	Email        string `json:"email"`
	Balance      *int   `json:"balance"`
	CreditsUsed  int    `json:"credits_used"`
	QualityScore int    `json:"quality_score"`
	IsForwarding bool   `json:"is_forwarding"`
}

// Verify implements Verifier. An API failure is never an "invalid" verdict:
// it degrades to unknown with the reason.
func (m *MailVeri) Verify(ctx context.Context, email string) Result {
	res, _ := m.Check(ctx, email)
	return res
}

// Check is Verify with the transport/account error surfaced.
func (m *MailVeri) Check(ctx context.Context, email string) (Result, error) {
	res := Result{Email: strings.ToLower(strings.TrimSpace(email)), CheckedAt: time.Now().UTC(), Status: StatusUnknown, Provider: ProviderMailVeri}
	body, _ := json.Marshal(map[string]string{"email": res.Email})
	var out mailVeriResponse
	if err := m.request(ctx, http.MethodPost, "/v1/verify-mail/", body, &out); err != nil {
		res.Reason = err.Error()
		return res, err
	}

	v, ok := NormalizeExternal(ProviderMailVeri, out.Status)
	if !ok {
		res.Reason = "mailveri status not recognised: " + out.Status
		return res, nil
	}
	res.Status, res.SubStatus = v.Status, v.SubStatus
	res.IsCatchAll = v.SubStatus == SubStatusCatchAll
	res.HasMX = !strings.EqualFold(out.Status, "INVALID")
	res.Confidence = out.QualityScore
	res.Reason = "mailveri: " + strings.ToLower(out.Status)
	return res, nil
}

// ObservesBalance is true: the balance endpoint is free and reports the
// remaining credits, so an exhausted account is visible without a check.
func (m *MailVeri) ObservesBalance() bool { return true }

// Account validates the key and returns the remaining credits. The balance
// endpoint is free and unthrottled.
func (m *MailVeri) Account(ctx context.Context) (*int, error) {
	var out mailVeriResponse
	if err := m.request(ctx, http.MethodGet, "/v1/get-balance/", nil, &out); err != nil {
		return nil, err
	}
	if out.Balance == nil {
		return nil, fmt.Errorf("mailveri: balance missing from the account response")
	}
	return out.Balance, nil
}

func (m *MailVeri) request(ctx context.Context, method, path string, body []byte, out *mailVeriResponse) error {
	// Verification allows 5 checks in flight per account, fewer than Warmbly's
	// paid-provider concurrency, so a 429 is retried briefly before it counts
	// as a failure.
	for attempt := 1; ; attempt++ {
		status, err := m.do(ctx, method, path, body, out)
		if status != http.StatusTooManyRequests || attempt >= mailVeriRateLimitAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("mailveri: %w", ctx.Err())
		case <-time.After(time.Duration(attempt) * mailVeriRateLimitBackoff):
		}
	}
}

func (m *MailVeri) do(ctx context.Context, method, path string, body []byte, out *mailVeriResponse) (int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, reader)
	if err != nil {
		return 0, fmt.Errorf("mailveri: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("mailveri: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// A missing, invalid, revoked or expired key answers 403.
		return resp.StatusCode, ErrProviderKey
	case http.StatusTooManyRequests:
		return resp.StatusCode, fmt.Errorf("mailveri: rate limited (HTTP 429)")
	default:
		return resp.StatusCode, fmt.Errorf("mailveri: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("mailveri: unreadable response: %w", err)
	}
	switch out.Error {
	case 0:
		return resp.StatusCode, nil
	case mailVeriNoCredits:
		return resp.StatusCode, ErrProviderCredits
	default:
		return resp.StatusCode, fmt.Errorf("mailveri: error %d: %s", out.Error, out.Message)
	}
}
