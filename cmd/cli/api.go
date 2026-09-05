package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/output"
)

// newAPICmd is the escape hatch that makes the typed commands optional: every
// endpoint is reachable on day one, whether or not a noun-verb command for it
// exists yet.
func newAPICmd(f *Factory) *cobra.Command {
	var (
		method   string
		rawField []string
		field    []string
		headers  []string
		input    string
		paginate bool
		maxPages int
		include  bool
		silent   bool
		idemKey  string
	)

	cmd := &cobra.Command{
		Use:     "api <endpoint>",
		Short:   "Call any Warmbly API endpoint",
		GroupID: groupDevelop,
		Long: `Make an authenticated request to the Warmbly REST API.

The endpoint is relative to /v1 unless it already names a version, so
"/campaigns" and "/v1/campaigns" are the same call. The method defaults to GET,
or POST when any field is supplied.

Fields build a JSON body. -f keeps the value a string; -F guesses the type, so
true, false, null and numbers arrive as themselves, and @file or @- reads a
value from a file or stdin. Nested keys use key[sub]=value and repeated key[]
builds an array.`,
		Example: `  $ warmbly api /me
  $ warmbly api "/campaigns?limit=10" --paginate
  $ warmbly api /contacts -f email=jane@example.com -f first_name=Jane
  $ warmbly api /campaigns/CAMPAIGN_ID -X PATCH -F daily_limit=40
  $ warmbly api /contacts/search -X POST --input filter.json
  $ warmbly api /webhooks/WEBHOOK_ID -X DELETE`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			client, err := f.Client()
			if err != nil {
				return err
			}

			parsed, err := url.Parse(args[0])
			if err != nil {
				return fmt.Errorf("%q is not a usable endpoint: %w", args[0], err)
			}

			body, err := bodyFromArg(f.IO.In, input)
			if err != nil {
				return err
			}
			fields, err := buildFields(rawField, field, f.IO.In)
			if err != nil {
				return err
			}
			if len(fields) > 0 {
				if body != nil {
					return fmt.Errorf("pass fields or --input, not both")
				}
				body, err = json.Marshal(fields)
				if err != nil {
					return err
				}
			}
			if body != nil && !json.Valid(body) {
				return fmt.Errorf("the request body is not valid JSON")
			}

			if method == "" {
				method = http.MethodGet
				if body != nil {
					method = http.MethodPost
				}
			}
			method = strings.ToUpper(method)
			if method == http.MethodGet && body != nil {
				return fmt.Errorf("a GET request carries no body. Put parameters in the query string, or pass -X POST.")
			}

			req := api.Request{
				Method:         method,
				Path:           parsed.Path,
				Query:          parsed.Query(),
				Body:           body,
				IdempotencyKey: idemKey,
				Headers:        map[string]string{},
			}
			for _, h := range headers {
				name, value, ok := strings.Cut(h, ":")
				if !ok {
					return fmt.Errorf("headers are name:value, not %q", h)
				}
				req.Headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
			}

			if paginate {
				if method != http.MethodGet {
					return fmt.Errorf("--paginate only makes sense on a GET")
				}
				merged, perr := client.Paginate(c.Context(), req, maxPages)
				if perr != nil {
					return perr
				}
				if silent {
					return nil
				}
				return (&output.Printer{IO: f.IO, JSON: true}).Print(merged, output.Table{})
			}

			resp, err := client.Do(c.Context(), req)
			if resp != nil && include {
				f.IO.Printf("HTTP %d\n", resp.Status)
				for name, values := range resp.Header {
					for _, v := range values {
						f.IO.Printf("%s: %s\n", name, v)
					}
				}
				f.IO.Println()
			}
			if err != nil {
				return err
			}
			if silent {
				return nil
			}
			return (&output.Printer{IO: f.IO, JSON: true, Template: f.Template}).Print(resp.Body, output.Table{})
		},
	}

	cmd.Flags().StringVarP(&method, "method", "X", "", "HTTP method (default GET, or POST when fields are given)")
	cmd.Flags().StringArrayVarP(&rawField, "raw-field", "f", nil, "Body field as a string: key=value")
	cmd.Flags().StringArrayVarP(&field, "field", "F", nil, "Body field with a guessed type: key=value, key=@file")
	cmd.Flags().StringArrayVarP(&headers, "header", "H", nil, "Extra request header: name:value")
	cmd.Flags().StringVar(&input, "input", "", "Request body: JSON, @file, or - for stdin")
	cmd.Flags().BoolVar(&paginate, "paginate", false, "Follow the cursor and merge every page")
	cmd.Flags().IntVar(&maxPages, "max-pages", 100, "Stop after this many pages")
	cmd.Flags().BoolVarP(&include, "include", "i", false, "Print the status and response headers too")
	cmd.Flags().BoolVar(&silent, "silent", false, "Do not print the response body")
	cmd.Flags().StringVar(&idemKey, "idempotency-key", "", "Idempotency-Key header for a safely retryable write")
	return cmd
}

// buildFields turns -f and -F into one JSON object. Order matters only for
// duplicate keys, where the last one wins, as in curl.
func buildFields(raw, typed []string, stdin io.Reader) (map[string]any, error) {
	out := map[string]any{}
	for _, kv := range raw {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("fields are key=value, not %q", kv)
		}
		if err := assign(out, key, value); err != nil {
			return nil, err
		}
	}
	for _, kv := range typed {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("fields are key=value, not %q", kv)
		}
		if strings.HasPrefix(value, "@") {
			source := value
			if value == "@-" {
				source = "-"
			}
			data, err := bodyFromArg(stdin, source)
			if err != nil {
				return nil, err
			}
			if err := assign(out, key, strings.TrimRight(string(data), "\n")); err != nil {
				return nil, err
			}
			continue
		}
		if err := assign(out, key, guessType(value)); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// guessType is the -F conversion: JSON literals become themselves, everything
// else stays a string.
func guessType(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	if fl, err := strconv.ParseFloat(v, 64); err == nil {
		return fl
	}
	return v
}

// assign writes one field, understanding key[sub] for nesting and key[] for
// appending to an array.
func assign(obj map[string]any, key string, value any) error {
	open := strings.Index(key, "[")
	if open < 0 {
		obj[key] = value
		return nil
	}
	if !strings.HasSuffix(key, "]") {
		return fmt.Errorf("unbalanced brackets in field %q", key)
	}
	head := key[:open]
	inner := key[open+1 : len(key)-1]
	if head == "" {
		return fmt.Errorf("field %q has no name", key)
	}
	if inner == "" {
		existing, _ := obj[head].([]any)
		obj[head] = append(existing, value)
		return nil
	}
	nested, ok := obj[head].(map[string]any)
	if !ok {
		nested = map[string]any{}
		obj[head] = nested
	}
	return assign(nested, inner, value)
}
