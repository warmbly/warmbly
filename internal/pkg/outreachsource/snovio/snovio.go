// Package snovio reads a Snov.io account through its REST API (https://snov.io/api).
package snovio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	label       = "Snov.io"
	defaultBase = "https://api.snov.io"
)

// pageSize is how many records one request asks for; tests lower it.
var pageSize = 100

type provider struct {
	base string
	// sleep replaces the retry wait in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Snov.io provider.
func New() source.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: strings.TrimRight(base, "/")} }

func (p *provider) Info() source.Info {
	return source.Info{
		ID:    "snovio",
		Label: label,
		Fields: []source.Field{
			{Key: "client_id", Label: "Client ID"},
			{Key: "client_secret", Label: "Client secret", Secret: true},
		},
		KeyHelp: "In Snov.io, open Account settings, then API, and copy the client ID and secret.",
		KeyURL:  "https://app.snov.io/account/api",
		Kinds:   []source.Kind{source.KindBlocklist, source.KindMailboxes, source.KindCampaigns, source.KindLeads},
		Notes: []string{
			"LinkedIn steps are not brought over.",
			"Where a sequence branches on a condition, only the path for leads who did not meet it is brought over.",
			"Snov.io's prospect lists hold names and addresses only, so company, job title and custom fields are not brought over.",
			"Snov.io does not report how many emails a lead was sent, so a lead part way through a campaign cannot continue where it left off.",
			"Mailbox passwords are not available through the API, so each mailbox is connected again.",
			"Snov.io allows 60 API requests a minute, so a large account takes a while to read.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds source.Credentials) (source.Session, error) {
	id, secret := strings.TrimSpace(creds["client_id"]), strings.TrimSpace(creds["client_secret"])
	if id == "" || secret == "" {
		return nil, fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	s := &session{clientID: id, secret: secret, now: time.Now}
	s.c = source.NewClient(label, p.base, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+s.bearer()) })
	if p.sleep != nil {
		s.c.Sleep = p.sleep
	}
	if err := s.get(ctx, "/v1/get-balance", nil, nil); err != nil {
		return nil, err
	}
	return s, nil
}

type session struct {
	c        *source.Client
	clientID string
	secret   string
	now      func() time.Time

	tokenMu sync.Mutex
	token   string
	expires time.Time

	mu        sync.Mutex
	senders   map[int64]string
	campaigns []userCampaign
	lists     []userList
	schedules map[int64]schedule
}

func (s *session) bearer() string {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	return s.token
}

// refresh exchanges the client credentials for a bearer token when it is missing,
// expiring within a minute, or force is set.
func (s *session) refresh(ctx context.Context, force bool) error {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if !force && s.token != "" && s.now().Before(s.expires.Add(-time.Minute)) {
		return nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {s.clientID}, "client_secret": {s.secret}}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.c.Base+"/v1/oauth/access_token", strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		resp, err := s.c.HTTP.Do(req)
		retryAfter := ""
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("%s: %w", label, source.ErrUnavailable)
		default:
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				var tok struct {
					AccessToken string `json:"access_token"`
					ExpiresIn   int    `json:"expires_in"`
				}
				if json.Unmarshal(data, &tok) != nil || tok.AccessToken == "" {
					return fmt.Errorf("%s: %w", label, source.ErrKey)
				}
				if tok.ExpiresIn <= 0 {
					tok.ExpiresIn = 3600
				}
				s.token, s.expires = tok.AccessToken, s.now().Add(time.Duration(tok.ExpiresIn)*time.Second)
				return nil
			case resp.StatusCode == http.StatusTooManyRequests:
				lastErr = fmt.Errorf("%s: %w", label, source.ErrRateLimited)
				retryAfter = resp.Header.Get("Retry-After")
			case resp.StatusCode >= 500:
				lastErr = fmt.Errorf("%s: %w", label, source.ErrUnavailable)
			default:
				return fmt.Errorf("%s: %w", label, source.ErrKey)
			}
		}
		if err := s.c.Sleep(ctx, source.Backoff(attempt, retryAfter)); err != nil {
			return err
		}
	}
	return lastErr
}

// call runs one request with a fresh token, renewing it once if the API refuses it.
func (s *session) call(ctx context.Context, do func() error) error {
	if err := s.refresh(ctx, false); err != nil {
		return err
	}
	err := do()
	if !errors.Is(err, source.ErrKey) {
		return err
	}
	if rerr := s.refresh(ctx, true); rerr != nil {
		return rerr
	}
	return do()
}

func (s *session) get(ctx context.Context, path string, q url.Values, out any) error {
	return s.call(ctx, func() error { return s.c.Get(ctx, path, q, out) })
}

func (s *session) Account(ctx context.Context) (source.Account, error) {
	return source.Account{Name: label + " account"}, nil
}

func (s *session) Workspaces(ctx context.Context) ([]source.Workspace, error) {
	return []source.Workspace{{ID: "", Name: label + " account"}}, nil
}

type sender struct {
	ID         int64    `json:"id"`
	SenderName string   `json:"sender_name"`
	EmailFrom  string   `json:"email_from"`
	Limitation int      `json:"limitation"`
	Provider   string   `json:"provider"`
	Tags       []string `json:"tags"`
}

func (sn sender) mailbox() source.Mailbox {
	m := source.Mailbox{
		ID: strconv.FormatInt(sn.ID, 10), Email: strings.TrimSpace(sn.EmailFrom), FromName: strings.TrimSpace(sn.SenderName),
		Kind: source.MailboxSMTP, DailyCap: sn.Limitation, Tags: sn.Tags,
	}
	p := strings.ToLower(sn.Provider)
	switch {
	case strings.Contains(p, "gmail"), strings.Contains(p, "google"), strings.Contains(p, "gsuite"):
		m.Kind = source.MailboxGoogle
	case strings.Contains(p, "outlook"), strings.Contains(p, "microsoft"), strings.Contains(p, "office"), strings.Contains(p, "hotmail"):
		m.Kind = source.MailboxMicrosoft
	}
	return m
}

func (s *session) senderList(ctx context.Context) ([]sender, error) {
	var resp struct {
		Data []sender `json:"data"`
	}
	if err := s.get(ctx, "/v2/sender-accounts/emails", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (s *session) Mailboxes(ctx context.Context, ws source.Workspace) ([]source.Mailbox, error) {
	list, err := s.senderList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Mailbox, 0, len(list))
	for _, sn := range list {
		if sn.EmailFrom != "" {
			out = append(out, sn.mailbox())
		}
	}
	return out, nil
}

// senderEmails maps sender account ids to addresses, read once per session.
func (s *session) senderEmails(ctx context.Context) (map[int64]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.senders != nil {
		return s.senders, nil
	}
	list, err := s.senderList(ctx)
	if err != nil {
		return nil, err
	}
	s.senders = map[int64]string{}
	for _, sn := range list {
		s.senders[sn.ID] = strings.ToLower(strings.TrimSpace(sn.EmailFrom))
	}
	return s.senders, nil
}

type userCampaign struct {
	ID        int64  `json:"id"`
	Campaign  string `json:"campaign"`
	ListID    int64  `json:"list_id"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

type userList struct {
	ID        int64 `json:"id"`
	Name      string
	Contacts  int  `json:"contacts"`
	IsDeleted bool `json:"isDeleted"`
}

func (s *session) userCampaigns(ctx context.Context) ([]userCampaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.campaigns != nil {
		return s.campaigns, nil
	}
	var out []userCampaign
	if err := s.get(ctx, "/v1/get-user-campaigns", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []userCampaign{}
	}
	s.campaigns = out
	return out, nil
}

func (s *session) userLists(ctx context.Context) ([]userList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lists != nil {
		return s.lists, nil
	}
	var all []userList
	if err := s.get(ctx, "/v1/get-user-lists", nil, &all); err != nil {
		return nil, err
	}
	out := []userList{}
	for _, l := range all {
		if !l.IsDeleted {
			out = append(out, l)
		}
	}
	s.lists = out
	return out, nil
}

func (s *session) Inventory(ctx context.Context, ws source.Workspace) (source.Inventory, error) {
	var inv source.Inventory
	senders, err := s.senderList(ctx)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(senders)
	lists, err := s.userLists(ctx)
	if err != nil {
		return inv, err
	}
	contacts := map[int64]int{}
	for _, l := range lists {
		inv.Leads += l.Contacts
		contacts[l.ID] = l.Contacts
	}
	camps, err := s.userCampaigns(ctx)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, uc := range camps {
		steps := 0
		if info, err := s.campaignInfo(ctx, uc.ID); err == nil {
			for _, st := range info.Sequence.Steps {
				if st.Type == "email" {
					steps++
				}
			}
		} else if !isAPIRefusal(err) {
			return inv, err
		}
		inv.Summaries = append(inv.Summaries, source.CampaignSummary{
			ID: strconv.FormatInt(uc.ID, 10), Name: uc.Campaign, Status: campaignStatus(uc.Status), Steps: steps, Leads: contacts[uc.ListID],
		})
	}
	bls, err := s.blacklists(ctx)
	if err != nil {
		return inv, err
	}
	for _, b := range bls {
		inv.Blocklist += b.Total
	}
	return inv, nil
}

// isAPIRefusal is a refusal of one record (a 404 on an old campaign), not of the key.
func isAPIRefusal(err error) bool {
	var ae *source.APIError
	return errors.As(err, &ae)
}

type prospect struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Emails    []struct {
		Email string `json:"email"`
	} `json:"emails"`
}

func (p prospect) lead() source.Lead {
	l := source.Lead{ID: p.ID, FirstName: strings.TrimSpace(p.FirstName), LastName: strings.TrimSpace(p.LastName), Status: source.LeadNotStarted}
	for _, e := range p.Emails {
		if v := strings.TrimSpace(e.Email); v != "" {
			l.Email = v
			break
		}
	}
	if n := strings.TrimSpace(p.Name); n != "" {
		l.Custom = map[string]string{"full_name": n}
	}
	return l
}

// listPage reads one page of a prospect list; last is true when no page follows it.
func (s *session) listPage(ctx context.Context, listID int64, page int) ([]source.Lead, bool, error) {
	q := url.Values{"listId": {strconv.FormatInt(listID, 10)}, "page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(pageSize)}}
	var resp struct {
		Prospects []prospect `json:"prospects"`
	}
	if err := s.call(ctx, func() error { return s.c.Post(ctx, "/v1/prospect-list", q, nil, &resp) }); err != nil {
		return nil, false, err
	}
	out := make([]source.Lead, 0, len(resp.Prospects))
	for _, p := range resp.Prospects {
		if l := p.lead(); l.Email != "" {
			out = append(out, l)
		}
	}
	return out, len(resp.Prospects) < pageSize, nil
}

// recipientStatus maps Snov.io's recipient states; an address not yet in the campaign has not started.
func recipientStatus(exist bool, status string) source.LeadStatus {
	if !exist {
		return source.LeadNotStarted
	}
	s := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(status)))
	switch {
	case s == "active":
		return source.LeadInProgress
	case s == "paused":
		return source.LeadPaused
	case s == "finished", s == "moved":
		return source.LeadCompleted
	case strings.HasPrefix(s, "unsubscribe"):
		return source.LeadUnsubscribed
	case strings.Contains(s, "bounce"):
		return source.LeadBounced
	case strings.Contains(s, "repl"):
		return source.LeadReplied
	}
	return source.LeadNotStarted
}

func (s *session) statuses(ctx context.Context, campaignID string, leads []source.Lead) error {
	if len(leads) == 0 {
		return nil
	}
	q := url.Values{}
	for _, l := range leads {
		q.Add("emails[]", l.Email)
	}
	var resp struct {
		Data []struct {
			Email  string `json:"email"`
			Exist  bool   `json:"exist"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := s.get(ctx, "/v2/campaigns/"+url.PathEscape(campaignID)+"/recipient", q, &resp); err != nil {
		return err
	}
	byEmail := map[string]source.LeadStatus{}
	for _, d := range resp.Data {
		byEmail[strings.ToLower(d.Email)] = recipientStatus(d.Exist, d.Status)
	}
	for i := range leads {
		if st, ok := byEmail[strings.ToLower(leads[i].Email)]; ok {
			leads[i].Status = st
		}
	}
	return nil
}

func (s *session) Leads(ctx context.Context, ws source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	if campaignID != "" {
		return s.campaignLeads(ctx, campaignID, cursor)
	}
	return s.looseLeads(ctx, cursor)
}

// campaignLeads pages the campaign's recipient list; the cursor is the page number.
func (s *session) campaignLeads(ctx context.Context, campaignID, cursor string) ([]source.Lead, string, error) {
	page := 1
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 1 {
			return nil, "", fmt.Errorf("%s: bad cursor %q", label, cursor)
		}
		page = n
	}
	camps, err := s.userCampaigns(ctx)
	if err != nil {
		return nil, "", err
	}
	var listID int64
	for _, uc := range camps {
		if strconv.FormatInt(uc.ID, 10) == campaignID {
			listID = uc.ListID
		}
	}
	if listID == 0 {
		return nil, "", nil
	}
	leads, last, err := s.listPage(ctx, listID, page)
	if err != nil {
		return nil, "", err
	}
	if err := s.statuses(ctx, campaignID, leads); err != nil {
		return nil, "", err
	}
	if last {
		return leads, "", nil
	}
	return leads, strconv.Itoa(page + 1), nil
}

// looseLeads pages the lists no campaign sends to; the cursor is "list index:page".
func (s *session) looseLeads(ctx context.Context, cursor string) ([]source.Lead, string, error) {
	idx, page, err := parsePair(cursor)
	if err != nil {
		return nil, "", err
	}
	lists, err := s.userLists(ctx)
	if err != nil {
		return nil, "", err
	}
	camps, err := s.userCampaigns(ctx)
	if err != nil {
		return nil, "", err
	}
	used := map[int64]bool{}
	for _, uc := range camps {
		used[uc.ListID] = true
	}
	var loose []userList
	for _, l := range lists {
		if !used[l.ID] {
			loose = append(loose, l)
		}
	}
	if idx >= len(loose) {
		return nil, "", nil
	}
	leads, last, err := s.listPage(ctx, loose[idx].ID, page)
	if err != nil {
		return nil, "", err
	}
	return leads, nextPair(idx, page, last, len(loose)), nil
}

func parsePair(cursor string) (int, int, error) {
	if cursor == "" {
		return 0, 1, nil
	}
	a, b, ok := strings.Cut(cursor, ":")
	i, err1 := strconv.Atoi(a)
	p, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil || i < 0 || p < 1 {
		return 0, 0, fmt.Errorf("%s: bad cursor %q", label, cursor)
	}
	return i, p, nil
}

func nextPair(idx, page int, last bool, n int) string {
	switch {
	case !last:
		return fmt.Sprintf("%d:%d", idx, page+1)
	case idx+1 < n:
		return fmt.Sprintf("%d:1", idx+1)
	}
	return ""
}

type blacklist struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Total int    `json:"total"`
}

func (s *session) blacklists(ctx context.Context) ([]blacklist, error) {
	var resp struct {
		Data []blacklist `json:"data"`
	}
	if err := s.get(ctx, "/v2/blacklists", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Blocklist pages every do-not-email list; the cursor is "list index:page".
func (s *session) Blocklist(ctx context.Context, ws source.Workspace, cursor string) ([]source.BlockEntry, string, error) {
	idx, page, err := parsePair(cursor)
	if err != nil {
		return nil, "", err
	}
	lists, err := s.blacklists(ctx)
	if err != nil {
		return nil, "", err
	}
	if idx >= len(lists) {
		return nil, "", nil
	}
	l := lists[idx]
	q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
	var resp struct {
		Data []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"data"`
	}
	if err := s.get(ctx, "/v2/blacklists/"+strconv.FormatInt(l.ID, 10)+"/items", q, &resp); err != nil {
		return nil, "", err
	}
	reason := label + " do-not-email list"
	if l.Name != "" {
		reason += ": " + l.Name
	}
	out := make([]source.BlockEntry, 0, len(resp.Data))
	for _, it := range resp.Data {
		v := strings.ToLower(strings.TrimSpace(it.Value))
		if v == "" {
			continue
		}
		switch strings.ToLower(it.Type) {
		case "email":
			out = append(out, source.BlockEntry{Value: v, Reason: reason})
		case "domain":
			out = append(out, source.BlockEntry{Value: v, Domain: true, Reason: reason})
		}
	}
	return out, nextPair(idx, page, len(resp.Data) < pageSize, len(lists)), nil
}

// ref is a step reference Snov.io sends as a string, though it is numeric.
type ref string

func (r *ref) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*r = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*r = ref(s)
		return nil
	}
	*r = ref(string(b))
	return nil
}
