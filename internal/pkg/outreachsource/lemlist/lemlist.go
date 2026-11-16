// Package lemlist reads a lemlist team through its public API
// (https://developer.lemlist.com/) into outreachsource types.
package lemlist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	defaultBase = "https://api.lemlist.com/api"
	label       = "lemlist"
	pageSize    = 100
)

type provider struct {
	base string
	// sleep replaces the client's backoff wait in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the lemlist provider.
func New() source.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() source.Info {
	return source.Info{
		ID:    "lemlist",
		Label: "lemlist",
		Fields: []source.Field{{
			Key:    "api_key",
			Label:  "API key",
			Secret: true,
		}},
		KeyHelp:    "In lemlist, open Settings, then Integrations, and generate an API key.",
		KeyURL:     "https://app.lemlist.com/settings/integrations",
		Kinds:      []source.Kind{source.KindBlocklist, source.KindMailboxes, source.KindCampaigns, source.KindLeads},
		Workspaces: false,
		MultiKey:   true,
		Notes: []string{
			"Each lemlist team has its own API key, so an agency adds one key per client team.",
			"Only email steps are brought over. LinkedIn, call, WhatsApp, manual task and condition steps are listed so you can rebuild them.",
			"Mailbox passwords are not available through the lemlist API, so each mailbox is connected again in Warmbly.",
			"Unsubscribed LinkedIn URLs and phone numbers are not brought over, only addresses and domains.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds source.Credentials) (source.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	c := source.NewClient(label, p.base, func(r *http.Request) { r.SetBasicAuth("", key) })
	// A campaign's lead export is one unpaged response.
	c.MaxBody = 256 << 20
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	s := &session{c: c, exports: map[string][]source.Lead{}}
	var t team
	if err := c.Get(ctx, "/team", nil, &t); err != nil {
		return nil, keyErr(err)
	}
	s.team = t
	return s, nil
}

// keyErr reports a refusal of the first call as a bad key, which is what
// lemlist means by a 400 "No API key provided" or a 404 "No user found".
func keyErr(err error) error {
	var ae *source.APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusBadRequest || ae.Status == http.StatusNotFound) {
		return fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	return err
}

type team struct {
	ID   string `json:"_id"`
	Name string `json:"name"`
}

type session struct {
	c    *source.Client
	team team

	mu      sync.Mutex
	exports map[string][]source.Lead
}

func (s *session) Account(context.Context) (source.Account, error) {
	return source.Account{Name: s.team.Name}, nil
}

func (s *session) Workspaces(context.Context) ([]source.Workspace, error) {
	return []source.Workspace{{ID: "", Name: s.team.Name}}, nil
}

func (s *session) Inventory(ctx context.Context, _ source.Workspace) (source.Inventory, error) {
	inv := source.Inventory{Leads: -1, Blocklist: -1}
	camps, err := s.listCampaigns(ctx)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, c := range camps {
		inv.Summaries = append(inv.Summaries, source.CampaignSummary{
			ID: c.ID, Name: c.Name, Status: campaignStatus(c.Status), Steps: -1, Leads: -1,
		})
	}
	senders, err := s.senders(ctx, "")
	if err != nil {
		return inv, err
	}
	for _, sn := range senders {
		for _, ch := range sn.Channels {
			if ch.Type == "email" {
				inv.Mailboxes++
			}
		}
	}
	return inv, nil
}

type senderList struct {
	Data struct {
		Senders []sender `json:"senders"`
	} `json:"data"`
}

type sender struct {
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	InPool   bool      `json:"inPool"`
	Channels []channel `json:"channels"`
}

type channel struct {
	Type    string   `json:"type"`
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Aliases []string `json:"aliases"`
	ESP     string   `json:"esp"`
	InPool  bool     `json:"inPool"`
}

func (s *session) senders(ctx context.Context, campaignID string) ([]sender, error) {
	var q url.Values
	if campaignID != "" {
		q = url.Values{"campaignId": {campaignID}}
	}
	var out senderList
	if err := s.c.Get(ctx, "/senders", q, &out); err != nil {
		return nil, err
	}
	return out.Data.Senders, nil
}

type lemwarm struct {
	Active *bool `json:"active"`
}

func (s *session) Mailboxes(ctx context.Context, _ source.Workspace) ([]source.Mailbox, error) {
	senders, err := s.senders(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []source.Mailbox
	for _, sn := range senders {
		for _, ch := range sn.Channels {
			if ch.Type != "email" {
				continue
			}
			m := source.Mailbox{ID: ch.ID, Email: ch.Label, FromName: sn.Name, Kind: mailboxKind(ch.ESP)}
			// lemwarm is optional per plan; a refusal only leaves warmup unknown.
			var lw lemwarm
			if err := s.c.Get(ctx, "/lemwarm/"+url.PathEscape(ch.ID)+"/settings", nil, &lw); err == nil {
				m.Warmup = lw.Active
			} else if !isRefusal(err) {
				return nil, err
			}
			out = append(out, m)
		}
	}
	return out, nil
}

func mailboxKind(esp string) source.MailboxKind {
	switch esp {
	case "google":
		return source.MailboxGoogle
	case "microsoft":
		return source.MailboxMicrosoft
	}
	return source.MailboxSMTP
}

// isRefusal is a plan or not-found answer that leaves one optional read empty.
func isRefusal(err error) bool {
	var ae *source.APIError
	return errors.As(err, &ae) || errors.Is(err, source.ErrForbidden)
}

func (s *session) Blocklist(ctx context.Context, _ source.Workspace, cursor string) ([]source.BlockEntry, string, error) {
	offset, err := parseOffset(cursor)
	if err != nil {
		return nil, "", err
	}
	q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(pageSize)}}
	var rows []struct {
		Value  string `json:"value"`
		Source string `json:"source"`
	}
	if err := s.c.Get(ctx, "/v2/unsubscribes/variables", q, &rows); err != nil {
		return nil, "", err
	}
	var out []source.BlockEntry
	for _, r := range rows {
		if e, ok := blockEntry(r.Value, r.Source); ok {
			out = append(out, e)
		}
	}
	next := ""
	if len(rows) == pageSize {
		next = strconv.Itoa(offset + pageSize)
	}
	return out, next, nil
}

// blockEntry keeps addresses and domains; LinkedIn URLs and phone numbers share the list.
func blockEntry(value, src string) (source.BlockEntry, bool) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" || strings.ContainsAny(v, "/ ") {
		return source.BlockEntry{}, false
	}
	reason := ""
	switch src {
	case "bounced":
		reason = "bounced"
	case "lead":
		reason = "unsubscribed"
	case "abuse":
		reason = "complaint"
	}
	if at := strings.LastIndex(v, "@"); at > 0 {
		return source.BlockEntry{Value: v, Reason: reason}, true
	}
	d := strings.TrimPrefix(v, "@")
	if !strings.Contains(d, ".") || strings.Trim(d, "+0123456789.-() ") == "" {
		return source.BlockEntry{}, false
	}
	return source.BlockEntry{Value: d, Domain: true, Reason: reason}, true
}

func parseOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: invalid cursor", label)
	}
	return n, nil
}
