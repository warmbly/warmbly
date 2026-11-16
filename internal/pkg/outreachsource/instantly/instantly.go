// Package instantly reads an Instantly workspace through API V2.
package instantly

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// pageSize is Instantly's maximum page; tests shrink it.
var pageSize = 100

const (
	defaultBase = "https://api.instantly.ai/api/v2"
	label       = "Instantly"
	// asWorkspace makes an admin workspace key act as one of its sub workspaces.
	asWorkspace = "x-as-workspace"
)

type provider struct {
	base  string
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Instantly provider.
func New() outreachsource.Provider { return newProvider(defaultBase) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() outreachsource.Info {
	return outreachsource.Info{
		ID:    "instantly",
		Label: label,
		Fields: []outreachsource.Field{{
			Key:         "api_key",
			Label:       "API key",
			Help:        "An API V2 key with the all:read scope.",
			Secret:      true,
			Placeholder: "Instantly API V2 key",
		}},
		KeyHelp: "In Instantly, open Settings, then Integrations, then API Keys, and create a key with the all:read scope.",
		KeyURL:  "https://app.instantly.ai/app/settings/integrations",
		Kinds: []outreachsource.Kind{
			outreachsource.KindBlocklist, outreachsource.KindMailboxes,
			outreachsource.KindCampaigns, outreachsource.KindLeads,
		},
		Workspaces:     true,
		MultiKey:       true,
		MailboxSecrets: false,
		Notes: []string{
			"Each Instantly key reads one workspace. An admin workspace key also reads the sub workspaces in its workspace group; add a key for every other workspace.",
			"Instantly does not return mailbox passwords, so custom SMTP and IMAP mailboxes are asked for again.",
			"Subsequences and Liquid conditions are not brought over.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds outreachsource.Credentials) (outreachsource.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, &labelled{outreachsource.ErrKey}
	}
	c := outreachsource.NewClient(label, p.base, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+key)
	})
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	var ws workspace
	if err := c.Get(ctx, "/workspaces/current", nil, &ws); err != nil {
		return nil, err
	}
	return &session{c: c, current: ws, steps: map[string]int{}, tagged: map[string][]string{}}, nil
}

type labelled struct{ err error }

func (l *labelled) Error() string { return label + ": " + l.err.Error() }
func (l *labelled) Unwrap() error { return l.err }

type session struct {
	c       *outreachsource.Client
	current workspace

	mu     sync.Mutex
	steps  map[string]int      // campaign id -> email steps, for StepsSent
	tagged map[string][]string // sender tag ids -> mailbox addresses
}

type workspace struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	PlanID *string `json:"plan_id"`
}

// api is the client reading ws; a sub workspace is read through the admin key.
func (s *session) api(ws outreachsource.Workspace) *outreachsource.Client {
	if ws.ID == "" || ws.ID == s.current.ID {
		return s.c
	}
	c := *s.c
	auth, id := s.c.Auth, ws.ID
	c.Auth = func(r *http.Request) {
		auth(r)
		r.Header.Set(asWorkspace, id)
	}
	return &c
}

func (s *session) Account(context.Context) (outreachsource.Account, error) {
	a := outreachsource.Account{Name: s.current.Name}
	if s.current.PlanID != nil {
		a.Plan = *s.current.PlanID
	}
	return a, nil
}

type groupMember struct {
	SubWorkspaceID   string  `json:"sub_workspace_id"`
	SubWorkspaceName *string `json:"sub_workspace_name"`
	Status           string  `json:"status"`
}

func (s *session) Workspaces(ctx context.Context) ([]outreachsource.Workspace, error) {
	out := []outreachsource.Workspace{{ID: "", Name: s.current.Name}}
	cursor := ""
	for {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}}
		if cursor != "" {
			q.Set("starting_after", cursor)
		}
		var page struct {
			Items []groupMember `json:"items"`
			Next  string        `json:"next_starting_after"`
		}
		if err := s.c.Get(ctx, "/workspace-group-members", q, &page); err != nil {
			// A key without the group scope, or a workspace outside any group, reads only itself.
			var ae *outreachsource.APIError
			if errors.Is(err, outreachsource.ErrForbidden) || errors.Is(err, outreachsource.ErrKey) ||
				(errors.As(err, &ae) && ae.Status == http.StatusNotFound) {
				return out, nil
			}
			return nil, err
		}
		for _, m := range page.Items {
			if m.Status != "accepted" || m.SubWorkspaceID == "" || m.SubWorkspaceID == s.current.ID {
				continue
			}
			name := m.SubWorkspaceID
			if m.SubWorkspaceName != nil && *m.SubWorkspaceName != "" {
				name = *m.SubWorkspaceName
			}
			out = append(out, outreachsource.Workspace{ID: m.SubWorkspaceID, Name: name})
		}
		if cursor = nextCursor(len(page.Items), page.Next); cursor == "" {
			return out, nil
		}
	}
}

func (s *session) Inventory(ctx context.Context, ws outreachsource.Workspace) (outreachsource.Inventory, error) {
	inv := outreachsource.Inventory{Blocklist: -1}
	mailboxes, err := s.listAccounts(ctx, ws, nil)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(mailboxes)
	camps, err := s.listCampaigns(ctx, ws)
	if err != nil {
		return inv, err
	}
	leads, err := s.leadCounts(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, c := range camps {
		n := leads[c.ID]
		inv.Leads += n
		inv.Summaries = append(inv.Summaries, outreachsource.CampaignSummary{
			ID: c.ID, Name: c.Name, Status: campaignStatus(c.Status), Steps: len(c.emailSteps()), Leads: n,
		})
	}
	return inv, nil
}

// nextCursor ends paging on a short page, since Instantly may still hand back a cursor.
func nextCursor(got int, next string) string {
	if got < pageSize {
		return ""
	}
	return next
}

func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
