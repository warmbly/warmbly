// Package webread fetches a public page through a reader service (Crawl4AI,
// Jina Reader, a DDGS server, or any endpoint that answers with the page's
// HTML) for the times a site refuses a direct visit. Readers are operator or
// workspace configuration, tried in order, and every one returns raw HTML so
// callers parse it exactly as they parse a page they fetched themselves.
package webread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

const (
	readTimeout  = 40 * time.Second
	maxResponse  = 6 << 20
	defaultJina  = "https://r.jina.ai"
	providerJina = "jina"
)

// Providers are the reader backends this package speaks, in documentation order.
var Providers = []string{"crawl4ai", "jina", "ddgs", "custom"}

// Page is one page a reader returned.
type Page struct {
	HTML string
	// URL is where the reader landed after redirects, or the requested URL
	// when the reader does not say.
	URL string
	// Via names the reader that answered.
	Via string
}

// Reader fetches one page.
type Reader interface {
	Name() string
	Read(ctx context.Context, target string) (*Page, error)
}

// Backend is one configured reader.
type Backend struct {
	Provider string
	URL      string
	Key      string
}

// ErrNotPublic means the reader landed somewhere a caller must not read.
var ErrNotPublic = errors.New("the reader was redirected to a private address")

// Blocked reports whether a status means the site turned an automated visitor
// away, rather than the page being missing or broken.
func Blocked(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotAcceptable, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	}
	return false
}

// New builds the readers in order, skipping any missing what it needs. It
// returns nil when none is usable.
func New(backends []Backend) Reader {
	var rs []Reader
	for _, b := range backends {
		if r := build(b); r != nil {
			rs = append(rs, r)
		}
	}
	switch len(rs) {
	case 0:
		return nil
	case 1:
		return guarded{rs[0]}
	}
	return guarded{chain(rs)}
}

// Jina is a Jina Reader client for a workspace key, keyless when key is empty.
func Jina(key string) Reader {
	return guarded{build(Backend{Provider: providerJina, Key: key})}
}

// guarded hands a reader only pages a caller could have fetched itself.
type guarded struct{ Reader }

func (g guarded) Read(ctx context.Context, target string) (*Page, error) {
	if err := publicTarget(ctx, target); err != nil {
		return nil, err
	}
	return g.Reader.Read(ctx, target)
}

// publicTarget refuses a page on a private address or a non-web port.
func publicTarget(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrNotPublic
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" && !config.UnsafeWebhookURLsAllowed() {
		return ErrNotPublic
	}
	return publicURL(ctx, raw)
}

// readerClient follows redirects only within the reader's own host, so a
// reader cannot bounce the request to wherever a page points.
func readerClient(base string) *http.Client {
	host := ""
	if u, err := url.Parse(base); err == nil {
		host = u.Host
	}
	return &http.Client{
		Timeout: readTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.URL.Host != host {
				return errors.New("the reader redirected away from itself")
			}
			// An https reader's key never follows a redirect onto plain http.
			if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return errors.New("the reader redirected from https to http")
			}
			return nil
		},
	}
}

// Lookup reads one setting: an env var, or its AWS parameter or secret.
type Lookup func(envKey, awsKey string, secret bool) string

// FromConfig builds the instance's readers from WEB_READER (a comma-separated
// list, tried in order) and each one's WEB_READER_<PROVIDER>_URL and
// WEB_READER_<PROVIDER>_API_KEY.
func FromConfig(get Lookup) Reader {
	var backends []Backend
	for _, p := range splitList(get("WEB_READER", "web_reader/providers", false)) {
		up := strings.ToUpper(p)
		backends = append(backends, Backend{
			Provider: p,
			URL:      get("WEB_READER_"+up+"_URL", "web_reader/"+p+"_url", false),
			Key:      get("WEB_READER_"+up+"_API_KEY", "web_reader/"+p+"_api_key", true),
		})
	}
	return New(backends)
}

func splitList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func build(b Backend) Reader {
	base := strings.TrimRight(strings.TrimSpace(b.URL), "/")
	key := strings.TrimSpace(b.Key)
	switch strings.ToLower(strings.TrimSpace(b.Provider)) {
	case providerJina:
		if base == "" {
			base = defaultJina
		}
		return &jina{base: base, key: key, http: readerClient(base)}
	case "crawl4ai":
		if base == "" {
			return nil
		}
		return &crawl4ai{base: base, key: key, http: readerClient(base)}
	case "ddgs":
		if base == "" {
			return nil
		}
		return &ddgs{base: base, http: readerClient(base)}
	case "custom":
		if !strings.Contains(base, "{url}") && !strings.Contains(base, "{url_encoded}") {
			return nil
		}
		return &custom{template: base, key: key, http: readerClient(base)}
	}
	return nil
}

type multi []Reader

func chain(rs []Reader) Reader { return multi(rs) }

func (m multi) Name() string {
	names := make([]string, len(m))
	for i, r := range m {
		names[i] = r.Name()
	}
	return strings.Join(names, ",")
}

func (m multi) Read(ctx context.Context, target string) (*Page, error) {
	var errs []error
	for _, r := range m {
		if ctx.Err() != nil {
			break
		}
		p, err := r.Read(ctx, target)
		if err == nil {
			return p, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, ctx.Err()
	}
	return nil, errors.Join(errs...)
}

// do sends req and returns the body of a 2xx answer.
func do(c *http.Client, req *http.Request, name string) ([]byte, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: the reader did not answer", name)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, fmt.Errorf("%s: the reader stopped mid-answer", name)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: the reader answered %d", name, resp.StatusCode)
	}
	return body, nil
}

func bearer(req *http.Request, key string) {
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// landed checks where a reader ended up and returns the page.
func landed(ctx context.Context, name, html, final, target string) (*Page, error) {
	if strings.TrimSpace(html) == "" {
		return nil, fmt.Errorf("%s: the reader returned an empty page", name)
	}
	if final == "" {
		final = target
	}
	if err := publicURL(ctx, final); err != nil {
		return nil, err
	}
	return &Page{HTML: html, URL: final, Via: name}, nil
}

// lookupIP resolves a host for publicURL; tests replace it.
var lookupIP = net.DefaultResolver.LookupIPAddr

// publicURL refuses a final address a caller could not have fetched itself.
func publicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ErrNotPublic
	}
	if config.UnsafeWebhookURLsAllowed() {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if safehttp.IsBlockedIP(ip) {
			return ErrNotPublic
		}
		return nil
	}
	if h := strings.ToLower(host); h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") {
		return ErrNotPublic
	}
	lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := lookupIP(lctx, host)
	if err != nil || len(addrs) == 0 {
		return ErrNotPublic
	}
	for _, a := range addrs {
		if safehttp.IsBlockedIP(a.IP) {
			return ErrNotPublic
		}
	}
	return nil
}

// jina reads through Jina Reader (r.jina.ai or a self-hosted copy).
type jina struct {
	base string
	key  string
	http *http.Client
}

func (j *jina) Name() string { return providerJina }

func (j *jina) Read(ctx context.Context, target string) (*Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.base+"/"+target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Return-Format", "html")
	bearer(req, j.key)
	body, err := do(j.http, req, j.Name())
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			HTML     string `json:"html"`
			Content  string `json:"content"`
			URL      string `json:"url"`
			FinalURL string `json:"finalUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, errors.New("jina: the reader's answer could not be read")
	}
	if out.Code != 0 && out.Code != http.StatusOK {
		return nil, fmt.Errorf("jina: the reader answered %d", out.Code)
	}
	html := firstNonEmpty(out.Data.HTML, out.Data.Content)
	return landed(ctx, j.Name(), html, firstNonEmpty(out.Data.FinalURL, out.Data.URL), target)
}

// crawl4ai reads through a Crawl4AI server's /crawl endpoint.
type crawl4ai struct {
	base string
	key  string
	http *http.Client
}

func (c *crawl4ai) Name() string { return "crawl4ai" }

func (c *crawl4ai) Read(ctx context.Context, target string) (*Page, error) {
	payload, _ := json.Marshal(map[string]any{"urls": []string{target}, "browser_config": map[string]any{}, "crawler_config": map[string]any{}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/crawl", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	bearer(req, c.key)
	body, err := do(c.http, req, c.Name())
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []struct {
			Success     bool   `json:"success"`
			HTML        string `json:"html"`
			CleanedHTML string `json:"cleaned_html"`
			URL         string `json:"url"`
			Redirected  string `json:"redirected_url"`
			StatusCode  int    `json:"status_code"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Results) == 0 {
		return nil, errors.New("crawl4ai: the reader's answer could not be read")
	}
	r := out.Results[0]
	if !r.Success || r.StatusCode >= 400 {
		return nil, fmt.Errorf("crawl4ai: the site answered the reader %d", r.StatusCode)
	}
	return landed(ctx, c.Name(), firstNonEmpty(r.HTML, r.CleanedHTML), firstNonEmpty(r.Redirected, r.URL), target)
}

// ddgs reads through a DDGS API server's /extract endpoint.
type ddgs struct {
	base string
	http *http.Client
}

func (d *ddgs) Name() string { return "ddgs" }

func (d *ddgs) Read(ctx context.Context, target string) (*Page, error) {
	q := url.Values{"url": {target}, "fmt": {"text"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/extract?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	body, err := do(d.http, req, d.Name())
	if err != nil {
		return nil, err
	}
	var out struct {
		URL     string `json:"url"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, errors.New("ddgs: the reader's answer could not be read")
	}
	return landed(ctx, d.Name(), out.Content, out.URL, target)
}

// custom reads through any endpoint built from a template holding {url} (the
// page address as is) or {url_encoded} (query-escaped). It may answer with the
// HTML itself or JSON carrying it.
type custom struct {
	template string
	key      string
	http     *http.Client
}

func (c *custom) Name() string { return "custom" }

func (c *custom) Read(ctx context.Context, target string) (*Page, error) {
	endpoint := strings.NewReplacer("{url_encoded}", url.QueryEscape(target), "{url}", target).Replace(c.template)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.5")
	bearer(req, c.key)
	body, err := do(c.http, req, c.Name())
	if err != nil {
		return nil, err
	}
	html, final := string(body), ""
	if trimmed := strings.TrimSpace(html); strings.HasPrefix(trimmed, "{") {
		html, final = fromJSON(body)
	}
	return landed(ctx, c.Name(), html, final, target)
}

// fromJSON pulls a page out of the common reader answer shapes.
func fromJSON(body []byte) (string, string) {
	var out struct {
		HTML     string `json:"html"`
		Content  string `json:"content"`
		URL      string `json:"url"`
		FinalURL string `json:"final_url"`
		Data     *struct {
			HTML     string `json:"html"`
			RawHTML  string `json:"rawHtml"`
			Content  string `json:"content"`
			URL      string `json:"url"`
			FinalURL string `json:"finalUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", ""
	}
	if out.Data != nil {
		if h := firstNonEmpty(out.Data.RawHTML, out.Data.HTML, out.Data.Content); h != "" {
			return h, firstNonEmpty(out.Data.FinalURL, out.Data.URL)
		}
	}
	return firstNonEmpty(out.HTML, out.Content), firstNonEmpty(out.FinalURL, out.URL)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
