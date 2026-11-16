// Package woodpecker reads a Woodpecker account through its public REST API
// (https://developers.woodpecker.co/) into outreachsource types.
package woodpecker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// ID is Woodpecker's provider id.
const ID outreachsource.ProviderID = "woodpecker"

const (
	label       = "Woodpecker"
	defaultBase = "https://api.woodpecker.co/rest"
)

type provider struct {
	base  string
	sleep func(context.Context, time.Duration) error
}

// New returns the Woodpecker provider.
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
			Help:   "An agency HQ key reads every active client account.",
		}},
		KeyHelp:    "In Woodpecker, open Add-ons, then API & Integrations, then API keys, and create a key.",
		KeyURL:     "https://app.woodpecker.co/panel#add-ons/integrations/api-keys",
		Kinds:      []outreachsource.Kind{outreachsource.KindBlocklist, outreachsource.KindMailboxes, outreachsource.KindCampaigns, outreachsource.KindLeads},
		Workspaces: true,
		Notes: []string{
			"The key needs Woodpecker's API keys add-on.",
			"LinkedIn steps are not brought over.",
			"Woodpecker's API cannot read campaigns with IF conditions, a scheduled start or manual task steps, so their emails are not brought over.",
			"Mailbox passwords are not available through the API, so SMTP mailboxes are connected again in Warmbly.",
			"Templates refer to custom snippets by number (snippet_1 to snippet_15), as Woodpecker's API returns them.",
			"Wildcard domain patterns in the blacklist are not brought over.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds outreachsource.Credentials) (outreachsource.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, outreachsource.ErrKey)
	}
	c := outreachsource.NewClient(label, p.base, func(r *http.Request) { r.Header.Set("x-api-key", key) })
	c.HTTP.Transport = v1RateLimit{}
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	if err := c.Get(ctx, "/v1/me", nil, nil); err != nil {
		return nil, err
	}
	return &session{c: c}, nil
}

// v1RateLimit turns the v1 API's rate-limit answer (409 E_TOO_MANY_REQUESTS)
// into the 429 the shared client retries.
type v1RateLimit struct{}

func (v1RateLimit) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil || resp.StatusCode != http.StatusConflict {
		return resp, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if bytes.Contains(body, []byte("E_TOO_MANY_REQUESTS")) {
		resp.StatusCode = http.StatusTooManyRequests
		resp.Status = "429 Too Many Requests"
	}
	return resp, nil
}

type company struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type pagination struct {
	TotalPages int `json:"total_pages"`
}

type session struct {
	c *outreachsource.Client

	mu        sync.Mutex
	probed    bool
	agency    bool
	companies []company
}

// client reads as ws: an agency client account through the HQ key's x-company-id.
func (s *session) client(ws outreachsource.Workspace) *outreachsource.Client {
	if ws.ID == "" {
		return s.c
	}
	c := *s.c
	auth := s.c.Auth
	c.Auth = func(r *http.Request) {
		auth(r)
		r.Header.Set("x-company-id", ws.ID)
	}
	return &c
}

// agencyCompanies lists active client companies; ok is false for a key without the agency API.
func (s *session) agencyCompanies(ctx context.Context) (list []company, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probed {
		return s.companies, s.agency, nil
	}
	var all []company
	for page := 1; ; page++ {
		var resp struct {
			Content    []company  `json:"content"`
			Pagination pagination `json:"pagination_data"`
		}
		q := url.Values{"active": {"true"}, "page": {strconv.Itoa(page)}}
		if err := s.c.Get(ctx, "/v2/agency/companies", q, &resp); err != nil {
			if page == 1 && notAgency(err) {
				s.probed = true
				return nil, false, nil
			}
			return nil, false, err
		}
		all = append(all, resp.Content...)
		if len(resp.Content) == 0 || page >= resp.Pagination.TotalPages {
			break
		}
	}
	s.probed, s.agency, s.companies = true, true, all
	return all, true, nil
}

func notAgency(err error) bool {
	var ae *outreachsource.APIError
	return errors.Is(err, outreachsource.ErrKey) || errors.Is(err, outreachsource.ErrForbidden) || errors.As(err, &ae)
}

func (s *session) Account(ctx context.Context) (outreachsource.Account, error) {
	var resp struct {
		Content []struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"content"`
	}
	if err := s.c.Get(ctx, "/v2/users", url.Values{"page": {"0"}}, &resp); err != nil {
		return outreachsource.Account{}, err
	}
	// The key's own user is not exposed; the account's admin stands for it.
	for _, u := range resp.Content {
		if strings.EqualFold(u.Role, "admin") {
			return outreachsource.Account{Name: u.Name, Email: u.Email}, nil
		}
	}
	if len(resp.Content) > 0 {
		return outreachsource.Account{Name: resp.Content[0].Name, Email: resp.Content[0].Email}, nil
	}
	return outreachsource.Account{}, nil
}

func (s *session) Workspaces(ctx context.Context) ([]outreachsource.Workspace, error) {
	companies, agency, err := s.agencyCompanies(ctx)
	if err != nil {
		return nil, err
	}
	if !agency {
		acct, err := s.Account(ctx)
		if err != nil {
			return nil, err
		}
		name := acct.Name
		if name == "" {
			name = "Woodpecker account"
		}
		return []outreachsource.Workspace{{ID: "", Name: name}}, nil
	}
	out := []outreachsource.Workspace{{ID: "", Name: "Agency HQ"}}
	for _, c := range companies {
		out = append(out, outreachsource.Workspace{ID: strconv.FormatInt(c.ID, 10), Name: c.Name})
	}
	return out, nil
}

func (s *session) Inventory(ctx context.Context, ws outreachsource.Workspace) (outreachsource.Inventory, error) {
	c := s.client(ws)
	var inv outreachsource.Inventory
	boxes, err := s.Mailboxes(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(boxes)
	list, err := campaignList(ctx, c)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(list)
	for _, cl := range list {
		st, err := campaignStats(ctx, c, cl.ID)
		if err != nil {
			return inv, err
		}
		// A prospect in two campaigns counts twice, as it is enrolled twice.
		inv.Leads += st.prospects
		inv.Summaries = append(inv.Summaries, outreachsource.CampaignSummary{
			ID: strconv.FormatInt(cl.ID, 10), Name: cl.Name, Status: campaignStatus(cl.Status), Steps: st.emails, Leads: st.prospects,
		})
	}
	sources, err := s.blockSources(ctx, ws)
	if err != nil {
		return inv, err
	}
	for _, src := range sources {
		var resp struct {
			Total int `json:"total"`
		}
		if err := src.client.Get(ctx, src.path, url.Values{"per_page": {"1"}}, &resp); err != nil {
			return inv, err
		}
		inv.Blocklist += resp.Total
	}
	return inv, nil
}

type wpMailbox struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Details struct {
		Email      string  `json:"email"`
		Provider   string  `json:"provider"`
		Login      string  `json:"login"`
		Server     *string `json:"server"`
		Port       *int    `json:"port"`
		FromName   string  `json:"from_name"`
		DailyLimit int     `json:"daily_limit"`
		Signature  string  `json:"signature"`
		ImapID     int64   `json:"imap_id"`
		Warmup     *struct {
			Status string `json:"status"`
		} `json:"warmup_data"`
	} `json:"details"`
}

func (s *session) Mailboxes(ctx context.Context, ws outreachsource.Workspace) ([]outreachsource.Mailbox, error) {
	var list []wpMailbox
	if err := s.client(ws).Get(ctx, "/v2/mailboxes", nil, &list); err != nil {
		return nil, err
	}
	imap := map[int64]wpMailbox{}
	for _, m := range list {
		if strings.EqualFold(m.Type, "IMAP") {
			imap[m.ID] = m
		}
	}
	var out []outreachsource.Mailbox
	for _, m := range list {
		if !strings.EqualFold(m.Type, "SMTP") {
			continue
		}
		mb := outreachsource.Mailbox{
			ID:        strconv.FormatInt(m.ID, 10),
			Email:     m.Details.Email,
			FromName:  m.Details.FromName,
			DailyCap:  m.Details.DailyLimit,
			Signature: m.Details.Signature,
		}
		switch strings.ToUpper(m.Details.Provider) {
		case "GOOGLE":
			mb.Kind = outreachsource.MailboxGoogle
		case "OFFICE":
			mb.Kind = outreachsource.MailboxMicrosoft
		default:
			mb.Kind = outreachsource.MailboxSMTP
			mb.SMTP = server(m)
			if im, ok := imap[m.Details.ImapID]; ok {
				mb.IMAP = server(im)
			}
		}
		if w := m.Details.Warmup; w != nil && w.Status != "" {
			on := strings.EqualFold(w.Status, "RUNNING")
			mb.Warmup = &on
		}
		out = append(out, mb)
	}
	return out, nil
}

func server(m wpMailbox) *outreachsource.Server {
	srv := &outreachsource.Server{Username: m.Details.Login}
	if m.Details.Server != nil {
		srv.Host = *m.Details.Server
	}
	if m.Details.Port != nil {
		srv.Port = *m.Details.Port
	}
	switch srv.Port {
	case 465, 993:
		srv.Security = outreachsource.SecuritySSL
	case 587, 143:
		srv.Security = outreachsource.SecurityStartTLS
	}
	return srv
}

// blockSource is one paginated blacklist: the account's own or the agency-wide one.
type blockSource struct {
	client *outreachsource.Client
	path   string
	field  string
	domain bool
	reason string
}

func (s *session) blockSources(ctx context.Context, ws outreachsource.Workspace) ([]blockSource, error) {
	c := s.client(ws)
	out := []blockSource{
		{c, "/v2/blacklist/emails", "emails", false, "Woodpecker blacklist"},
		{c, "/v2/blacklist/domains", "domains", true, "Woodpecker blacklist"},
	}
	// The agency blacklist binds HQ and every client, so each workspace takes it.
	_, agency, err := s.agencyCompanies(ctx)
	if err != nil {
		return nil, err
	}
	if agency {
		out = append(out,
			blockSource{s.c, "/v2/agency/blacklist/emails", "emails", false, "Woodpecker agency blacklist"},
			blockSource{s.c, "/v2/agency/blacklist/domains", "domains", true, "Woodpecker agency blacklist"},
		)
	}
	return out, nil
}

var blockPageSize = 500 // a var so tests can page through small fixtures

// Blocklist cursors are "source/page/first": page 0 asks without a page
// number, and first is the previous page's first value. The docs do not say
// whether pages count from 0 or 1; a repeat of the previous page tells.
func (s *session) Blocklist(ctx context.Context, ws outreachsource.Workspace, cursor string) ([]outreachsource.BlockEntry, string, error) {
	sources, err := s.blockSources(ctx, ws)
	if err != nil {
		return nil, "", err
	}
	for {
		page, next, err := s.blockPage(ctx, sources, cursor)
		if err != nil || len(page) > 0 || next == "" {
			return page, next, err
		}
		cursor = next
	}
}

func (s *session) blockPage(ctx context.Context, sources []blockSource, cursor string) ([]outreachsource.BlockEntry, string, error) {
	idx, pageNo, first, err := parseBlockCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if idx >= len(sources) {
		return nil, "", nil
	}
	src := sources[idx]
	fetch := func(page int) ([]string, error) {
		q := url.Values{"per_page": {strconv.Itoa(blockPageSize)}}
		if page > 0 {
			q.Set("page", strconv.Itoa(page))
		}
		var resp map[string]any
		if err := src.client.Get(ctx, src.path, q, &resp); err != nil {
			return nil, err
		}
		raw, _ := resp[src.field].([]any)
		vals := make([]string, 0, len(raw))
		for _, v := range raw {
			if str, ok := v.(string); ok {
				vals = append(vals, str)
			}
		}
		return vals, nil
	}
	vals, err := fetch(pageNo)
	if err != nil {
		return nil, "", err
	}
	repeats := func() bool { return pageNo > 0 && len(vals) > 0 && vals[0] == first }
	if repeats() && pageNo == 1 {
		// Pages count from 1, so page 1 repeated the first page.
		pageNo = 2
		if vals, err = fetch(pageNo); err != nil {
			return nil, "", err
		}
	}
	if repeats() {
		vals = nil // the page number was ignored; stop rather than loop
	}
	var next string
	switch {
	case len(vals) < blockPageSize:
		if idx+1 < len(sources) {
			next = fmt.Sprintf("%d/0/", idx+1)
		}
	default:
		next = fmt.Sprintf("%d/%d/%s", idx, pageNo+1, url.QueryEscape(vals[0]))
	}
	out := make([]outreachsource.BlockEntry, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || strings.ContainsAny(v, "%*") {
			continue
		}
		out = append(out, outreachsource.BlockEntry{Value: strings.ToLower(v), Domain: src.domain, Reason: src.reason})
	}
	return out, next, nil
}

func parseBlockCursor(cursor string) (idx, page int, first string, err error) {
	if cursor == "" {
		return 0, 0, "", nil
	}
	parts := strings.SplitN(cursor, "/", 3)
	if len(parts) != 3 {
		return 0, 0, "", fmt.Errorf("%s: bad blocklist cursor", label)
	}
	if idx, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, "", fmt.Errorf("%s: bad blocklist cursor", label)
	}
	if page, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, "", fmt.Errorf("%s: bad blocklist cursor", label)
	}
	first, _ = url.QueryUnescape(parts[2])
	return idx, page, first, nil
}

// parseTime reads Woodpecker's "2025-03-20T14:32:34+0100" and RFC 3339.
func parseTime(s string) *time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
