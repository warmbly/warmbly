package formserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/formwire"
)

var errFormNotFound = errors.New("formserver: form not found")

const (
	// Short positive TTL: an unpublish takes effect within it, and one busy
	// form costs the backend at most a few requests a minute.
	formCacheTTL    = 15 * time.Second
	formNegCacheTTL = 30 * time.Second
	// formCacheMax bounds the map against public-id spray; real instances
	// hold a few hundred forms at most.
	formCacheMax = 4096
)

// backendClient is the forms service's only dependency: the backend's
// internal API, authenticated with the same bearer token the workers and the
// tracking service use.
type backendClient struct {
	base  string
	token string
	http  *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// cacheEntry with a nil form is a negative entry: the id is known missing.
type cacheEntry struct {
	form    *formwire.PublicForm
	expires time.Time
}

// newBackendClient trusts only roots for the backend when it is set, else the system roots.
func newBackendClient(base, token string, roots *x509.CertPool) *backendClient {
	client := &http.Client{Timeout: 10 * time.Second}
	if roots != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		client.Transport = transport
	}
	return &backendClient{
		base:  base,
		token: token,
		http:  client,
		cache: map[string]cacheEntry{},
	}
}

func (b *backendClient) formURL(publicID, suffix string) string {
	return b.base + "/api/v1/internal/forms/" + url.PathEscape(publicID) + suffix
}

func (b *backendClient) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.http.Do(req)
}

func (b *backendClient) cached(publicID string) (cacheEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.cache[publicID]
	if !ok || time.Now().After(e.expires) {
		return cacheEntry{}, false
	}
	return e, true
}

func (b *backendClient) store(publicID string, f *formwire.PublicForm, ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.cache) >= formCacheMax {
		now := time.Now()
		for k, e := range b.cache {
			if now.After(e.expires) {
				delete(b.cache, k)
			}
		}
		// Still full after sweeping means an id spray; dropping the window
		// costs a few extra backend hits, holding it costs unbounded memory.
		if len(b.cache) >= formCacheMax {
			b.cache = map[string]cacheEntry{}
		}
	}
	b.cache[publicID] = cacheEntry{form: f, expires: time.Now().Add(ttl)}
}

// cacheKey scopes a cached answer to the host it was given for, since a custom host answers only for its workspace's forms.
func cacheKey(host, publicID string) string { return host + "\x00" + publicID }

// Form resolves a published form as seen on host, TTL-cached. errFormNotFound
// for unknown or unpublished ids and for a form another workspace's custom
// host does not serve; any other error means the backend was unreachable.
func (b *backendClient) Form(ctx context.Context, publicID, host string) (*formwire.PublicForm, error) {
	key := cacheKey(host, publicID)
	if e, ok := b.cached(key); ok {
		if e.form == nil {
			return nil, errFormNotFound
		}
		return e.form, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.formURL(publicID, "?host="+url.QueryEscape(host)), nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var f formwire.PublicForm
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&f); err != nil {
			return nil, err
		}
		b.store(key, &f, formCacheTTL)
		return &f, nil
	case http.StatusNotFound:
		b.store(key, nil, formNegCacheTTL)
		return nil, errFormNotFound
	default:
		return nil, fmt.Errorf("formserver: backend returned %d for form fetch", resp.StatusCode)
	}
}

// submitOutcome is the backend's verdict on one submission. Exactly one of
// Result, Rejected or NotFound is meaningful.
type submitOutcome struct {
	Result   *formwire.SubmitResult
	Rejected string
	NotFound bool
}

// Submit forwards a visitor's answers. An error return means the backend was
// unreachable, never that the submission was invalid.
func (b *backendClient) Submit(ctx context.Context, publicID string, in *formwire.SubmitRequest) (*submitOutcome, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.formURL(publicID, "/submissions"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var res formwire.SubmitResult
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&res); err != nil {
			return nil, err
		}
		return &submitOutcome{Result: &res}, nil
	case http.StatusNotFound:
		return &submitOutcome{NotFound: true}, nil
	case http.StatusBadRequest:
		var rej formwire.SubmitError
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rej); err != nil || rej.Message == "" {
			rej.Message = "The submission could not be processed."
		}
		return &submitOutcome{Rejected: rej.Message}, nil
	default:
		return nil, fmt.Errorf("formserver: backend returned %d for submit", resp.StatusCode)
	}
}

// FormWithToken resolves a form together with its ?t= prefill. Deliberately
// uncached: per-contact prefill must never be served to another visitor, and
// the render-token gate plus the event budget bound the extra backend hits.
func (b *backendClient) FormWithToken(ctx context.Context, publicID, linkToken, host string) (*formwire.PublicForm, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.formURL(publicID, "?t="+url.QueryEscape(linkToken)+"&host="+url.QueryEscape(host)), nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var f formwire.PublicForm
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&f); err != nil {
			return nil, err
		}
		return &f, nil
	case http.StatusNotFound:
		b.store(cacheKey(host, publicID), nil, formNegCacheTTL)
		return nil, errFormNotFound
	default:
		return nil, fmt.Errorf("formserver: backend returned %d for form fetch", resp.StatusCode)
	}
}

// RecordEvent forwards one funnel event; best-effort, the page never waits.
func (b *backendClient) RecordEvent(publicID string, ev *formwire.EventRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.formURL(publicID, "/events"), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
