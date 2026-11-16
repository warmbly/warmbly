// Package mailshake reads a Mailshake team through its public API
// (https://api-docs.mailshake.com/) into outreachsource types.
package mailshake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// ID is Mailshake's provider id.
const ID outreachsource.ProviderID = "mailshake"

const (
	label       = "Mailshake"
	defaultBase = "https://api.mailshake.com/2017-04-01"
)

var pageSize = 100 // a var so tests can page through small fixtures

type provider struct {
	base  string
	sleep func(context.Context, time.Duration) error
}

// New returns the Mailshake provider.
func New() outreachsource.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() outreachsource.Info {
	return outreachsource.Info{
		ID:    ID,
		Label: label,
		Fields: []outreachsource.Field{{
			Key:    "api_key",
			Label:  "API key",
			Secret: true,
		}},
		KeyHelp:  "In Mailshake, open Extensions, then API, and create a key.",
		KeyURL:   "https://mailshake.com/app/#/redirect/extensions/api",
		Kinds:    []outreachsource.Kind{outreachsource.KindMailboxes, outreachsource.KindCampaigns, outreachsource.KindLeads},
		MultiKey: true,
		Notes: []string{
			"Mailshake's API returns each email's subject but not its body, delay or sending hours, so campaigns need their emails written before they start.",
			"Drip and on-click emails are not brought over.",
			"The unsubscribe list cannot be read through Mailshake's API; export it in Mailshake and upload the CSV.",
			"Senders are connected again in Warmbly.",
			"Mailshake meters its API by the hour, so a large team can take a few hours to read.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds outreachsource.Credentials) (outreachsource.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, outreachsource.ErrKey)
	}
	c := outreachsource.NewClient(label, p.base, func(r *http.Request) { r.SetBasicAuth(key, "") })
	c.HTTP.Transport = errorCodes{}
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	var me struct {
		User user `json:"user"`
	}
	if err := c.Get(ctx, "/me", nil, &me); err != nil {
		return nil, err
	}
	return &session{c: c, me: me.User, progress: map[string]*progress{}}, nil
}

// errorCodes gives Mailshake's documented error codes the HTTP status the
// shared client acts on, whatever status they arrived with.
type errorCodes struct{}

var codeStatus = map[string]int{
	"invalid_api_key":      http.StatusUnauthorized,
	"unauthorized_request": http.StatusUnauthorized,
	"invalid_token":        http.StatusUnauthorized,
	"not_authorized":       http.StatusForbidden,
	"user_not_admin":       http.StatusForbidden,
	"user_is_disabled":     http.StatusForbidden,
	"missing_subscription": http.StatusForbidden,
	"missing_team_admin":   http.StatusForbidden,
	"team_blocked":         http.StatusForbidden,
	"limit_reached":        http.StatusTooManyRequests,
	"internal_error":       http.StatusInternalServerError,
}

func (errorCodes) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil || resp.StatusCode < 400 {
		return resp, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var e struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return resp, nil
	}
	if st, ok := codeStatus[e.Code]; ok {
		resp.StatusCode = st
		resp.Status = strconv.Itoa(st) + " " + http.StatusText(st)
	}
	// limit_reached says when to come back: "...try again after: 2017-08-21T15:16:15.207Z".
	if e.Code == "limit_reached" {
		if i := strings.LastIndex(e.Error, "after:"); i >= 0 {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Error[i+len("after:"):])); err == nil {
				if d := time.Until(t); d > 0 {
					resp.Header.Set("Retry-After", strconv.Itoa(int(math.Ceil(d.Seconds()))))
				}
			}
		}
	}
	return resp, nil
}

type user struct {
	ID           int64  `json:"id"`
	TeamID       int64  `json:"teamID"`
	TeamName     string `json:"teamName"`
	EmailAddress string `json:"emailAddress"`
	FullName     string `json:"fullName"`
}

type session struct {
	c  *outreachsource.Client
	me user

	mu       sync.Mutex
	progress map[string]*progress
}

type page[T any] struct {
	NextToken *string `json:"nextToken"`
	Results   []T     `json:"results"`
}

// list reads one page of a paginated GET endpoint.
func list[T any](ctx context.Context, c *outreachsource.Client, path string, q url.Values, token string) ([]T, string, error) {
	if q == nil {
		q = url.Values{}
	}
	if q.Get("perPage") == "" {
		q.Set("perPage", strconv.Itoa(pageSize))
	}
	if token != "" {
		q.Set("nextToken", token)
	}
	var p page[T]
	if err := c.Get(ctx, path, q, &p); err != nil {
		return nil, "", err
	}
	next := ""
	if p.NextToken != nil && len(p.Results) > 0 {
		next = *p.NextToken
	}
	return p.Results, next, nil
}

// listAll reads every page of a paginated GET endpoint.
func listAll[T any](ctx context.Context, c *outreachsource.Client, path string, q url.Values) ([]T, error) {
	var all []T
	token := ""
	for {
		items, next, err := list[T](ctx, c, path, cloneValues(q), token)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		token = next
	}
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (s *session) Account(context.Context) (outreachsource.Account, error) {
	return outreachsource.Account{Name: s.me.FullName, Email: s.me.EmailAddress}, nil
}

func (s *session) Workspaces(context.Context) ([]outreachsource.Workspace, error) {
	name := s.me.TeamName
	if name == "" {
		name = "Mailshake team"
	}
	return []outreachsource.Workspace{{ID: "", Name: name}}, nil
}

func (s *session) Inventory(ctx context.Context, ws outreachsource.Workspace) (outreachsource.Inventory, error) {
	inv := outreachsource.Inventory{Leads: -1, Blocklist: -1}
	senders, err := listAll[sender](ctx, s.c, "/senders/list", nil)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(senders)
	camps, err := s.campaigns(ctx)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, c := range camps {
		inv.Summaries = append(inv.Summaries, outreachsource.CampaignSummary{
			ID: strconv.FormatInt(c.ID, 10), Name: c.Title, Status: c.status(), Steps: len(c.sequence()), Leads: -1,
		})
	}
	return inv, nil
}

// flexID reads an id Mailshake sends as a number or a string.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flexID(strings.Trim(string(b), `"`))
	return nil
}

type sender struct {
	ID           flexID `json:"id"`
	EmailAddress string `json:"emailAddress"`
	FromName     string `json:"fromName"`
}

func (s *session) Mailboxes(ctx context.Context, _ outreachsource.Workspace) ([]outreachsource.Mailbox, error) {
	senders, err := listAll[sender](ctx, s.c, "/senders/list", nil)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Mailbox, 0, len(senders))
	for _, sd := range senders {
		out = append(out, outreachsource.Mailbox{
			ID:       string(sd.ID),
			Email:    sd.EmailAddress,
			FromName: sd.FromName,
			Kind:     guessKind(sd.EmailAddress),
		})
	}
	return out, nil
}

// guessKind reads the sign-in from a consumer address; Mailshake does not say
// how any other sender connects.
func guessKind(addr string) outreachsource.MailboxKind {
	domain := strings.ToLower(addr[strings.LastIndex(addr, "@")+1:])
	switch domain {
	case "gmail.com", "googlemail.com":
		return outreachsource.MailboxGoogle
	case "outlook.com", "hotmail.com", "live.com", "msn.com":
		return outreachsource.MailboxMicrosoft
	}
	return outreachsource.MailboxUnknown
}

func (s *session) Blocklist(context.Context, outreachsource.Workspace, string) ([]outreachsource.BlockEntry, string, error) {
	return nil, "", fmt.Errorf("%s: the unsubscribe list: %w", label, outreachsource.ErrUnsupported)
}
