// Package cfsaas registers customer hostnames as Cloudflare for SaaS custom
// hostnames, so a name a customer points at the tracking host is served
// instead of refused at Cloudflare's edge (error 1000 or 1014).
package cfsaas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

// Config is the SaaS zone the tracking host lives on; both values are required.
type Config struct {
	ZoneID   string
	APIToken string
	// Origin overrides the zone's fallback origin for the hostnames this creates; optional.
	Origin     string
	BaseURL    string
	HTTPClient *http.Client
}

// Client talks to one zone's custom hostnames.
type Client struct {
	cfg Config
}

// New is nil unless the zone and token are both set, which keeps the feature off by default.
func New(cfg Config) *Client {
	cfg.ZoneID = strings.TrimSpace(cfg.ZoneID)
	cfg.APIToken = strings.TrimSpace(cfg.APIToken)
	if cfg.ZoneID == "" || cfg.APIToken == "" {
		return nil
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{cfg: cfg}
}

// TXTRecord is a record Cloudflare asks the hostname's owner to publish.
type TXTRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Hostname is Cloudflare's view of one custom hostname.
type Hostname struct {
	ID        string
	Hostname  string
	Status    string // active, pending, moved, blocked, ...
	SSLStatus string
	// Ownership is the TXT that proves the name when its DNS does not CNAME to the zone.
	Ownership *TXTRecord
	Errors    []string
}

// Active is a hostname Cloudflare routes to the zone.
func (h *Hostname) Active() bool { return h != nil && h.Status == "active" }

type apiHostname struct {
	ID     string `json:"id"`
	Host   string `json:"hostname"`
	Status string `json:"status"`
	SSL    struct {
		Status           string `json:"status"`
		ValidationErrors []struct {
			Message string `json:"message"`
		} `json:"validation_errors"`
	} `json:"ssl"`
	Ownership *struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"ownership_verification"`
	VerificationErrors []string `json:"verification_errors"`
}

func (a apiHostname) hostname() *Hostname {
	h := &Hostname{ID: a.ID, Hostname: a.Host, Status: a.Status, SSLStatus: a.SSL.Status}
	if a.Ownership != nil && a.Ownership.Type == "txt" && a.Ownership.Name != "" {
		h.Ownership = &TXTRecord{Name: strings.TrimSuffix(a.Ownership.Name, "."), Value: a.Ownership.Value}
	}
	h.Errors = append(h.Errors, a.VerificationErrors...)
	for _, e := range a.SSL.ValidationErrors {
		h.Errors = append(h.Errors, e.Message)
	}
	return h
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

// APIError is a refusal from Cloudflare, carrying its numeric codes.
type APIError struct {
	Status int
	Codes  []int
	Msg    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cloudflare: %d %s", e.Status, e.Msg)
}

// duplicateCode is Cloudflare's answer to a create for a hostname the zone already has.
const duplicateCode = 1406

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+"/zones/"+url.PathEscape(c.cfg.ZoneID)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{Status: resp.StatusCode, Msg: "unreadable response"}
	}
	if !env.Success || resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode}
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			ae.Codes = append(ae.Codes, e.Code)
			msgs = append(msgs, e.Message)
		}
		ae.Msg = strings.Join(msgs, "; ")
		return ae
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Zone is the zone the hostnames are registered on.
func (c *Client) Zone() string { return c.cfg.ZoneID }

// Find is the zone's custom hostname for name, or nil when it has none.
func (c *Client) Find(ctx context.Context, name string) (*Hostname, error) {
	name = normalize(name)
	var got []apiHostname
	if err := c.do(ctx, http.MethodGet, "/custom_hostnames?hostname="+url.QueryEscape(name), nil, &got); err != nil {
		return nil, err
	}
	for _, h := range got {
		if normalize(h.Host) == name {
			return h.hostname(), nil
		}
	}
	return nil, nil
}

// Ensure registers name unless the zone already has it, and returns the zone's view either way.
func (c *Client) Ensure(ctx context.Context, name string) (*Hostname, error) {
	name = normalize(name)
	if h, err := c.Find(ctx, name); err != nil || h != nil {
		return h, err
	}
	body := map[string]any{
		"hostname": name,
		"ssl": map[string]any{
			"method":   "http",
			"type":     "dv",
			"settings": map[string]any{"min_tls_version": "1.2"},
		},
	}
	if c.cfg.Origin != "" {
		body["custom_origin_server"] = c.cfg.Origin
	}
	var created apiHostname
	err := c.do(ctx, http.MethodPost, "/custom_hostnames", body, &created)
	var ae *APIError
	if errors.As(err, &ae) && ae.has(duplicateCode) {
		// Another replica created it between the find and the create.
		return c.Find(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	return created.hostname(), nil
}

// Remove deletes name from the zone; one the zone does not have is already removed.
func (c *Client) Remove(ctx context.Context, name string) error {
	h, err := c.Find(ctx, name)
	if err != nil || h == nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, "/custom_hostnames/"+url.PathEscape(h.ID), nil, nil)
}

func (e *APIError) has(code int) bool {
	for _, c := range e.Codes {
		if c == code {
			return true
		}
	}
	return false
}

func normalize(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}
