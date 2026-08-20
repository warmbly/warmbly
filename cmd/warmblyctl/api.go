package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/apikey"
)

// apiClient talks to a Warmbly instance's public REST API with an API key.
// It is how the CLI drives a running instance, including a hosted one, where
// the database commands cannot reach. The DB commands stay for recovery; this
// path is for day-to-day operation, which makes it the surface agents script.
type apiClient struct {
	base  string
	key   string
	http  *http.Client
	debug bool
}

// apiEndpoint resolves the API base URL an agent most plausibly means:
// an explicit WARMBLY_API_URL, then the instance's own API_PUBLIC_URL when the
// command runs where the backend's environment is present, then the hosted
// service.
func apiEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("WARMBLY_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("API_PUBLIC_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.warmbly.com"
}

func newAPIClient() (*apiClient, error) {
	key := strings.TrimSpace(os.Getenv("WARMBLY_API_KEY"))
	if key == "" {
		return nil, errors.New("WARMBLY_API_KEY is not set, so there is no key to call the API with.\nCreate one under Settings > API keys (or `warmblyctl api keys` on an instance you can already reach), then:\n  export WARMBLY_API_KEY=wmbly_...\n  export WARMBLY_API_URL=https://api.your-instance.com   # omit for the hosted service")
	}
	if !strings.HasPrefix(key, apikey.KeyPrefix) {
		return nil, fmt.Errorf("WARMBLY_API_KEY does not look like a Warmbly API key: it should start with %q.", apikey.KeyPrefix)
	}
	return &apiClient{
		base:  apiEndpoint(),
		key:   key,
		http:  &http.Client{Timeout: 60 * time.Second},
		debug: os.Getenv("WARMBLYCTL_DEBUG") != "",
	}, nil
}

// apiError is the backend's stable error envelope. Every field is part of the
// public contract, so they are safe to surface verbatim.
type apiError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

// do performs one request. path is relative to /v1 unless it already names a
// version. A non-2xx response comes back as an error carrying the backend's
// own code and request id, so an agent can branch without parsing prose.
func (c *apiClient) do(ctx context.Context, method, path string, query url.Values, body any, idempotencyKey string) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasPrefix(path, "/v1/") && path != "/v1" {
		path = "/v1" + path
	}
	full := c.base + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		switch b := body.(type) {
		case json.RawMessage:
			reader = bytes.NewReader(b)
		case []byte:
			reader = bytes.NewReader(b)
		default:
			buf, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("encoding the request body: %w", err)
			}
			reader = bytes.NewReader(buf)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	if c.debug {
		fmt.Fprintf(os.Stderr, "> %s %s\n", method, full)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the API at %s: %w\nSet WARMBLY_API_URL to your instance's API base URL, or omit it for the hosted service.", c.base, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the API response: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}

	var apiErr apiError
	if json.Unmarshal(payload, &apiErr) == nil && (apiErr.Message != "" || apiErr.Code != "") {
		msg := apiErr.Message
		if msg == "" {
			msg = apiErr.Error
		}
		detail := fmt.Sprintf("%s %s failed (%d %s): %s", method, path, resp.StatusCode, apiErr.Code, msg)
		if apiErr.RequestID != "" {
			detail += " (request " + apiErr.RequestID + ")"
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if retry := resp.Header.Get("Retry-After"); retry != "" {
				detail += ". Rate limited; retry after " + retry + "s."
			}
		}
		return nil, errors.New(detail)
	}
	return nil, fmt.Errorf("%s %s failed with HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
}

// printJSON writes the API's response for a human and a parser alike: the
// payload is already JSON, so it is re-indented and passed through untouched.
func printJSON(payload json.RawMessage) error {
	if len(bytes.TrimSpace(payload)) == 0 {
		fmt.Println("{}")
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, payload, "", "  "); err != nil {
		// Not JSON (some endpoints stream files); pass it through as-is.
		_, werr := os.Stdout.Write(payload)
		if werr == nil {
			fmt.Println()
		}
		return werr
	}
	fmt.Println(buf.String())
	return nil
}

// readBodyArg turns a --data value into a request body: a JSON literal, `-`
// for stdin, or @path for a file, the same conventions curl taught everyone.
func readBodyArg(data string) (json.RawMessage, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}
	var raw []byte
	switch {
	case data == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading the body from stdin: %w", err)
		}
		raw = b
	case strings.HasPrefix(data, "@"):
		b, err := os.ReadFile(strings.TrimPrefix(data, "@"))
		if err != nil {
			return nil, fmt.Errorf("reading the body file: %w", err)
		}
		raw = b
	default:
		raw = []byte(data)
	}
	if !json.Valid(raw) {
		return nil, errors.New("the request body is not valid JSON. Pass a JSON literal, `-` for stdin, or @file.")
	}
	return json.RawMessage(raw), nil
}

// runAPI is the raw passthrough: any method, any path, so nothing the API can
// do is out of the CLI's reach even before a typed command exists for it.
func runAPI(ctx context.Context, args []string) error {
	if len(args) == 0 {
		apiUsage(os.Stderr)
		return errors.New("`api` needs a method and a path. Pick a form from the list above.")
	}

	method := strings.ToUpper(args[0])
	switch method {
	case "HELP", "-H", "--HELP":
		apiUsage(os.Stdout)
		return nil
	case "GET", "POST", "PATCH", "PUT", "DELETE":
	default:
		apiUsage(os.Stderr)
		return fmt.Errorf("unknown method %q. Use get, post, patch, put or delete.", args[0])
	}

	fs := newFlagSet("api")
	data := fs.String("data", "", "JSON request body: a literal, `-` for stdin, or @file")
	idem := fs.String("idempotency-key", "", "Idempotency-Key header for a safely retryable write")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("missing the request path, for example `warmblyctl api get /campaigns`.")
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("unexpected argument %q. Put query parameters in the path itself: /campaigns?limit=10", fs.Arg(1))
	}

	rawPath := fs.Arg(0)
	parsed, err := url.Parse(rawPath)
	if err != nil {
		return fmt.Errorf("%q is not a usable path: %w", rawPath, err)
	}

	body, err := readBodyArg(*data)
	if err != nil {
		return err
	}
	if body != nil && method == "GET" {
		return errors.New("a GET request carries no body. Put parameters in the query string instead.")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}

	payload, err := client.do(ctx, method, parsed.Path, parsed.Query(), body, *idem)
	if err != nil {
		return err
	}
	return printJSON(payload)
}

func apiUsage(w *os.File) {
	fmt.Fprint(w, `Call the public REST API of a Warmbly instance directly.

Usage:
  warmblyctl api <get|post|patch|put|delete> <path> [--data JSON] [--idempotency-key KEY]

Paths are relative to /v1. Examples:
  warmblyctl api get "/campaigns?limit=10"
  warmblyctl api post /contacts --data '{"email":"jane@example.com"}'
  warmblyctl api patch /campaigns/<id> --data @changes.json
  warmblyctl api delete /webhooks/<id>

Environment:
  WARMBLY_API_KEY   The API key (starts with wmbly_). Required.
  WARMBLY_API_URL   API base URL. Defaults to the instance's own API_PUBLIC_URL
                    when run inside the backend, then https://api.warmbly.com.

The response body is printed to stdout as JSON. A non-2xx response exits 1 and
prints the API's machine-readable error code and request id to stderr.

The typed commands (campaign, contact, mailbox, inbox, ...) cover the common
operations with flags; this passthrough covers everything else.
`)
}
