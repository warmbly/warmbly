// Package saleshandy reads a Saleshandy account through its Open API
// (https://developer.saleshandy.com/api-reference/introduction).
package saleshandy

import (
	"context"
	"encoding/json"
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
	label       = "Saleshandy"
	defaultBase = "https://open-api.saleshandy.com"
)

// pageSize is how many records one request asks for; tests lower it.
var pageSize = 100

type provider struct {
	base string
	// sleep replaces the retry wait in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Saleshandy provider.
func New() source.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() source.Info {
	return source.Info{
		ID:    "saleshandy",
		Label: label,
		Fields: []source.Field{{
			Key: "api_key", Label: "API key", Secret: true,
		}},
		KeyHelp:    "In Saleshandy, open Settings, then API Keys, and generate a key.",
		KeyURL:     "https://my.saleshandy.com/settings/api-keys",
		Kinds:      []source.Kind{source.KindBlocklist, source.KindMailboxes, source.KindCampaigns, source.KindLeads},
		Workspaces: true,
		Notes: []string{
			"Saleshandy's API does not say which prospects are in a sequence, so prospects come over as contacts and are not enrolled in the imported campaigns.",
			"Prospects belong to the whole Saleshandy account, so they are imported into the account's own workspace, not a client's.",
			"Every do-not-contact list is applied to each workspace.",
			"Mailbox passwords are not available through the API, so each mailbox is connected again.",
			"LinkedIn, call, WhatsApp and task steps are not brought over.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds source.Credentials) (source.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	s := &session{c: source.NewClient(label, p.base, func(r *http.Request) { r.Header.Set("x-api-key", key) })}
	if p.sleep != nil {
		s.c.Sleep = p.sleep
	}
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/v1/sequences", url.Values{"page": {"1"}, "pageSize": {"1"}}, &raw); err != nil {
		return nil, keyError(err)
	}
	return s, nil
}

// keyError reads Saleshandy's "1001: Invalid token", which arrives as a 400, as a rejected key.
func keyError(err error) error {
	var ae *source.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusBadRequest {
		m := strings.ToLower(ae.Message)
		if strings.Contains(m, "token") || strings.Contains(m, "api key") || strings.Contains(m, "apikey") {
			return fmt.Errorf("%s: %w", label, source.ErrKey)
		}
	}
	return err
}

type session struct {
	c *source.Client

	mu        sync.Mutex
	schedules map[string]schedule
	dnc       []dncList
	dncLoaded bool
}

type clientRef struct {
	ID          flexString `json:"id"`
	FirstName   string     `json:"firstName"`
	LastName    string     `json:"lastName"`
	CompanyName string     `json:"companyName"`
}

// in reports whether a record owned by c belongs to ws: the account holds what has no client.
func in(ws source.Workspace, c *clientRef) bool {
	if ws.ID == "" {
		return c == nil || c.ID == ""
	}
	return c != nil && string(c.ID) == ws.ID
}

func (s *session) Account(ctx context.Context) (source.Account, error) {
	return source.Account{Name: label + " account"}, nil
}

func (s *session) Workspaces(ctx context.Context) ([]source.Workspace, error) {
	out := []source.Workspace{{ID: "", Name: label + " account"}}
	for page := 1; ; page++ {
		var raw json.RawMessage
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		if err := s.c.Get(ctx, "/v1/clients", q, &raw); err != nil {
			return nil, err
		}
		var clients []clientRef
		if err := decodeList(raw, &clients, "clients"); err != nil {
			return nil, fmt.Errorf("%s: decode clients: %w", label, err)
		}
		for _, c := range clients {
			name := strings.TrimSpace(c.CompanyName)
			if name == "" {
				name = strings.TrimSpace(c.FirstName + " " + c.LastName)
			}
			if name == "" {
				name = string(c.ID)
			}
			out = append(out, source.Workspace{ID: string(c.ID), Name: name})
		}
		if len(clients) < pageSize {
			return out, nil
		}
	}
}

func (s *session) Inventory(ctx context.Context, ws source.Workspace) (source.Inventory, error) {
	// The API gives no lead total for an account or a client workspace.
	inv := source.Inventory{Leads: -1}
	mbs, err := s.Mailboxes(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(mbs)
	seqs, err := s.sequences(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(seqs)
	for _, sq := range seqs {
		inv.Summaries = append(inv.Summaries, source.CampaignSummary{
			ID: sq.ID, Name: sq.title(), Status: sq.status(), Steps: len(sq.Steps), Leads: s.prospectCount(ctx, sq.ID),
		})
	}
	lists, err := s.dncLists(ctx)
	if err != nil {
		return inv, err
	}
	for _, l := range lists {
		inv.Blocklist += int(l.NumberOfItems)
	}
	return inv, nil
}

type emailAccount struct {
	ID                   flexString `json:"id"`
	FromName             string     `json:"fromName"`
	FromEmail            string     `json:"fromEmail"`
	EmailServiceProvider string     `json:"emailServiceProvider"`
	Client               *clientRef `json:"client"`
}

func (a emailAccount) mailbox() source.Mailbox {
	m := source.Mailbox{ID: string(a.ID), Email: strings.TrimSpace(a.FromEmail), FromName: strings.TrimSpace(a.FromName), Kind: source.MailboxSMTP}
	esp := strings.ToLower(a.EmailServiceProvider)
	switch {
	case strings.Contains(esp, "gsuite"), strings.Contains(esp, "gmail"), strings.Contains(esp, "google"):
		m.Kind = source.MailboxGoogle
	case strings.Contains(esp, "microsoft"), strings.Contains(esp, "outlook"), strings.Contains(esp, "office"), strings.Contains(esp, "hotmail"):
		m.Kind = source.MailboxMicrosoft
	}
	return m
}

func (s *session) Mailboxes(ctx context.Context, ws source.Workspace) ([]source.Mailbox, error) {
	var out []source.Mailbox
	for page := 1; ; page++ {
		body := map[string]any{"page": page, "pageSize": pageSize}
		if ws.ID != "" {
			body["clientIds"] = []string{ws.ID}
		}
		var raw json.RawMessage
		if err := s.c.Post(ctx, "/v1/email-accounts", nil, body, &raw); err != nil {
			return nil, err
		}
		var resp struct {
			Emails []emailAccount `json:"emails"`
			Meta   struct {
				TotalPages flexInt `json:"totalPages"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(unwrap(raw), &resp); err != nil {
			return nil, fmt.Errorf("%s: decode email accounts: %w", label, err)
		}
		for _, a := range resp.Emails {
			if in(ws, a.Client) && a.FromEmail != "" {
				out = append(out, a.mailbox())
			}
		}
		if len(resp.Emails) < pageSize || (resp.Meta.TotalPages > 0 && page >= int(resp.Meta.TotalPages)) {
			return out, nil
		}
	}
}

type prospect struct {
	ID         flexString `json:"id"`
	Email      string     `json:"email"`
	Attributes []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"attributes"`
}

// lead routes each attribute through the template dialect, so a tag and the
// field it reads always land on the same key.
func (p prospect) lead() source.Lead {
	l := source.Lead{ID: string(p.ID), Email: strings.TrimSpace(p.Email), Status: source.LeadNotStarted}
	for _, a := range p.Attributes {
		v := strings.TrimSpace(a.Value)
		key := source.FieldKey(a.Key)
		if v == "" || key == "" {
			continue
		}
		field, known := dialect.Fields[key]
		switch {
		case field == "FirstName":
			l.FirstName = v
		case field == "LastName":
			l.LastName = v
		case field == "Email":
			if l.Email == "" {
				l.Email = v
			}
		case field == "Company":
			l.Company = v
		case field == "Phone":
			l.Phone = v
		case field == "title":
			l.Title = v
		case field == "website":
			l.Website = v
		case field == "linkedin":
			l.LinkedIn = v
		case field == "location":
			l.Location = v
		case field == "timezone":
			l.Timezone = v
		case !known:
			if l.Custom == nil {
				l.Custom = map[string]string{}
			}
			l.Custom[key] = v
		}
	}
	return l
}

func (s *session) Leads(ctx context.Context, ws source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	if campaignID != "" {
		return nil, "", fmt.Errorf("%s: the prospects of a sequence: %w", label, source.ErrUnsupported)
	}
	if ws.ID != "" {
		return nil, "", nil
	}
	page := 1
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 1 {
			return nil, "", fmt.Errorf("%s: bad cursor %q", label, cursor)
		}
		page = n
	}
	q := url.Values{
		"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)},
		"sort": {"ASC"}, "sortBy": {"createdAt"}, "includeCustomFields": {"true"},
	}
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/v1/prospects", q, &raw); err != nil {
		return nil, "", err
	}
	var ps []prospect
	if err := decodeList(raw, &ps, "prospects"); err != nil {
		return nil, "", fmt.Errorf("%s: decode prospects: %w", label, err)
	}
	out := make([]source.Lead, 0, len(ps))
	for _, p := range ps {
		if l := p.lead(); l.Email != "" {
			out = append(out, l)
		}
	}
	next := ""
	if len(ps) >= pageSize {
		next = strconv.Itoa(page + 1)
	}
	return out, next, nil
}

type dncList struct {
	ID            flexString `json:"id"`
	Name          string     `json:"name"`
	NumberOfItems flexInt    `json:"numberOfItems"`
}

func (s *session) dncLists(ctx context.Context) ([]dncList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dncLoaded {
		return s.dnc, nil
	}
	var all []dncList
	for page := 1; ; page++ {
		var raw json.RawMessage
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		if err := s.c.Get(ctx, "/v1/dnc", q, &raw); err != nil {
			return nil, err
		}
		var lists []dncList
		if err := decodeList(raw, &lists, "dncList", "lists"); err != nil {
			return nil, fmt.Errorf("%s: decode do-not-contact lists: %w", label, err)
		}
		all = append(all, lists...)
		if len(lists) < pageSize {
			break
		}
	}
	s.dnc, s.dncLoaded = all, true
	return all, nil
}

// Blocklist pages every do-not-contact list; the cursor is "list index:page".
func (s *session) Blocklist(ctx context.Context, ws source.Workspace, cursor string) ([]source.BlockEntry, string, error) {
	lists, err := s.dncLists(ctx)
	if err != nil {
		return nil, "", err
	}
	idx, page := 0, 1
	if cursor != "" {
		a, b, ok := strings.Cut(cursor, ":")
		i, err1 := strconv.Atoi(a)
		pg, err2 := strconv.Atoi(b)
		if !ok || err1 != nil || err2 != nil || i < 0 || pg < 1 {
			return nil, "", fmt.Errorf("%s: bad cursor %q", label, cursor)
		}
		idx, page = i, pg
	}
	if idx >= len(lists) {
		return nil, "", nil
	}
	l := lists[idx]
	q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}, "type": {"all"}}
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/v1/dnc/"+url.PathEscape(string(l.ID)), q, &raw); err != nil {
		return nil, "", err
	}
	var resp struct {
		Items []struct {
			Value string `json:"value"`
			Type  string `json:"type"`
		} `json:"dncDetails"`
		Meta struct {
			TotalPages flexInt `json:"totalPages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(unwrap(raw), &resp); err != nil {
		return nil, "", fmt.Errorf("%s: decode do-not-contact items: %w", label, err)
	}
	reason := label + " do-not-contact list"
	if l.Name != "" {
		reason += ": " + l.Name
	}
	out := make([]source.BlockEntry, 0, len(resp.Items))
	for _, it := range resp.Items {
		v := strings.ToLower(strings.TrimSpace(it.Value))
		if v == "" {
			continue
		}
		domain := strings.EqualFold(it.Type, "domain") || (it.Type == "" && !strings.Contains(v, "@"))
		out = append(out, source.BlockEntry{Value: v, Domain: domain, Reason: reason})
	}
	last := len(resp.Items) < pageSize || (resp.Meta.TotalPages > 0 && page >= int(resp.Meta.TotalPages))
	switch {
	case !last:
		return out, fmt.Sprintf("%d:%d", idx, page+1), nil
	case idx+1 < len(lists):
		return out, fmt.Sprintf("%d:1", idx+1), nil
	}
	return out, "", nil
}
