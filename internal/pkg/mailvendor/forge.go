package mailvendor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// Mailforge (https://api.mailforge.ai/swagger/doc.json) and Infraforge
// (https://api.infraforge.ai/public/swagger/doc.json) share one API shape.
// The key reaches every workspace of the account, and the mailbox list spans them all.
type forge struct {
	vendor     string
	t          *transport
	cache      credCache
	dnsMu      sync.Mutex
	workspaces workspaceCache
}

func newForge(vendor, base string, vals map[string]string, o options) *forge {
	key := vals[FieldAPIKey]
	// Both take the raw key, with no Bearer prefix.
	auth := func(h http.Header) { h.Set("Authorization", key) }
	return &forge{vendor: vendor, t: newTransport(vendor, base, o, auth, 0)}
}

func (c *forge) Vendor() string { return c.vendor }

// listWorkspaces reads GET /workspaces; a body in another shape yields no names rather than an error.
func (c *forge) listWorkspaces(ctx context.Context) ([]workspace, error) {
	var raw json.RawMessage
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/workspaces"}, &raw); err != nil {
		return nil, err
	}
	var res []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &res)
	out := make([]workspace, 0, len(res))
	for _, w := range res {
		out = append(out, workspace{ID: w.ID, Name: w.Name})
	}
	return out, nil
}

func (c *forge) Verify(ctx context.Context) error {
	return c.t.do(ctx, call{method: http.MethodGet, path: "/workspaces"}, nil)
}

// workspaceNames labels mailboxes by workspace; it is cosmetic, so a failed read leaves them unlabelled.
func (c *forge) workspaceNames(ctx context.Context) map[string]string {
	wss, err := c.workspaces.get(ctx, c.listWorkspaces)
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(wss))
	for _, w := range wss {
		names[w.ID] = w.Name
	}
	return names
}

type forgeMailbox struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Domain      string `json:"domain"`
	Status      string `json:"status"`
	WorkspaceID string `json:"workspaceId"`
	Credentials *struct {
		IMAPHost     string `json:"imapHost"`
		IMAPPort     int    `json:"imapPort"`
		IMAPUsername string `json:"imapUsername"`
		IMAPPassword string `json:"imapPassword"`
		SMTPHost     string `json:"smtpHost"`
		SMTPPort     int    `json:"smtpPort"`
		SMTPUsername string `json:"smtpUsername"`
		SMTPPassword string `json:"smtpPassword"`
	} `json:"credentials"`
}

func (m forgeMailbox) credentials() (Credentials, bool) {
	k := m.Credentials
	if k == nil {
		return Credentials{}, false
	}
	cr := Credentials{
		Password: firstNonEmpty(k.SMTPPassword, k.IMAPPassword),
		SMTP:     endpoint(k.SMTPHost, k.SMTPPort, firstNonEmpty(k.SMTPUsername, m.Email)),
		IMAP:     endpoint(k.IMAPHost, k.IMAPPort, firstNonEmpty(k.IMAPUsername, m.Email)),
	}
	if cr.SMTP != nil && k.SMTPPassword != "" && k.SMTPPassword != cr.Password {
		cr.SMTP.Password = k.SMTPPassword
	}
	if cr.IMAP != nil && k.IMAPPassword != "" && k.IMAPPassword != cr.Password {
		cr.IMAP.Password = k.IMAPPassword
	}
	return cr, cr.Password != "" || cr.SMTP != nil || cr.IMAP != nil
}

func (m forgeMailbox) mailbox() Mailbox {
	return Mailbox{
		ID:        m.ID,
		Email:     m.Email,
		FirstName: m.FirstName,
		LastName:  m.LastName,
		Domain:    firstNonEmpty(m.Domain, domainOf(m.Email)),
		// Both vendors host the mailboxes on their own SMTP/IMAP servers.
		Provider: ProviderSMTP,
		Status:   m.Status,
	}
}

// List reads every workspace's mailboxes with credentials in one unpaginated call.
func (c *forge) List(ctx context.Context) ([]Mailbox, error) {
	q := url.Values{"with_credentials": {"true"}}
	var res []forgeMailbox
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/mailboxes", query: q, limit: listBodyLimit}, &res); err != nil {
		return nil, err
	}
	names := c.workspaceNames(ctx)
	out := make([]Mailbox, 0, min(len(res), MaxMailboxes))
	for _, m := range res {
		if m.ID == "" {
			continue
		}
		if cr, ok := m.credentials(); ok {
			c.cache.put(m.ID, cr)
		}
		mb := m.mailbox()
		mb.Workspace = names[m.WorkspaceID]
		out = append(out, mb)
		if len(out) >= MaxMailboxes {
			break
		}
	}
	return out, nil
}

// Credentials comes from List's cache, else from the single-mailbox endpoint.
func (c *forge) Credentials(ctx context.Context, m Mailbox) (Credentials, error) {
	if cr, ok := c.cache.get(m.ID); ok {
		return cr, nil
	}
	if m.ID == "" {
		return Credentials{}, vendorErr(c.vendor, 0, "mailbox id is required", ErrNotFound)
	}
	var res forgeMailbox
	q := url.Values{"with_credentials": {"true"}}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/mailboxes/" + url.PathEscape(strings.TrimSpace(m.ID)), query: q}, &res); err != nil {
		return Credentials{}, err
	}
	cr, _ := res.credentials()
	return cr, nil
}

func (c *forge) DomainCapabilities() DomainCapabilities {
	// No endpoint removes a forward; A/AAAA stay out since the root's record name is undocumented.
	return DomainCapabilities{Forwarding: true, DNS: true, DNSTypes: []string{DNSTypeCNAME, DNSTypeTXT}}
}

type forgeDomain struct {
	ID              string `json:"id"`
	SLD             string `json:"sld"`
	TLD             string `json:"tld"`
	ForwardToDomain string `json:"forwardToDomain"`
}

// Domains reads GET /domains in one unpaginated call.
func (c *forge) Domains(ctx context.Context) ([]Domain, error) {
	var raw json.RawMessage
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/domains", limit: listBodyLimit}, &raw); err != nil {
		return nil, err
	}
	var res []forgeDomain
	// Mailforge documents an array; Infraforge's spec shows a single object, so both are read.
	if err := json.Unmarshal(raw, &res); err != nil {
		var one forgeDomain
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, vendorErr(c.vendor, http.StatusOK, "malformed response", nil)
		}
		if one.ID != "" {
			res = []forgeDomain{one}
		}
	}
	out := make([]Domain, 0, min(len(res), MaxDomains))
	for _, d := range res {
		if d.ID == "" {
			continue
		}
		name := d.SLD
		if d.TLD != "" {
			name += "." + strings.TrimPrefix(d.TLD, ".")
		}
		out = append(out, Domain{ID: d.ID, Name: name, Forwarding: d.ForwardToDomain})
		if len(out) >= MaxDomains {
			break
		}
	}
	return out, nil
}

// SetForwarding calls PATCH /domains/forwards. domainMasking is omitted, which the docs ask for on a new forward.
func (c *forge) SetForwarding(ctx context.Context, d Domain, target string) error {
	target, err := forwardingTarget(c.vendor, target, c.DomainCapabilities())
	if err != nil {
		return err
	}
	if d.ID == "" {
		return invalid(c.vendor, "domain id is required")
	}
	body := []map[string]string{{"domainId": d.ID, "forwardToDomain": target}}
	return c.t.do(ctx, call{method: http.MethodPatch, path: "/domains/forwards", body: body}, nil)
}

type forgeRecord struct {
	Editable *bool  `json:"editable,omitempty"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
}

// UpsertDNSRecord reads GET /domains/{id}/dns and PUTs the whole set back (the PUT replaces every record),
// in listed order as Infraforge asks, new record appended, nothing dropped.
func (c *forge) UpsertDNSRecord(ctx context.Context, d Domain, r DNSRecord) error {
	r, domain, err := prepareRecord(c.vendor, d, r, c.DomainCapabilities())
	if err != nil {
		return err
	}
	if d.ID == "" {
		return invalid(c.vendor, "domain id is required")
	}
	// A read-modify-write of the whole set; serialize it so two upserts from this client cannot lose each other.
	c.dnsMu.Lock()
	defer c.dnsMu.Unlock()
	path := "/domains/" + url.PathEscape(d.ID) + "/dns"
	var records []forgeRecord
	if err := c.t.do(ctx, call{method: http.MethodGet, path: path}, &records); err != nil {
		return err
	}
	existing := make([]existingRecord, len(records))
	for i, e := range records {
		existing[i] = existingRecord{Type: e.Type, Name: absoluteHost(e.Name, domain), Value: e.Value, Locked: e.Editable != nil && !*e.Editable}
	}
	p := planUpsert(existing, r)
	if p.noop {
		return nil
	}
	p.remove = nil
	if err := checkPlan(c.vendor, existing, p); err != nil {
		return err
	}
	next := slices.Clone(records)
	if next == nil {
		next = []forgeRecord{}
	}
	if p.update >= 0 {
		next[p.update].Value = r.Value
	} else {
		next = append(next, forgeRecord{Name: relativeHost(r.Name, domain), Type: r.Type, Value: r.Value})
	}
	return c.t.do(ctx, call{method: http.MethodPut, path: path, body: map[string]any{"records": next}}, nil)
}
