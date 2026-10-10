package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type remoteError struct {
	Exit      int    `json:"-"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Status    int    `json:"status,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Retry     int    `json:"retry_after_seconds,omitempty"`
}

func (e *remoteError) Error() string { return e.Message }

func remoteFailure(exit int, code, message string) *remoteError {
	return &remoteError{Exit: exit, Code: code, Message: message}
}

var remoteSafeCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)
var remoteSafeID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)
var remoteProfileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func normalizeRemoteURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", remoteFailure(2, "invalid_url", "--url must be a bare HTTPS backend origin without credentials, path, query or fragment.")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return "", remoteFailure(2, "invalid_url", "Remote credentials require HTTPS. HTTP is allowed only for loopback development.")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", remoteFailure(2, "invalid_url", "The backend port must be between 1 and 65535.")
		}
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			port = ""
		}
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = ""
	return u.String(), nil
}

type remoteSelection struct {
	URL    string
	Server string
}

func (s *remoteSelection) flags(fs *flag.FlagSet) {
	fs.StringVar(&s.URL, "url", "", "Explicit backend origin, for example https://api.example.com")
	fs.StringVar(&s.Server, "server", "", "Named isolated profile registered by login --server NAME --url URL")
}

func (s remoteSelection) resolve(store *remoteStore) (string, string, error) {
	if s.URL == "" && s.Server == "" {
		return "", "", remoteFailure(2, "server_required", "Select a backend explicitly with --url or --server. No default server or environment credential is used.")
	}
	profile := s.Server
	if profile == "" {
		profile = "default"
	}
	if !remoteProfileName.MatchString(profile) {
		return "", "", remoteFailure(2, "invalid_profile", "Profile names use 1 to 64 letters, digits, underscores or hyphens.")
	}
	base := s.URL
	if base == "" {
		base = store.state.Servers[profile]
		if base == "" {
			return "", "", remoteFailure(2, "unknown_server", "Register this profile with login --server NAME --url URL first.")
		}
	}
	base, err := normalizeRemoteURL(base)
	if err != nil {
		return "", "", err
	}
	if registered := store.state.Servers[profile]; s.Server != "" && registered != "" && registered != base {
		return "", "", remoteFailure(2, "server_mismatch", "This profile belongs to another backend. Select a different profile name; credentials never move between instances.")
	}
	return base, profile, nil
}

func newRemoteFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: warmblyctl %s [flags]\n", name)
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	return fs
}

func parseRemoteFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return remoteFailure(2, "invalid_arguments", "Invalid flags. Use --help; do not pass passwords or tokens on the command line.")
	}
	if fs.NArg() != 0 {
		return remoteFailure(2, "invalid_arguments", "Unexpected positional arguments. Values must use the documented flags.")
	}
	return nil
}

func remoteHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

type remoteClient struct {
	base       string
	profile    string
	key        string
	credential remoteCredential
	store      *remoteStore
	http       *http.Client
	now        func() time.Time
}

func newRemoteClient(ctx context.Context, sel remoteSelection) (*remoteClient, error) {
	path, err := remoteStorePath()
	if err != nil {
		return nil, err
	}
	store, err := openRemoteStore(ctx, path)
	if err != nil {
		return nil, err
	}
	base, profile, err := sel.resolve(store)
	if err != nil {
		store.close()
		return nil, err
	}
	key := remoteCredentialKey(base, profile)
	cred, ok := store.state.Credentials[key]
	if !ok || cred.URL != base {
		store.close()
		return nil, remoteFailure(4, "login_required", "No session for this instance and profile. Run login with the same selection.")
	}
	return &remoteClient{base: base, profile: profile, key: key, credential: cred, store: store, http: remoteHTTPClient(), now: time.Now}, nil
}

func remoteRequest(ctx context.Context, client *http.Client, base, method, path, token string, body json.RawMessage) (json.RawMessage, error) {
	// Callers supply only fixed auth paths or validated catalog paths.
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n#\\") {
		return nil, remoteFailure(2, "invalid_path", "Use an allowed relative API path.")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, remoteFailure(2, "invalid_request", "Could not construct the request.")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "warmblyctl")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, remoteFailure(7, "cancelled", "Operation cancelled. A write may have reached the server; inspect its state before another action.")
		}
		return nil, remoteFailure(5, "transport_failed", "Could not complete the request. A write may have reached the server; inspect its state before another action.")
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(payload) > 8<<20 {
		return nil, remoteFailure(5, "invalid_response", "Response unreadable or exceeds the 8 MiB limit. No raw response was printed.")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if resp.StatusCode == http.StatusNoContent && len(bytes.TrimSpace(payload)) == 0 {
			return json.RawMessage(`{}`), nil
		}
		if !json.Valid(payload) {
			return nil, remoteFailure(5, "invalid_response", "The endpoint did not return JSON. No raw response was printed.")
		}
		return payload, nil
	}
	e := remoteFailure(5, "http_error", "The server refused the request. Inspect the code and request ID; no response message or headers are printed.")
	e.Status = resp.StatusCode
	var envelope apiError
	if json.Unmarshal(payload, &envelope) == nil {
		if remoteSafeCode.MatchString(envelope.Code) {
			e.Code = envelope.Code
		}
		if remoteSafeID.MatchString(envelope.RequestID) {
			e.RequestID = envelope.RequestID
		}
	}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		e.Exit = 2
	case http.StatusUnauthorized:
		e.Exit = 4
		e.Message = "Session rejected. Run login again for this profile. No password or MFA bypass is available."
	case http.StatusForbidden:
		e.Exit = 3
		e.Message = "Permission, MFA or fresh authentication required. Refresh does not renew proof; use a new explicit browser-approved login for reauth_required."
	case http.StatusTooManyRequests:
		e.Exit = 6
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 && n <= 3600 {
			e.Retry = n
		}
	}
	return nil, e
}

func (c *remoteClient) forget() error {
	delete(c.store.state.Credentials, c.key)
	c.credential.Token = remoteToken{}
	return c.store.save()
}

func (c *remoteClient) refresh(ctx context.Context) error {
	old := c.credential.Token
	if old.RefreshToken == "" || !old.RefreshTokenExpiresAt.After(c.now()) {
		if err := c.forget(); err != nil {
			return err
		}
		return remoteFailure(4, "login_required", "Refresh session expired. Run login again.")
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": old.RefreshToken})
	payload, err := remoteRequest(ctx, c.http, c.base, http.MethodPost, "/v1/auth/refresh", "", body)
	if err != nil {
		// Rotation may have committed even when its response was lost. Never reuse it.
		if ferr := c.forget(); ferr != nil {
			return ferr
		}
		return err
	}
	var next remoteToken
	if json.Unmarshal(payload, &next) != nil || !next.valid(c.now()) {
		if err := c.forget(); err != nil {
			return err
		}
		return remoteFailure(4, "refresh_unusable", "Rotated credential could not be validated. Run login again; refresh is not replayed.")
	}
	c.credential.Token = next
	c.store.state.Credentials[c.key] = c.credential
	if err := c.store.save(); err != nil {
		_, _ = remoteRequest(ctx, c.http, c.base, http.MethodPost, "/v1/auth/logout", next.AccessToken, nil)
		return err
	}
	return nil
}

func (c *remoteClient) do(ctx context.Context, method, path string, body json.RawMessage) (json.RawMessage, error) {
	refreshed := false
	if c.credential.Token.AccessToken == "" || !c.credential.Token.AccessTokenExpiresAt.After(c.now().Add(30*time.Second)) {
		if err := c.refresh(ctx); err != nil {
			return nil, err
		}
		refreshed = true
	}
	payload, err := remoteRequest(ctx, c.http, c.base, method, path, c.credential.Token.AccessToken, body)
	var e *remoteError
	if !errors.As(err, &e) || e.Status != http.StatusUnauthorized {
		return payload, err
	}
	if method != http.MethodGet || refreshed {
		if ferr := c.forget(); ferr != nil {
			return nil, ferr
		}
		return nil, err // Never automatically repeat writes.
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	payload, err = remoteRequest(ctx, c.http, c.base, method, path, c.credential.Token.AccessToken, body)
	if errors.As(err, &e) && e.Status == http.StatusUnauthorized {
		if ferr := c.forget(); ferr != nil {
			return nil, ferr
		}
	}
	return payload, err
}

func writeRemoteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(value); err != nil {
		return remoteFailure(5, "output_failed", "Could not write JSON output.")
	}
	return nil
}
