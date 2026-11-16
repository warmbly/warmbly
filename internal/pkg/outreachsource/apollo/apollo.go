// Package apollo reads an Apollo team's sequences, contacts and linked
// mailboxes through Apollo's REST API.
package apollo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	defaultBase = "https://api.apollo.io/api/v1"
	label       = "Apollo"
	// maxPages is Apollo's display limit on searches: 500 pages of 100.
	maxPages = 500
)

// pageSize is Apollo's largest search page; tests shrink it.
var pageSize = 100

type provider struct {
	base  string
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Apollo provider.
func New() outreachsource.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() outreachsource.Info {
	return outreachsource.Info{
		ID:    "apollo",
		Label: label,
		Fields: []outreachsource.Field{{
			Key:         "api_key",
			Label:       "API key",
			Help:        "A master key, or a key allowed to search sequences, contacts, outreach emails, email accounts, schedules and custom fields.",
			Secret:      true,
			Placeholder: "Apollo API key",
		}},
		KeyHelp: "In Apollo, open Settings, then Integrations, then API Keys, and create a master key (or a scoped key with the sequence, contact and email account endpoints).",
		KeyURL:  "https://developer.apollo.io/#/keys",
		Kinds: []outreachsource.Kind{
			outreachsource.KindMailboxes, outreachsource.KindCampaigns, outreachsource.KindLeads,
		},
		Notes: []string{
			"Apollo's API does not return a sequence's steps or email copy, so sequences come over with their settings and their emails are written again in Warmbly.",
			"Apollo mailboxes are linked Google or Microsoft accounts, so each one is connected again in Warmbly.",
			"Apollo has no readable blocklist. Contacts marked unsubscribed in Apollo come over unsubscribed.",
			"Apollo's contact search stops at 50,000 contacts, so a larger account brings over the first 50,000.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds outreachsource.Credentials) (outreachsource.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, outreachsource.ErrKey)
	}
	c := outreachsource.NewClient(label, p.base, func(r *http.Request) {
		r.Header.Set("x-api-key", key)
		r.Header.Set("Cache-Control", "no-cache")
	})
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	var me profile
	if err := c.Get(ctx, "/users/api_profile", nil, &me); err != nil {
		return nil, err
	}
	return &session{c: c, me: me}, nil
}

type profile struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type session struct {
	c  *outreachsource.Client
	me profile
	// fields maps custom field ids to their names, loaded once.
	fields map[string]customField
}

func (s *session) name() string {
	n := strings.TrimSpace(s.me.FirstName + " " + s.me.LastName)
	if n == "" {
		n = label
	}
	return n
}

func (s *session) Account(context.Context) (outreachsource.Account, error) {
	return outreachsource.Account{Name: s.name(), Email: strings.TrimSpace(s.me.Email)}, nil
}

// Workspaces is the one team the key belongs to.
func (s *session) Workspaces(context.Context) ([]outreachsource.Workspace, error) {
	return []outreachsource.Workspace{{ID: "", Name: s.name()}}, nil
}

func (s *session) Inventory(ctx context.Context, _ outreachsource.Workspace) (outreachsource.Inventory, error) {
	inv := outreachsource.Inventory{Blocklist: -1}
	accounts, err := s.emailAccounts(ctx)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(accounts)
	seqs, err := s.sequences(ctx)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(seqs)
	for _, q := range seqs {
		inv.Summaries = append(inv.Summaries, outreachsource.CampaignSummary{
			ID: q.ID, Name: q.Name, Status: q.status(), Steps: q.NumSteps, Leads: -1,
		})
	}
	var page contactPage
	if err := s.c.Post(ctx, "/contacts/search", nil, map[string]any{"page": 1, "per_page": 1}, &page); err != nil {
		return inv, err
	}
	inv.Leads = page.Pagination.TotalEntries
	return inv, nil
}

type emailAccount struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Type           string `json:"type"`
	DailyThreshold *int   `json:"email_daily_threshold"`
	SignatureHTML  string `json:"signature_html"`
	TrueWarmup     *bool  `json:"true_warmup_enabled"`
}

func (s *session) emailAccounts(ctx context.Context) ([]emailAccount, error) {
	var out struct {
		EmailAccounts []emailAccount `json:"email_accounts"`
	}
	if err := s.c.Get(ctx, "/email_accounts", nil, &out); err != nil {
		return nil, err
	}
	return out.EmailAccounts, nil
}

// mailboxKind reads Apollo's provider type; anything not Google or Microsoft
// is treated as SMTP and asked for again.
func mailboxKind(t string) outreachsource.MailboxKind {
	t = strings.ToLower(t)
	switch {
	case strings.Contains(t, "gmail"), strings.Contains(t, "google"):
		return outreachsource.MailboxGoogle
	case strings.Contains(t, "outlook"), strings.Contains(t, "office"), strings.Contains(t, "microsoft"), strings.Contains(t, "exchange"):
		return outreachsource.MailboxMicrosoft
	}
	return outreachsource.MailboxSMTP
}

func (s *session) Mailboxes(ctx context.Context, _ outreachsource.Workspace) ([]outreachsource.Mailbox, error) {
	accounts, err := s.emailAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]outreachsource.Mailbox, 0, len(accounts))
	for _, a := range accounts {
		email := strings.ToLower(strings.TrimSpace(a.Email))
		if email == "" {
			continue
		}
		m := outreachsource.Mailbox{
			ID:        a.ID,
			Email:     email,
			Kind:      mailboxKind(a.Type),
			Warmup:    a.TrueWarmup,
			Signature: a.SignatureHTML,
		}
		if a.DailyThreshold != nil && *a.DailyThreshold > 0 {
			m.DailyCap = *a.DailyThreshold
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *session) Blocklist(context.Context, outreachsource.Workspace, string) ([]outreachsource.BlockEntry, string, error) {
	return nil, "", fmt.Errorf("%s: blocklist: %w", label, outreachsource.ErrUnsupported)
}

// text decodes a JSON string and reads anything else (null, an object) as empty.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = text(strings.TrimSpace(s))
	} else {
		*t = ""
	}
	return nil
}

// truthy decodes a flag Apollo may send as a boolean, a string or null.
type truthy bool

func (t *truthy) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case bool:
		*t = truthy(x)
	case string:
		*t = truthy(strings.EqualFold(x, "true"))
	default:
		*t = false
	}
	return nil
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999-07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// optional runs a best-effort read that a scoped key may not be allowed.
func optional(err error) error {
	if errors.Is(err, outreachsource.ErrForbidden) {
		return nil
	}
	var ae *outreachsource.APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusUnprocessableEntity) {
		return nil
	}
	return err
}
