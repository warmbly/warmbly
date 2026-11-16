// Package smartlead reads a Smartlead account through its public API
// (https://api.smartlead.ai) into the neutral outreachsource types.
package smartlead

import (
	"bytes"
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
	id          source.ProviderID = "smartlead"
	label                         = "Smartlead"
	defaultBase                   = "https://server.smartlead.ai/api/v1"
	// accountName names the single workspace of an account without clients.
	accountName = "Smartlead account"
	// unassignedName names the campaigns and mailboxes no client owns.
	unassignedName = "Not assigned to a client"
)

type provider struct {
	base string
	// sleep replaces the client's retry wait in tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns the Smartlead provider.
func New() source.Provider { return newProvider(defaultBase) }

// NewWithBaseURL returns the Smartlead provider reading from another API root, such as the dev sandbox's mock.
func NewWithBaseURL(base string) source.Provider { return newProvider(strings.TrimRight(base, "/")) }

func newProvider(base string) *provider { return &provider{base: base} }

func (p *provider) Info() source.Info {
	return source.Info{
		ID:    id,
		Label: label,
		Fields: []source.Field{{
			Key:    "api_key",
			Label:  "API key",
			Help:   "Use the main account's key, not a client key, to see every client.",
			Secret: true,
		}},
		KeyHelp:        "In Smartlead, click your profile icon, open Settings and copy the key from the API Keys section.",
		Kinds:          []source.Kind{source.KindBlocklist, source.KindMailboxes, source.KindCampaigns, source.KindLeads},
		Workspaces:     true,
		MailboxSecrets: true,
		Notes: []string{
			"Each Smartlead client becomes its own workspace.",
			"SMTP mailboxes come over with their passwords. Google and Microsoft mailboxes are signed in again.",
			"Leads that are only in a lead list, outside every campaign, cannot be read through Smartlead's API.",
			"Subsequences come over as separate campaigns, without the reply categories that moved leads into them.",
		},
	}
}

func (p *provider) Open(ctx context.Context, creds source.Credentials) (source.Session, error) {
	key := strings.TrimSpace(creds["api_key"])
	if key == "" {
		return nil, fmt.Errorf("%s: %w", label, source.ErrKey)
	}
	c := source.NewClient(label, p.base, func(r *http.Request) {
		q := r.URL.Query()
		q.Set("api_key", key)
		r.URL.RawQuery = q.Encode()
	})
	if p.sleep != nil {
		c.Sleep = p.sleep
	}
	s := &session{c: c, leadTotals: map[string]int{}, progress: map[string]sent{}}
	// Categories are cheap, every key can read them and Leads needs them.
	if err := s.loadCategories(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

type session struct {
	c *source.Client

	mu         sync.Mutex
	categories map[string]category
	clients    []client
	// single is true when the key sees no clients, so nothing is filtered by client.
	single     bool
	clientsOK  bool
	campaigns  []campaign
	campOK     bool
	accounts   []account
	accountsOK bool
	leadTotals map[string]int
	progress   map[string]sent
}

// Account is empty: Smartlead's API has no endpoint naming the key's owner.
func (s *session) Account(context.Context) (source.Account, error) {
	return source.Account{}, nil
}

type client struct {
	ID   num    `json:"id"`
	Name string `json:"name"`
}

func (s *session) loadClients(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clientsOK {
		return nil
	}
	var raw json.RawMessage
	err := s.c.Get(ctx, "/client/", nil, &raw)
	var ae *source.APIError
	switch {
	case errors.Is(err, source.ErrForbidden), errors.As(err, &ae):
		// A client key cannot list clients; it reads its own client only.
		s.clients, s.single, s.clientsOK = nil, true, true
		return nil
	case err != nil:
		return err
	}
	var list []client
	if err := decodeList(raw, &list, "clients"); err != nil {
		return fmt.Errorf("%s: decode clients: %w", label, err)
	}
	s.clients, s.single, s.clientsOK = list, len(list) == 0, true
	return nil
}

func (s *session) Workspaces(ctx context.Context) ([]source.Workspace, error) {
	if err := s.loadClients(ctx); err != nil {
		return nil, err
	}
	if s.single {
		return []source.Workspace{{ID: "", Name: accountName}}, nil
	}
	out := make([]source.Workspace, 0, len(s.clients)+1)
	for _, c := range s.clients {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "Client " + c.ID.String()
		}
		out = append(out, source.Workspace{ID: c.ID.String(), Name: name})
	}
	unassigned, err := s.hasUnassigned(ctx)
	if err != nil {
		return nil, err
	}
	if unassigned {
		out = append(out, source.Workspace{ID: "", Name: unassignedName})
	}
	return out, nil
}

func (s *session) hasUnassigned(ctx context.Context) (bool, error) {
	camps, err := s.allCampaigns(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range camps {
		if c.ClientID == "" {
			return true, nil
		}
	}
	accs, err := s.allAccounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range accs {
		if a.ClientID == "" {
			return true, nil
		}
	}
	return false, nil
}

// owns reports whether a row with this client id belongs to the workspace.
func (s *session) owns(ctx context.Context, ws source.Workspace, clientID string) (bool, error) {
	if err := s.loadClients(ctx); err != nil {
		return false, err
	}
	if s.single {
		return true, nil
	}
	return clientID == ws.ID, nil
}

func (s *session) Inventory(ctx context.Context, ws source.Workspace) (source.Inventory, error) {
	inv := source.Inventory{Blocklist: -1}
	accs, err := s.accountsFor(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Mailboxes = len(accs)
	camps, err := s.campaignsFor(ctx, ws)
	if err != nil {
		return inv, err
	}
	inv.Campaigns = len(camps)
	for _, c := range camps {
		n, err := s.leadTotal(ctx, c.ID.String())
		if err != nil {
			return inv, err
		}
		inv.Leads += n
		// Steps would cost a request per campaign on a tight rate limit.
		inv.Summaries = append(inv.Summaries, source.CampaignSummary{
			ID: c.ID.String(), Name: c.Name, Status: campaignStatus(c.Status), Steps: -1, Leads: n,
		})
	}
	return inv, nil
}

type blockEntry struct {
	Value    string `json:"email_or_domain"`
	Source   string `json:"source"`
	ClientID num    `json:"client_id"`
}

const blockPage = 1000

func (s *session) Blocklist(ctx context.Context, ws source.Workspace, cursor string) ([]source.BlockEntry, string, error) {
	off, err := offset(cursor)
	if err != nil {
		return nil, "", err
	}
	if err := s.loadClients(ctx); err != nil {
		return nil, "", err
	}
	var raw json.RawMessage
	q := url.Values{"offset": {strconv.Itoa(off)}, "limit": {strconv.Itoa(blockPage)}}
	if err := s.c.Get(ctx, "/leads/get-domain-block-list", q, &raw); err != nil {
		return nil, "", err
	}
	var rows []blockEntry
	if err := decodeList(raw, &rows, "data"); err != nil {
		return nil, "", fmt.Errorf("%s: decode block list: %w", label, err)
	}
	var out []source.BlockEntry
	for _, r := range rows {
		// An entry with no client blocks the address for every client.
		if !s.single && r.ClientID != "" && r.ClientID.String() != ws.ID {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(r.Value))
		v = strings.TrimPrefix(strings.TrimPrefix(v, "*."), "@")
		if v == "" {
			continue
		}
		out = append(out, source.BlockEntry{Value: v, Domain: !strings.Contains(v, "@"), Reason: strings.TrimSpace(r.Source)})
	}
	next := ""
	if len(rows) == blockPage {
		next = strconv.Itoa(off + len(rows))
	}
	return out, next, nil
}

// num is an id or count Smartlead sends as a number, a string or null.
type num string

func (n *num) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*n = ""
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = num(strings.TrimSpace(s))
	default:
		*n = num(b)
	}
	return nil
}

func (n num) String() string { return string(n) }

func (n num) Int() int {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0
	}
	return int(f)
}

// stamp is a time Smartlead writes as RFC 3339 or as "2006-01-02 15:04:05".
type stamp struct{ time.Time }

func (t *stamp) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) != nil || strings.TrimSpace(s) == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999Z07:00", "2006-01-02 15:04:05.999", "2006-01-02"} {
		if v, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			t.Time = v.UTC()
			return nil
		}
	}
	return nil
}

func (t stamp) ptr() *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.Time
	return &v
}

// decodeList reads a list Smartlead returns bare or wrapped in an object
// under "data" or the given key; the docs show both shapes.
func decodeList(raw json.RawMessage, out any, key string) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '[' {
		return json.Unmarshal(raw, out)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	for _, k := range []string{key, "data"} {
		v := bytes.TrimSpace(obj[k])
		if len(v) == 0 {
			continue
		}
		if v[0] == '[' {
			return json.Unmarshal(v, out)
		}
		if v[0] == '{' {
			return decodeList(v, out, key)
		}
	}
	return nil
}

// decodeOne reads an object Smartlead returns bare or wrapped under "data".
func decodeOne(raw json.RawMessage, out any) error {
	var w struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &w) == nil && len(bytes.TrimSpace(w.Data)) > 0 && bytes.TrimSpace(w.Data)[0] == '{' {
		return json.Unmarshal(w.Data, out)
	}
	return json.Unmarshal(raw, out)
}

func offset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: invalid cursor %q", label, cursor)
	}
	return n, nil
}
