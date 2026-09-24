package mailvendor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ScaledMail: https://api.scaledmail.com/llms.txt
// Every call names an organization_id, so the client reads all of the key's organizations.
type scaledMail struct {
	t    *transport
	orgs workspaceCache
	// legacyOrg is an organization id a connection saved before organizations were discovered.
	legacyOrg string
	cache     credCache
}

// scaledMailPerSecond is the documented limit; going over it earns a temporary block.
const scaledMailPerSecond = 5

func newScaledMail(vals map[string]string, o options) *scaledMail {
	t := newTransport(VendorScaledMail, "https://server.scaledmail.com/api/v1", o, bearer(vals[FieldAPIKey]), scaledMailPerSecond)
	return &scaledMail{t: t, legacyOrg: vals[FieldOrganizationID]}
}

func (c *scaledMail) Vendor() string { return VendorScaledMail }

// listOrganizations reads GET /organizations. Its response is undocumented, so any list of objects with an id is read.
func (c *scaledMail) listOrganizations(ctx context.Context) ([]workspace, error) {
	var raw json.RawMessage
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/organizations"}, &raw); err != nil {
		return nil, err
	}
	out := scaledMailOrgs(raw)
	if len(out) == 0 && c.legacyOrg != "" {
		out = []workspace{{ID: c.legacyOrg}}
	}
	return out, nil
}

type scaledMailOrg struct {
	ID             string `json:"id"`
	UnderscoreID   string `json:"_id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
}

func (o scaledMailOrg) workspace() workspace {
	return workspace{ID: firstNonEmpty(o.ID, o.OrganizationID, o.UnderscoreID), Name: o.Name}
}

// scaledMailOrgs reads a top-level array, or the arrays and single objects one level inside an object.
func scaledMailOrgs(raw json.RawMessage) []workspace {
	var out []workspace
	seen := map[string]bool{}
	add := func(o scaledMailOrg) {
		if w := o.workspace(); w.ID != "" && !seen[w.ID] {
			seen[w.ID] = true
			out = append(out, w)
		}
	}
	var list []scaledMailOrg
	if json.Unmarshal(raw, &list) == nil {
		for _, o := range list {
			add(o)
		}
		return out
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	for _, v := range obj {
		var inner []scaledMailOrg
		if json.Unmarshal(v, &inner) == nil {
			for _, o := range inner {
				add(o)
			}
			continue
		}
		var one scaledMailOrg
		if json.Unmarshal(v, &one) == nil {
			add(one)
		}
	}
	return out
}

func (c *scaledMail) all(ctx context.Context) ([]workspace, error) {
	orgs, err := c.orgs.get(ctx, c.listOrganizations)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, vendorErr(VendorScaledMail, http.StatusOK, "no organization", ErrNoWorkspace)
	}
	return orgs, nil
}

// Verify reads the organizations, which checks the key and that it reaches one.
func (c *scaledMail) Verify(ctx context.Context) error {
	orgs, err := c.listOrganizations(ctx)
	if err == nil && len(orgs) == 0 {
		err = vendorErr(VendorScaledMail, http.StatusOK, "no organization", ErrNoWorkspace)
	}
	return err
}

type scaledMailDomain struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Redirect string `json:"redirect"`
}

type scaledMailMailbox struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Alias     string `json:"alias"`
	Status    string `json:"status"`
	Email     string `json:"email"`
	OrderType string `json:"order_type"`
	Password  string `json:"mailbox_password"`
}

func (c *scaledMail) domains(ctx context.Context, org string) ([]scaledMailDomain, error) {
	var res struct {
		Domains []scaledMailDomain `json:"domains"`
	}
	q := url.Values{"organization_id": {org}}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/domains", query: q}, &res); err != nil {
		return nil, err
	}
	return res.Domains, nil
}

func (c *scaledMail) mailboxes(ctx context.Context, org, domainID string) ([]scaledMailMailbox, error) {
	var res struct {
		// Null when the domain belongs to another organization.
		Mailboxes []scaledMailMailbox `json:"mailboxes"`
	}
	q := url.Values{"organization_id": {org}, "password": {"true"}}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/mailboxes/" + url.PathEscape(domainID), query: q}, &res); err != nil {
		return nil, err
	}
	return res.Mailboxes, nil
}

func (m scaledMailMailbox) email(domain string) string {
	if m.Email != "" {
		return m.Email
	}
	if m.Alias != "" && domain != "" {
		return m.Alias + "@" + domain
	}
	return ""
}

// List reads each organization's domains, then each domain's mailboxes with their passwords.
func (c *scaledMail) List(ctx context.Context) ([]Mailbox, error) {
	orgs, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Mailbox
	err = eachWorkspace(orgs, func(org workspace) error {
		domains, err := c.domains(ctx, org.ID)
		if err != nil {
			return err
		}
		for _, d := range domains {
			if d.ID == "" {
				continue
			}
			mbs, err := c.mailboxes(ctx, org.ID, d.ID)
			if err != nil {
				return err
			}
			for _, m := range mbs {
				email := m.email(d.Domain)
				if email == "" {
					continue
				}
				// ScaledMail has no mailbox id; the address is the stable key.
				id := scoped(org.ID, strings.ToLower(email))
				c.cache.put(id, Credentials{Password: m.Password})
				out = append(out, Mailbox{
					ID:        id,
					Email:     email,
					FirstName: m.FirstName,
					LastName:  m.LastName,
					Domain:    firstNonEmpty(d.Domain, domainOf(email)),
					Provider:  normalizeProvider(m.OrderType),
					Status:    m.Status,
					Workspace: org.Name,
				})
				if len(out) >= MaxMailboxes {
					return errStop
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Credentials comes from List's cache, else re-reads the mailbox's domain in its organization.
func (c *scaledMail) Credentials(ctx context.Context, m Mailbox) (Credentials, error) {
	if cr, ok := c.cache.get(m.ID); ok {
		return cr, nil
	}
	org, id := unscoped(m.ID)
	addr := strings.ToLower(firstNonEmpty(id, m.Email))
	domain := strings.ToLower(firstNonEmpty(m.Domain, domainOf(addr)))
	if org != "" {
		return c.find(ctx, org, domain, addr)
	}
	orgs, err := c.all(ctx)
	if err != nil {
		return Credentials{}, err
	}
	return findCredentials(VendorScaledMail, orgs, func(o workspace) (Credentials, error) {
		return c.find(ctx, o.ID, domain, addr)
	})
}

func (c *scaledMail) find(ctx context.Context, org, domain, addr string) (Credentials, error) {
	domains, err := c.domains(ctx, org)
	if err != nil {
		return Credentials{}, err
	}
	for _, d := range domains {
		if !strings.EqualFold(d.Domain, domain) || d.ID == "" {
			continue
		}
		mbs, err := c.mailboxes(ctx, org, d.ID)
		if err != nil {
			return Credentials{}, err
		}
		for _, mb := range mbs {
			if strings.EqualFold(mb.email(d.Domain), addr) {
				return Credentials{Password: mb.Password}, nil
			}
		}
	}
	return Credentials{}, vendorErr(VendorScaledMail, http.StatusOK, "not found", ErrNotFound)
}

// DomainCapabilities: ScaledMail's redirect change is a request its team applies; there is no DNS endpoint.
func (c *scaledMail) DomainCapabilities() DomainCapabilities {
	return DomainCapabilities{Forwarding: true, ForwardingRemove: true, ForwardingReviewed: true}
}

// Domains reads GET /domains, which lists one organization's active domains in one call, for each organization.
func (c *scaledMail) Domains(ctx context.Context) ([]Domain, error) {
	orgs, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Domain
	err = eachWorkspace(orgs, func(org workspace) error {
		domains, err := c.domains(ctx, org.ID)
		if err != nil {
			return err
		}
		for _, d := range domains {
			out = append(out, Domain{ID: scoped(org.ID, d.ID), Name: d.Domain, Forwarding: d.Redirect})
			if len(out) >= MaxDomains {
				return errStop
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// withScheme mirrors ScaledMail adding https:// to a redirect given without one.
func withScheme(u string) string {
	u = strings.TrimSpace(u)
	if u != "" && !strings.Contains(u, "://") {
		return "https://" + u
	}
	return u
}

// SetForwarding calls POST /swap-redirect/{domain}, which files a request ScaledMail's team applies later; it returns once accepted.
func (c *scaledMail) SetForwarding(ctx context.Context, d Domain, target string) error {
	target, err := forwardingTarget(VendorScaledMail, target, c.DomainCapabilities())
	if err != nil {
		return err
	}
	name, err := domainName(VendorScaledMail, d)
	if err != nil {
		return err
	}
	// The vendor refuses a request for the redirect a domain already has.
	if strings.EqualFold(strings.TrimRight(withScheme(d.Forwarding), "/"), strings.TrimRight(withScheme(target), "/")) {
		return nil
	}
	org, _ := unscoped(d.ID)
	if org == "" {
		return invalid(VendorScaledMail, "domain id is required")
	}
	q := url.Values{"organization_id": {org}}
	body := map[string]string{"new_redirect": target}
	return c.t.do(ctx, call{method: http.MethodPost, path: "/swap-redirect/" + url.PathEscape(name), query: q, body: body}, nil)
}

// UpsertDNSRecord is refused: ScaledMail documents no DNS record endpoint.
func (c *scaledMail) UpsertDNSRecord(context.Context, Domain, DNSRecord) error {
	return unsupported(VendorScaledMail, "DNS records are not supported")
}
