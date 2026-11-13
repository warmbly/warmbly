// Package quickmail reads a QuickMail agency through its GraphQL API V2.
package quickmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	defaultBase = "https://api.quickmail.com/v2/graphql"
	label       = "QuickMail"
)

// pageSize is the connection page asked for; tests shrink it.
var pageSize = 100

type provider struct {
	base  string
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the QuickMail provider.
func New() outreachsource.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() outreachsource.Info {
	return outreachsource.Info{
		ID:    "quickmail",
		Label: label,
		Fields: []outreachsource.Field{{
			Key:         "api_key",
			Label:       "API key",
			Help:        "An agency API key; the plan needs API access.",
			Secret:      true,
			Placeholder: "QuickMail API key",
		}},
		KeyHelp: "In QuickMail, open your agency settings, find API Keys and create a key; API access has to be included in your plan.",
		Kinds: []outreachsource.Kind{
			outreachsource.KindBlocklist, outreachsource.KindMailboxes,
			outreachsource.KindCampaigns, outreachsource.KindLeads,
		},
		Workspaces: true,
		Notes: []string{
			"One QuickMail key reads every workspace in the agency.",
			"QuickMail does not return mailbox passwords, so every inbox is connected again in Warmbly.",
			"QuickMail's API does not say which workspace an inbox belongs to, so the agency's inboxes are listed for every workspace.",
			"QuickMail's API does not return a campaign's sending schedule, inboxes or tracking settings; set them on the imported campaign.",
			"QuickMail's API does not say how far each lead got in a campaign, so leads already in a campaign are restarted or skipped, never continued.",
			"LinkedIn, call, task, SMS, Slack, webhook and checkpoint steps, Liquid conditions and domain-level Do Not Contact entries are not brought over.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds outreachsource.Credentials) (outreachsource.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, outreachsource.ErrKey)
	}
	c := outreachsource.NewClient(label, p.base, func(r *http.Request) {
		r.Header.Set("Authorization", key)
	})
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	s := &session{c: c}
	var data struct {
		Agency struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"agency"`
	}
	if err := s.query(ctx, `query Agency { agency { id name } }`, nil, &data); err != nil {
		return nil, err
	}
	s.agency = strings.TrimSpace(data.Agency.Name)
	return s, nil
}

type session struct {
	c      *outreachsource.Client
	agency string
}

type gqlError struct {
	Message string `json:"message"`
}

// query sends one GraphQL read. Only query operations are ever sent.
func (s *session) query(ctx context.Context, q string, vars map[string]any, out any) error {
	body := map[string]any{"query": q}
	if vars != nil {
		body["variables"] = vars
	}
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := s.c.Post(ctx, "", nil, body, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return &outreachsource.APIError{Label: label, Status: http.StatusOK, Message: resp.Errors[0].Message}
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return &outreachsource.APIError{Label: label, Status: http.StatusOK, Message: "the response held no data"}
	}
	return json.Unmarshal(resp.Data, out)
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// next is the cursor after a page, "" when it was the last.
func (p pageInfo) next() string {
	if !p.HasNextPage {
		return ""
	}
	return p.EndCursor
}

func cursorVar(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

func (s *session) Account(context.Context) (outreachsource.Account, error) {
	return outreachsource.Account{Name: s.agency}, nil
}

func (s *session) Workspaces(ctx context.Context) ([]outreachsource.Workspace, error) {
	const q = `query Workspaces($first: Int!, $after: String) {
  workspaces(first: $first, after: $after) { nodes { id name } pageInfo { hasNextPage endCursor } }
}`
	var out []outreachsource.Workspace
	cursor := ""
	for {
		var data struct {
			Workspaces struct {
				Nodes []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"workspaces"`
		}
		if err := s.query(ctx, q, map[string]any{"first": pageSize, "after": cursorVar(cursor)}, &data); err != nil {
			return nil, err
		}
		for _, w := range data.Workspaces.Nodes {
			out = append(out, outreachsource.Workspace{ID: w.ID, Name: strings.TrimSpace(w.Name)})
		}
		if cursor = data.Workspaces.PageInfo.next(); cursor == "" {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, outreachsource.Workspace{ID: "", Name: s.agency})
	}
	return out, nil
}

func (s *session) Inventory(ctx context.Context, ws outreachsource.Workspace) (outreachsource.Inventory, error) {
	inv := outreachsource.Inventory{Leads: -1, Blocklist: -1}
	accounts, err := s.emailAccounts(ctx)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(accounts)
	camps, err := s.campaigns(ctx, ws, true)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, c := range camps {
		inv.Summaries = append(inv.Summaries, outreachsource.CampaignSummary{
			ID: c.ID, Name: strings.TrimSpace(c.Name), Status: c.status(), Steps: c.emailSteps(), Leads: c.LeadStatus.Total,
		})
	}
	return inv, nil
}

type emailAccount struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Type              string `json:"type"`
	DailySendingLimit *int   `json:"dailySendingLimit"`
}

func (s *session) emailAccounts(ctx context.Context) ([]emailAccount, error) {
	const q = `query EmailAccounts($first: Int!, $after: String) {
  emailAccounts(first: $first, after: $after) {
    nodes { id name email type dailySendingLimit }
    pageInfo { hasNextPage endCursor }
  }
}`
	var out []emailAccount
	cursor := ""
	for {
		var data struct {
			EmailAccounts struct {
				Nodes    []emailAccount `json:"nodes"`
				PageInfo pageInfo       `json:"pageInfo"`
			} `json:"emailAccounts"`
		}
		if err := s.query(ctx, q, map[string]any{"first": pageSize, "after": cursorVar(cursor)}, &data); err != nil {
			return nil, err
		}
		out = append(out, data.EmailAccounts.Nodes...)
		if cursor = data.EmailAccounts.PageInfo.next(); cursor == "" {
			return out, nil
		}
	}
}

// mailboxKind reads QuickMail's provider type; anything not Google or
// Microsoft is SMTP and asked for again.
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

// Mailboxes lists the agency's inboxes; the API does not tie them to a workspace.
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
		m := outreachsource.Mailbox{ID: a.ID, Email: email, FromName: strings.TrimSpace(a.Name), Kind: mailboxKind(a.Type)}
		if a.DailySendingLimit != nil && *a.DailySendingLimit > 0 {
			m.DailyCap = *a.DailySendingLimit
		}
		out = append(out, m)
	}
	return out, nil
}
