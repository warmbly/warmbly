// Package replyio reads a Reply.io team through its v3 API
// (https://docs.reply.io/api-reference/introduction) into outreachsource types.
package replyio

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
	defaultBase = "https://api.reply.io"
	label       = "Reply.io"
	pageSize    = 100
	// bulkPage is the largest page Reply serves, used for reads kept in memory.
	bulkPage = 1000
)

type provider struct {
	base string
	// sleep replaces the client's backoff wait in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Reply.io provider.
func New() source.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() source.Info {
	return source.Info{
		ID:    "replyio",
		Label: "Reply.io",
		Fields: []source.Field{{
			Key:    "api_key",
			Label:  "API key",
			Help:   "A personal or Team key. The key needs read access to contacts, sequences and channels.",
			Secret: true,
		}},
		KeyHelp:    "In Reply.io, open Settings, then API Key, and create or copy a key.",
		KeyURL:     "https://run.reply.io/Dashboard/Material#/settings/api-keys",
		Kinds:      []source.Kind{source.KindBlocklist, source.KindMailboxes, source.KindCampaigns, source.KindLeads},
		Workspaces: false,
		MultiKey:   true,
		Notes: []string{
			"A Reply.io key reads one team, so an agency adds one key per client team. Organization keys are not supported.",
			"Only email steps are brought over. LinkedIn, call, SMS, WhatsApp, task and condition steps are listed so you can rebuild them.",
			"Mailbox passwords are not available through the Reply.io API, so each mailbox is connected again in Warmbly.",
			"Reply.io's own global blacklist rules are not brought over, only your team's.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds source.Credentials) (source.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	c := source.NewClient(label, p.base, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+key) })
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	s := &session{c: c, seqs: map[int]*sequence{}, schedules: map[int]*schedule{}}
	if err := c.Get(ctx, "/v3/whoami", nil, &s.me); err != nil {
		return nil, err
	}
	return s, nil
}

type whoami struct {
	UserID   int    `json:"userId"`
	Username string `json:"username"`
	TeamID   int    `json:"teamId"`
}

type session struct {
	c  *source.Client
	me whoami

	mu        sync.Mutex
	seqs      map[int]*sequence
	schedules map[int]*schedule
	contacts  map[int]contact // every contact of the team, read once
	order     []int           // contact ids in the order Reply listed them
	enrolled  map[int]bool    // contacts in at least one sequence
}

func (s *session) Account(context.Context) (source.Account, error) {
	a := source.Account{Name: s.me.Username}
	if strings.Contains(s.me.Username, "@") {
		a.Email = s.me.Username
	}
	return a, nil
}

func (s *session) Workspaces(context.Context) ([]source.Workspace, error) {
	return []source.Workspace{{ID: "", Name: s.me.Username}}, nil
}

func (s *session) Inventory(ctx context.Context, _ source.Workspace) (source.Inventory, error) {
	inv := source.Inventory{Leads: -1, Blocklist: -1}
	seqs, err := s.listSequences(ctx)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(seqs)
	for _, sq := range seqs {
		inv.Summaries = append(inv.Summaries, source.CampaignSummary{
			ID: strconv.Itoa(sq.ID), Name: sq.Name, Status: sq.status(), Steps: -1, Leads: -1,
		})
	}
	accts, err := s.listEmailAccounts(ctx)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(accts)
	return inv, nil
}

// page is Reply's list envelope, paged with top and skip.
type page[T any] struct {
	Items   []T  `json:"items"`
	HasMore bool `json:"hasMore"`
}

// all reads every page of a top/skip list.
func all[T any](ctx context.Context, c *source.Client, path string, q url.Values) ([]T, error) {
	var out []T
	for skip := 0; ; skip += bulkPage {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("top", strconv.Itoa(bulkPage))
		qq.Set("skip", strconv.Itoa(skip))
		var p page[T]
		if err := c.Get(ctx, path, qq, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		if !p.HasMore || len(p.Items) == 0 {
			return out, nil
		}
	}
}

type emailAccount struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	SenderName  string   `json:"senderName"`
	DailyLimit  int      `json:"dailyLimit"`
	AccountType string   `json:"emailAccountType"`
	Tags        []string `json:"tags"`
}

type emailAccountDetail struct {
	Connection struct {
		SMTPHost string `json:"smtpHost"`
		SMTPPort int    `json:"smtpPort"`
		SMTPSSL  bool   `json:"smtpSsl"`
		IMAPHost string `json:"imapHost"`
		IMAPPort int    `json:"imapPort"`
		IMAPSSL  bool   `json:"imapSsl"`
	} `json:"connection"`
	Safety struct {
		DailyLimit int `json:"dailyLimit"`
	} `json:"safety"`
	Signature struct {
		Signature string `json:"signature"`
	} `json:"signature"`
}

func (s *session) listEmailAccounts(ctx context.Context) ([]emailAccount, error) {
	return all[emailAccount](ctx, s.c, "/v3/email-accounts", nil)
}

func (s *session) Mailboxes(ctx context.Context, _ source.Workspace) ([]source.Mailbox, error) {
	accts, err := s.listEmailAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Mailbox, 0, len(accts))
	for _, a := range accts {
		m := source.Mailbox{
			ID: strconv.FormatInt(a.ID, 10), Email: strings.ToLower(a.Email), FromName: a.SenderName,
			Kind: mailboxKind(a.AccountType), DailyCap: a.DailyLimit, Tags: a.Tags,
		}
		var d emailAccountDetail
		if err := s.c.Get(ctx, "/v3/email-accounts/"+strconv.FormatInt(a.ID, 10), nil, &d); err != nil && !refused(err) {
			return nil, err
		}
		m.Signature = d.Signature.Signature
		if d.Safety.DailyLimit > 0 {
			m.DailyCap = d.Safety.DailyLimit
		}
		if m.Kind == source.MailboxSMTP && d.Connection.SMTPHost != "" {
			m.SMTP = &source.Server{Host: d.Connection.SMTPHost, Port: d.Connection.SMTPPort, Security: security(d.Connection.SMTPSSL, d.Connection.SMTPPort)}
			m.IMAP = &source.Server{Host: d.Connection.IMAPHost, Port: d.Connection.IMAPPort, Security: security(d.Connection.IMAPSSL, d.Connection.IMAPPort)}
		}
		out = append(out, m)
	}
	return out, nil
}

// refused is a 4xx Reply explained, which leaves one optional read empty.
func refused(err error) bool {
	var ae *source.APIError
	return errors.As(err, &ae)
}

func mailboxKind(t string) source.MailboxKind {
	switch t {
	case "gmail":
		return source.MailboxGoogle
	case "outlook", "exchange":
		return source.MailboxMicrosoft
	}
	return source.MailboxSMTP
}

// security reads Reply's one TLS flag; on the submission port it means STARTTLS.
func security(tls bool, port int) source.Security {
	switch {
	case port == 587:
		return source.SecurityStartTLS
	case tls:
		return source.SecuritySSL
	}
	return source.SecurityNone
}

type blacklistRule struct {
	Pattern  string `json:"pattern"`
	IsGlobal bool   `json:"isGlobal"`
}

// Blocklist pages the team's domain rules, then its email rules. The cursor
// is "domains:<skip>" or "emails:<skip>".
func (s *session) Blocklist(ctx context.Context, _ source.Workspace, cursor string) ([]source.BlockEntry, string, error) {
	kind, skip := "domains", 0
	if cursor != "" {
		k, n, ok := strings.Cut(cursor, ":")
		v, err := strconv.Atoi(n)
		if !ok || err != nil || v < 0 || (k != "domains" && k != "emails") {
			return nil, "", fmt.Errorf("%s: invalid cursor", label)
		}
		kind, skip = k, v
	}
	q := url.Values{"top": {strconv.Itoa(pageSize)}, "skip": {strconv.Itoa(skip)}}
	var p page[blacklistRule]
	if err := s.c.Get(ctx, "/v3/contact-blacklist-rules/"+kind, q, &p); err != nil {
		return nil, "", err
	}
	var out []source.BlockEntry
	for _, r := range p.Items {
		if r.IsGlobal {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(r.Pattern))
		if kind == "domains" {
			v = strings.TrimLeft(v, "*@.")
			if v != "" {
				out = append(out, source.BlockEntry{Value: v, Domain: true})
			}
			continue
		}
		if strings.HasPrefix(v, "*@") {
			out = append(out, source.BlockEntry{Value: strings.TrimPrefix(v, "*@"), Domain: true})
		} else if v != "" && !strings.Contains(v, "*") {
			out = append(out, source.BlockEntry{Value: v})
		}
	}
	switch {
	case p.HasMore && len(p.Items) > 0:
		return out, kind + ":" + strconv.Itoa(skip+len(p.Items)), nil
	case kind == "domains":
		return out, "emails:0", nil
	}
	return out, "", nil
}
