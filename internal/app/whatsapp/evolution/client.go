// Package evolution is the single HTTP adapter for Evolution API.
// Domain packages must not import provider wire DTOs from here for business logic;
// only this package speaks Evolution's REST shapes.
package evolution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Client is a hardened Evolution API HTTP client.
type Client struct {
	baseURL    string
	apiKey     string
	http       *http.Client
	maxRetries int
	userAgent  string
}

// ClientConfig configures the Evolution HTTP client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
}

// NewClient builds a client. API key is never logged.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = 0
	}
	if retries > 5 {
		retries = 5
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:     cfg.APIKey,
		http:       &http.Client{Timeout: timeout},
		maxRetries: retries,
		userAgent:  "warmbly-whatsapp-evolution/1.0",
	}
}

// APIError is a structured provider error with redacted details.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
}

func (e *APIError) Error() string {
	if e == nil {
		return "evolution: unknown error"
	}
	return fmt.Sprintf("evolution api status=%d code=%s msg=%s", e.StatusCode, e.Code, e.Message)
}

// RedactedString never includes the API key.
func (e *APIError) RedactedString() string {
	return e.Error()
}

// IsRetryable reports whether the caller should retry.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*APIError); ok {
		return ae.Retryable
	}
	// Network / timeout style errors are retryable.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "eof")
}

// sendTextBody is Evolution POST /message/sendText/{instance}
type sendTextBody struct {
	Number string `json:"number"`
	Text   string `json:"text"`
}

// sendTemplateBody is Evolution template send for Cloud API instances.
type sendTemplateBody struct {
	Number     string           `json:"number"`
	Name       string           `json:"name"`
	Language   string           `json:"language"`
	Components []map[string]any `json:"components,omitempty"`
}

// SendTextResponse is a minimal projection of Evolution send responses.
type SendTextResponse struct {
	Key struct {
		ID string `json:"id"`
	} `json:"key"`
	Status string `json:"status"`
	// Some versions nest message id differently; we also accept top-level.
	MessageID string `json:"messageId,omitempty"`
}

// Health checks GET / (welcome) or /instance/fetchInstances.
func (c *Client) Health(ctx context.Context) error {
	_, _, err := c.do(ctx, http.MethodGet, "/", nil, nil)
	return err
}

// ConnectionState is instance connection state.
type ConnectionState struct {
	Instance string `json:"instance"`
	State    string `json:"state"`
}

// GetConnectionState GET /instance/connectionState/{instance}
func (c *Client) GetConnectionState(ctx context.Context, instance string) (*ConnectionState, error) {
	path := "/instance/connectionState/" + urlPathEscape(instance)
	body, status, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Instance ConnectionState `json:"instance"`
		// some versions return flat
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, &APIError{StatusCode: status, Code: "decode", Message: "invalid connection state payload"}
	}
	out := wrap.Instance
	if out.State == "" {
		out.State = wrap.State
	}
	if out.Instance == "" {
		out.Instance = instance
	}
	return &out, nil
}

// SendText POST /message/sendText/{instance}
func (c *Client) SendText(ctx context.Context, instance, number, text string) (*SendTextResponse, error) {
	path := "/message/sendText/" + urlPathEscape(instance)
	payload, _ := json.Marshal(sendTextBody{Number: number, Text: text})
	body, status, err := c.do(ctx, http.MethodPost, path, payload, map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return nil, err
	}
	var res SendTextResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, &APIError{StatusCode: status, Code: "decode", Message: "invalid sendText response"}
	}
	if res.Key.ID == "" && res.MessageID != "" {
		res.Key.ID = res.MessageID
	}
	return &res, nil
}

// SendTemplate POST /message/sendTemplate/{instance} (Cloud API / WHATSAPP-BUSINESS).
func (c *Client) SendTemplate(ctx context.Context, instance, number, name, language string, variables []string) (*SendTextResponse, error) {
	path := "/message/sendTemplate/" + urlPathEscape(instance)
	var components []map[string]any
	if len(variables) > 0 {
		params := make([]map[string]string, 0, len(variables))
		for _, v := range variables {
			params = append(params, map[string]string{"type": "text", "text": v})
		}
		components = []map[string]any{{
			"type":       "body",
			"parameters": params,
		}}
	}
	payload, _ := json.Marshal(sendTemplateBody{
		Number:     number,
		Name:       name,
		Language:   language,
		Components: components,
	})
	body, status, err := c.do(ctx, http.MethodPost, path, payload, map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return nil, err
	}
	var res SendTextResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, &APIError{StatusCode: status, Code: "decode", Message: "invalid sendTemplate response"}
	}
	if res.Key.ID == "" && res.MessageID != "" {
		res.Key.ID = res.MessageID
	}
	return &res, nil
}

// SetWebhook POST /webhook/set/{instance}
func (c *Client) SetWebhook(ctx context.Context, instance, url, secret string, events []string, enabled bool) error {
	path := "/webhook/set/" + urlPathEscape(instance)
	headers := map[string]any{}
	if secret != "" {
		headers["Authorization"] = "Bearer " + secret
	}
	payload, _ := json.Marshal(map[string]any{
		"webhook": map[string]any{
			"enabled": enabled,
			"url":     url,
			"headers": headers,
			"events":  events,
		},
	})
	_, _, err := c.do(ctx, http.MethodPost, path, payload, map[string]string{
		"Content-Type": "application/json",
	})
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, headers map[string]string) ([]byte, int, error) {
	if c == nil || c.baseURL == "" {
		return nil, 0, &APIError{StatusCode: 0, Code: "config", Message: "base URL not configured", Retryable: false}
	}
	var lastErr error
	attempts := c.maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// jittered backoff
			backoff := time.Duration(50*(1<<uint(attempt-1))) * time.Millisecond
			jitter := time.Duration(rand.Intn(40)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff + jitter):
			}
		}
		status, respBody, err := c.once(ctx, method, path, body, headers)
		if err == nil {
			return respBody, status, nil
		}
		lastErr = err
		if !IsRetryable(err) {
			return respBody, status, err
		}
		log.Debug().
			Int("attempt", attempt+1).
			Int("status", status).
			Str("path", path).
			Str("error", redactSecrets(err.Error(), c.apiKey)).
			Msg("evolution retry")
	}
	return nil, 0, lastErr
}

func (c *Client) once(ctx context.Context, method, path string, body []byte, headers map[string]string) (int, []byte, error) {
	url := c.baseURL + path
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, respBody, nil
	}
	ae := classifyHTTPError(resp.StatusCode, respBody)
	return resp.StatusCode, respBody, ae
}

func classifyHTTPError(status int, body []byte) *APIError {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	// Never leave API keys in error text if server echoed them.
	ae := &APIError{
		StatusCode: status,
		Code:       http.StatusText(status),
		Message:    msg,
	}
	switch status {
	case 408, 425, 429, 500, 502, 503, 504:
		ae.Retryable = true
	case 400, 401, 403, 404, 409, 422:
		ae.Retryable = false
	default:
		ae.Retryable = status >= 500
	}
	if status == 401 {
		ae.Code = "unauthorized"
		ae.Message = "invalid or missing api key"
	}
	return ae
}

func urlPathEscape(s string) string {
	// Instance names are typically alphanumeric + hyphen; reject path traversal.
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "..", "")
	return s
}

func redactSecrets(s, apiKey string) string {
	if apiKey != "" && strings.Contains(s, apiKey) {
		return strings.ReplaceAll(s, apiKey, "[REDACTED]")
	}
	return s
}

// RedactAPIKey replaces the key in arbitrary strings (for tests/logs).
func RedactAPIKey(s, apiKey string) string {
	return redactSecrets(s, apiKey)
}
