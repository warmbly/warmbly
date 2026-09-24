package mailvendor

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

// Zapmail: https://docs.zapmail.ai/llms.txt
// Calls are scoped by workspace and by service provider, so the client reads every workspace for both providers.
type zapmail struct {
	t          *transport
	workspaces workspaceCache
	cache      credCache
	domains    domainProvider
}

// zapmailProviders are the service providers Zapmail lists separately.
var zapmailProviders = []string{"GOOGLE", "MICROSOFT"}

const (
	zapmailPageSize = 100
	// zapmailPerSecond is the documented general limit.
	zapmailPerSecond = 5
)

func newZapmail(vals map[string]string, o options) *zapmail {
	key := vals[FieldAPIKey]
	auth := func(h http.Header) { h.Set("x-auth-zapmail", key) }
	return &zapmail{t: newTransport(VendorZapmail, "https://api.zapmail.ai/api", o, auth, zapmailPerSecond)}
}

func (c *zapmail) Vendor() string { return VendorZapmail }

// zapmailEnvelope is Zapmail's wrapper; it can carry an error status inside an HTTP 200.
type zapmailEnvelope struct {
	Status int `json:"status"`
}

func (e zapmailEnvelope) err() error {
	if e.Status == 0 || (e.Status >= 200 && e.Status <= 299) {
		return nil
	}
	return statusErr(VendorZapmail, e.Status)
}

func (c *zapmail) header(ws, provider string) http.Header {
	h := http.Header{}
	if ws != "" {
		h.Set("x-workspace-key", ws)
	}
	if provider != "" {
		h.Set("x-service-provider", provider)
	}
	return h
}

// listWorkspaces pages through GET /v2/workspaces.
func (c *zapmail) listWorkspaces(ctx context.Context) ([]workspace, error) {
	var out []workspace
	for page := 1; page <= maxPages; page++ {
		var res struct {
			zapmailEnvelope
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(zapmailPageSize)}}
		if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v2/workspaces", query: q}, &res); err != nil {
			return nil, err
		}
		if err := res.err(); err != nil {
			return nil, err
		}
		for _, w := range res.Data {
			if w.ID != "" {
				out = append(out, workspace{ID: w.ID, Name: w.Name})
			}
		}
		if len(res.Data) < zapmailPageSize {
			break
		}
	}
	return out, nil
}

// all is every workspace the key reaches; with none listed, calls go to the key's primary workspace.
func (c *zapmail) all(ctx context.Context) ([]workspace, error) {
	wss, err := c.workspaces.get(ctx, c.listWorkspaces)
	if err != nil {
		return nil, err
	}
	if len(wss) == 0 {
		return []workspace{{}}, nil
	}
	return wss, nil
}

// Verify reads the workspace list, which checks the key.
func (c *zapmail) Verify(ctx context.Context) error {
	_, err := c.listWorkspaces(ctx)
	return err
}

type zapmailMailbox struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	FirstName   string  `json:"firstName"`
	LastName    string  `json:"lastName"`
	Password    string  `json:"password"`
	AppPassword *string `json:"appPassword"`
	Status      string  `json:"status"`
	Domain      string  `json:"domain"`
}

func (m zapmailMailbox) credentials() Credentials {
	cr := Credentials{Password: m.Password}
	if m.AppPassword != nil {
		cr.AppPassword = *m.AppPassword
	}
	return cr
}

func (m zapmailMailbox) email() string {
	if m.Email != "" {
		return m.Email
	}
	if m.Username != "" && m.Domain != "" {
		return m.Username + "@" + m.Domain
	}
	return ""
}

// List pages through every workspace's mailboxes, once per service provider; ids carry their workspace.
func (c *zapmail) List(ctx context.Context) ([]Mailbox, error) {
	wss, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Mailbox
	seen := make(map[string]bool)
	err = eachWorkspace(wss, func(ws workspace) error {
		for _, provider := range zapmailProviders {
			for page := 1; page <= maxPages; page++ {
				var res struct {
					zapmailEnvelope
					Data struct {
						CurrentPage int `json:"currentPage"`
						NextPage    int `json:"nextPage"`
						TotalPages  int `json:"totalPages"`
						Domains     []struct {
							Domain    string           `json:"domain"`
							Mailboxes []zapmailMailbox `json:"mailboxes"`
						} `json:"domains"`
					} `json:"data"`
				}
				q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(zapmailPageSize)}}
				if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v2/mailboxes/list", query: q, header: c.header(ws.ID, provider)}, &res); err != nil {
					return err
				}
				if err := res.err(); err != nil {
					return err
				}
				for _, d := range res.Data.Domains {
					for _, m := range d.Mailboxes {
						id := scoped(ws.ID, m.ID)
						if m.ID == "" || seen[id] {
							continue
						}
						seen[id] = true
						c.cache.put(id, m.credentials())
						out = append(out, Mailbox{
							ID:        id,
							Email:     m.email(),
							FirstName: m.FirstName,
							LastName:  m.LastName,
							Domain:    firstNonEmpty(m.Domain, d.Domain),
							Provider:  normalizeProvider(provider),
							Status:    m.Status,
							Workspace: ws.Name,
						})
						if len(out) >= MaxMailboxes {
							return errStop
						}
					}
				}
				more := res.Data.TotalPages > page || (res.Data.TotalPages == 0 && res.Data.NextPage > page)
				if len(res.Data.Domains) == 0 || !more {
					break
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

// Credentials comes from List's cache, else from the mailbox detail endpoint in the mailbox's workspace.
func (c *zapmail) Credentials(ctx context.Context, m Mailbox) (Credentials, error) {
	if cr, ok := c.cache.get(m.ID); ok {
		return cr, nil
	}
	ws, id := unscoped(m.ID)
	if id == "" {
		return Credentials{}, vendorErr(VendorZapmail, 0, "mailbox id is required", ErrNotFound)
	}
	provider := ""
	switch m.Provider {
	case ProviderGoogle:
		provider = "GOOGLE"
	case ProviderMicrosoft:
		provider = "MICROSOFT"
	}
	if ws != "" {
		return c.detail(ctx, ws, provider, id)
	}
	wss, err := c.all(ctx)
	if err != nil {
		return Credentials{}, err
	}
	return findCredentials(VendorZapmail, wss, func(w workspace) (Credentials, error) {
		return c.detail(ctx, w.ID, provider, id)
	})
}

func (c *zapmail) detail(ctx context.Context, ws, provider, id string) (Credentials, error) {
	var res struct {
		zapmailEnvelope
		Data struct {
			Mailbox *zapmailMailbox `json:"mailbox"`
		} `json:"data"`
	}
	q := url.Values{"id": {id}}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v2/mailboxes", query: q, header: c.header(ws, provider)}, &res); err != nil {
		return Credentials{}, err
	}
	if err := res.err(); err != nil {
		return Credentials{}, err
	}
	if res.Data.Mailbox == nil {
		return Credentials{}, vendorErr(VendorZapmail, http.StatusOK, "not found", ErrNotFound)
	}
	return res.Data.Mailbox.credentials(), nil
}

// domainProvider remembers which service provider listed a domain, for the header its later calls send.
type domainProvider struct {
	mu sync.Mutex
	m  map[string]string
}

func (p *domainProvider) put(id, provider string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = make(map[string]string)
	}
	p.m[id] = provider
}

func (p *domainProvider) get(id string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[id]
}

func (c *zapmail) DomainCapabilities() DomainCapabilities {
	return DomainCapabilities{Forwarding: true, ForwardingRemove: true, DNS: true, DNSTypes: allDNSTypes()}
}

// Domains pages through GET /v2/domains in every workspace, once per service provider; ids carry their workspace.
func (c *zapmail) Domains(ctx context.Context) ([]Domain, error) {
	wss, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Domain
	seen := make(map[string]bool)
	err = eachWorkspace(wss, func(ws workspace) error {
		for _, provider := range zapmailProviders {
			for page := 1; page <= maxPages; page++ {
				var res struct {
					zapmailEnvelope
					Data struct {
						NextPage   int `json:"nextPage"`
						TotalPages int `json:"totalPages"`
						Domains    []struct {
							ID        string  `json:"id"`
							Domain    string  `json:"domain"`
							ForwardTo *string `json:"forwardTo"`
						} `json:"domains"`
					} `json:"data"`
				}
				q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(zapmailPageSize)}}
				if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v2/domains", query: q, header: c.header(ws.ID, provider)}, &res); err != nil {
					return err
				}
				if err := res.err(); err != nil {
					return err
				}
				for _, d := range res.Data.Domains {
					id := scoped(ws.ID, d.ID)
					if d.ID == "" || seen[id] {
						continue
					}
					seen[id] = true
					c.domains.put(id, provider)
					dom := Domain{ID: id, Name: d.Domain}
					if d.ForwardTo != nil {
						dom.Forwarding = *d.ForwardTo
					}
					out = append(out, dom)
					if len(out) >= MaxDomains {
						return errStop
					}
				}
				more := res.Data.TotalPages > page || (res.Data.TotalPages == 0 && res.Data.NextPage > page)
				if len(res.Data.Domains) == 0 || !more {
					break
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

// zapmailDomain is a listed domain's workspace, vendor id and the headers its calls send.
func (c *zapmail) zapmailDomain(d Domain) (id string, h http.Header, err error) {
	ws, id := unscoped(d.ID)
	if id == "" {
		return "", nil, invalid(VendorZapmail, "domain id is required")
	}
	h = c.header(ws, c.domains.get(d.ID))
	// The remove-forwarding page names the workspace header x-workspace-id, so both are sent.
	if ws != "" {
		h.Set("x-workspace-id", ws)
	}
	return id, h, nil
}

// SetForwarding calls POST /v2/domains/forwarding, or POST /v2/domains/remove-forwarding for an empty target.
func (c *zapmail) SetForwarding(ctx context.Context, d Domain, target string) error {
	target, err := forwardingTarget(VendorZapmail, target, c.DomainCapabilities())
	if err != nil {
		return err
	}
	id, h, err := c.zapmailDomain(d)
	if err != nil {
		return err
	}
	var cl call
	if target == "" {
		cl = call{method: http.MethodPost, path: "/v2/domains/remove-forwarding", body: map[string]any{"domainId": id}, header: h}
	} else {
		// contains and tagIds are filters that widen the target set, so they are left out: only this domain changes.
		cl = call{method: http.MethodPost, path: "/v2/domains/forwarding", body: map[string]any{"domainIds": []string{id}, "forwardTo": target}, header: h}
	}
	var res zapmailEnvelope
	if err := c.t.do(ctx, cl, &res); err != nil {
		return err
	}
	return res.err()
}

// UpsertDNSRecord reads GET /v2/dns/, then writes through POST, PUT or DELETE /v2/dns. Zapmail takes no TTL.
func (c *zapmail) UpsertDNSRecord(ctx context.Context, d Domain, r DNSRecord) error {
	r, domain, err := prepareRecord(VendorZapmail, d, r, c.DomainCapabilities())
	if err != nil {
		return err
	}
	id, h, err := c.zapmailDomain(d)
	if err != nil {
		return err
	}
	var list struct {
		zapmailEnvelope
		Data struct {
			Records []struct {
				ID         string `json:"id"`
				RecordType string `json:"recordType"`
				Value      string `json:"value"`
				Host       string `json:"host"`
			} `json:"records"`
		} `json:"data"`
	}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v2/dns/", query: url.Values{"id": {id}}, header: h}, &list); err != nil {
		return err
	}
	if err := list.err(); err != nil {
		return err
	}
	existing := make([]existingRecord, len(list.Data.Records))
	for i, e := range list.Data.Records {
		existing[i] = existingRecord{ID: e.ID, Type: e.RecordType, Name: absoluteHost(e.Host, domain), Value: e.Value}
	}
	p := planUpsert(existing, r)
	if p.noop {
		return nil
	}
	if err := needIDs(VendorZapmail, existing, p); err != nil {
		return err
	}
	host := relativeHost(r.Name, domain)
	var cl call
	if p.update >= 0 {
		// The documented body also names a zoneId, which no documented response carries; it is left out.
		cl = call{method: http.MethodPut, path: "/v2/dns", header: h, body: map[string]any{
			"assignedDomainId": id, "dnsRecordId": existing[p.update].ID,
			"host": host, "value": r.Value, "recordType": r.Type,
		}}
	} else {
		cl = call{method: http.MethodPost, path: "/v2/dns", header: h, body: map[string]any{
			"assignedDomainId": id,
			"records":          []map[string]string{{"host": host, "value": r.Value, "recordType": r.Type}},
		}}
	}
	var res zapmailEnvelope
	if err := c.t.do(ctx, cl, &res); err != nil {
		return err
	}
	if err := res.err(); err != nil {
		return err
	}
	for _, idx := range p.remove {
		q := url.Values{"id": {existing[idx].ID}, "assignedDomainId": {id}}
		res = zapmailEnvelope{}
		if err := c.t.do(ctx, call{method: http.MethodDelete, path: "/v2/dns", query: q, header: h}, &res); err != nil {
			return err
		}
		if err := res.err(); err != nil {
			return err
		}
	}
	return nil
}
