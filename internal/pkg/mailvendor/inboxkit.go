package mailvendor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// InboxKit: https://docs.inboxkit.com/llms.txt
// Every call but the workspace list is scoped by X-Workspace-Id, so the client reads all of the key's workspaces.
type inboxKit struct {
	t          *transport
	workspaces workspaceCache
}

const inboxKitPageSize = 100

func newInboxKit(vals map[string]string, o options) *inboxKit {
	return &inboxKit{t: newTransport(VendorInboxKit, "https://api.inboxkit.com", o, bearer(vals[FieldAPIKey]), 0)}
}

func (c *inboxKit) Vendor() string { return VendorInboxKit }

func inboxKitHeader(ws string) http.Header {
	h := http.Header{}
	h.Set("X-Workspace-Id", ws)
	return h
}

// listWorkspaces reads GET /v1/api/workspaces/list, the one call the key alone answers.
func (c *inboxKit) listWorkspaces(ctx context.Context) ([]workspace, error) {
	var res struct {
		inboxKitEnvelope
		Workspaces []struct {
			UID  string `json:"uid"`
			Name string `json:"name"`
		} `json:"workspaces"`
	}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v1/api/workspaces/list"}, &res); err != nil {
		return nil, err
	}
	if err := res.err(); err != nil {
		return nil, err
	}
	out := make([]workspace, 0, len(res.Workspaces))
	for _, w := range res.Workspaces {
		if w.UID != "" {
			out = append(out, workspace{ID: w.UID, Name: w.Name})
		}
	}
	return out, nil
}

// all is every workspace the key reaches; a key that reaches none can read nothing.
func (c *inboxKit) all(ctx context.Context) ([]workspace, error) {
	wss, err := c.workspaces.get(ctx, c.listWorkspaces)
	if err != nil {
		return nil, err
	}
	if len(wss) == 0 {
		return nil, vendorErr(VendorInboxKit, http.StatusOK, "no workspace", ErrNoWorkspace)
	}
	return wss, nil
}

type inboxKitList struct {
	Error     bool `json:"error"`
	Mailboxes []struct {
		UID        string `json:"uid"`
		DomainName string `json:"domain_name"`
		FirstName  string `json:"first_name"`
		LastName   string `json:"last_name"`
		Username   string `json:"username"`
		Platform   string `json:"platform"`
		Status     string `json:"status"`
	} `json:"mailboxes"`
	Pages       int `json:"pages"`
	CurrentPage int `json:"current_page"`
}

func (c *inboxKit) page(ctx context.Context, ws string, page, limit int) (inboxKitList, error) {
	var out inboxKitList
	body := map[string]any{"page": page, "limit": limit}
	if err := c.t.do(ctx, call{method: http.MethodPost, path: "/v1/api/mailboxes/list", body: body, header: inboxKitHeader(ws)}, &out); err != nil {
		return out, err
	}
	if out.Error {
		return out, vendorErr(VendorInboxKit, http.StatusOK, "vendor reported an error", nil)
	}
	return out, nil
}

// Verify reads the workspace list, which checks the key and that it reaches one.
func (c *inboxKit) Verify(ctx context.Context) error {
	wss, err := c.listWorkspaces(ctx)
	if err == nil && len(wss) == 0 {
		err = vendorErr(VendorInboxKit, http.StatusOK, "no workspace", ErrNoWorkspace)
	}
	return err
}

// List pages through every workspace's mailboxes; ids carry their workspace.
func (c *inboxKit) List(ctx context.Context) ([]Mailbox, error) {
	wss, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Mailbox
	err = eachWorkspace(wss, func(ws workspace) error {
		for page := 1; page <= maxPages; page++ {
			res, err := c.page(ctx, ws.ID, page, inboxKitPageSize)
			if err != nil {
				return err
			}
			for _, m := range res.Mailboxes {
				email := m.Username
				if m.DomainName != "" {
					email = m.Username + "@" + m.DomainName
				}
				out = append(out, Mailbox{
					ID:        scoped(ws.ID, m.UID),
					Email:     email,
					FirstName: m.FirstName,
					LastName:  m.LastName,
					Domain:    m.DomainName,
					Provider:  normalizeProvider(m.Platform),
					Status:    m.Status,
					Workspace: ws.Name,
				})
				if len(out) >= MaxMailboxes {
					return errStop
				}
			}
			if len(res.Mailboxes) == 0 || page >= res.Pages {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Credentials reads show-credentials in the mailbox's workspace; an id stored without one is looked up in each.
func (c *inboxKit) Credentials(ctx context.Context, m Mailbox) (Credentials, error) {
	ws, uid := unscoped(m.ID)
	if ws != "" {
		return c.show(ctx, ws, uid, m.Email)
	}
	wss, err := c.all(ctx)
	if err != nil {
		return Credentials{}, err
	}
	return findCredentials(VendorInboxKit, wss, func(w workspace) (Credentials, error) {
		return c.show(ctx, w.ID, uid, m.Email)
	})
}

func (c *inboxKit) show(ctx context.Context, ws, uid, email string) (Credentials, error) {
	q := url.Values{}
	if uid != "" {
		q.Set("uid", uid)
	} else {
		q.Set("email", email)
	}
	var res struct {
		Error       bool   `json:"error"`
		Password    string `json:"password"`
		AppPassword string `json:"app_password"`
	}
	if err := c.t.do(ctx, call{method: http.MethodGet, path: "/v1/api/mailboxes/show-credentials", query: q, header: inboxKitHeader(ws)}, &res); err != nil {
		return Credentials{}, err
	}
	// An error envelope here means the workspace does not hold the mailbox.
	if res.Error {
		return Credentials{}, vendorErr(VendorInboxKit, http.StatusOK, "not found", ErrNotFound)
	}
	return Credentials{Password: res.Password, AppPassword: res.AppPassword}, nil
}

const inboxKitDomainPageSize = 100

// inboxKitEnvelope carries InboxKit's error flag, which can be set inside an HTTP 200.
type inboxKitEnvelope struct {
	Error bool `json:"error"`
}

func (e inboxKitEnvelope) err() error {
	if e.Error {
		return vendorErr(VendorInboxKit, http.StatusOK, "vendor reported an error", nil)
	}
	return nil
}

func (c *inboxKit) DomainCapabilities() DomainCapabilities {
	return DomainCapabilities{Forwarding: true, ForwardingRemove: true, DNS: true, DNSTypes: allDNSTypes()}
}

// Domains pages through POST /v1/api/domains/list in every workspace; ids carry their workspace.
func (c *inboxKit) Domains(ctx context.Context) ([]Domain, error) {
	wss, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Domain
	err = eachWorkspace(wss, func(ws workspace) error {
		for page := 1; page <= maxPages; page++ {
			var res struct {
				inboxKitEnvelope
				Domains []struct {
					UID           string `json:"uid"`
					Name          string `json:"name"`
					TLD           string `json:"tld"`
					ForwardingURL string `json:"forwarding_url"`
				} `json:"domains"`
				Pages int `json:"pages"`
			}
			body := map[string]any{"page": page, "limit": inboxKitDomainPageSize}
			if err := c.t.do(ctx, call{method: http.MethodPost, path: "/v1/api/domains/list", body: body, header: inboxKitHeader(ws.ID)}, &res); err != nil {
				return err
			}
			if err := res.err(); err != nil {
				return err
			}
			for _, d := range res.Domains {
				name := d.Name
				// The Domain schema describes name as the label without its TLD; the list example carries both.
				if name != "" && !strings.Contains(name, ".") && d.TLD != "" {
					name += "." + strings.TrimPrefix(d.TLD, ".")
				}
				out = append(out, Domain{ID: scoped(ws.ID, d.UID), Name: name, Forwarding: d.ForwardingURL})
				if len(out) >= MaxDomains {
					return errStop
				}
			}
			if len(res.Domains) == 0 || page >= res.Pages {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// inboxKitDomain splits a listed domain's id into its workspace and uid.
func inboxKitDomain(d Domain) (ws, uid string, err error) {
	ws, uid = unscoped(d.ID)
	if ws == "" || uid == "" {
		return "", "", invalid(VendorInboxKit, "domain id is required")
	}
	return ws, uid, nil
}

// SetForwarding calls POST /v1/api/domains/forwarding; an empty forwarding_url removes it.
func (c *inboxKit) SetForwarding(ctx context.Context, d Domain, target string) error {
	target, err := forwardingTarget(VendorInboxKit, target, c.DomainCapabilities())
	if err != nil {
		return err
	}
	ws, uid, err := inboxKitDomain(d)
	if err != nil {
		return err
	}
	var res inboxKitEnvelope
	body := map[string]any{"uids": []string{uid}, "forwarding_url": target}
	if err := c.t.do(ctx, call{method: http.MethodPost, path: "/v1/api/domains/forwarding", body: body, header: inboxKitHeader(ws)}, &res); err != nil {
		return err
	}
	return res.err()
}

type inboxKitRecord struct {
	// ID is not in the documented list schema; record ids are the store's _id, which update and delete take.
	ID       string `json:"_id,omitempty"`
	RecordID string `json:"record_id,omitempty"`
	Host     string `json:"host"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      *int   `json:"ttl,omitempty"`
}

// inboxKitTarget names the domain by uid.
func inboxKitTarget(uid string) map[string]any {
	return map[string]any{"uid": uid}
}

// UpsertDNSRecord reads /v1/api/dns/list, then adds, updates or deletes through /v1/api/dns/{add,update,delete}.
func (c *inboxKit) UpsertDNSRecord(ctx context.Context, d Domain, r DNSRecord) error {
	r, domain, err := prepareRecord(VendorInboxKit, d, r, c.DomainCapabilities())
	if err != nil {
		return err
	}
	ws, uid, err := inboxKitDomain(d)
	if err != nil {
		return err
	}
	h := inboxKitHeader(ws)
	q := url.Values{"uid": {uid}}
	var list struct {
		inboxKitEnvelope
		DNSRecord struct {
			Records []inboxKitRecord `json:"records"`
		} `json:"dns_record"`
	}
	err = c.t.do(ctx, call{method: http.MethodGet, path: "/v1/api/dns/list", query: q, header: h}, &list)
	// A 404 also means "no records yet"; a missing domain fails again on the write.
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil {
		if err := list.err(); err != nil {
			return err
		}
	}
	existing := make([]existingRecord, len(list.DNSRecord.Records))
	for i, e := range list.DNSRecord.Records {
		existing[i] = existingRecord{ID: firstNonEmpty(e.ID, e.RecordID), Type: e.Type, Name: absoluteHost(e.Host, domain), Value: e.Value}
	}
	p := planUpsert(existing, r)
	if p.noop {
		return nil
	}
	if err := needIDs(VendorInboxKit, existing, p); err != nil {
		return err
	}
	rec := inboxKitRecord{Host: relativeHost(r.Name, domain), Type: r.Type, Value: r.Value, TTL: ttlOrNil(r.TTL)}
	path := "/v1/api/dns/add"
	if p.update >= 0 {
		path = "/v1/api/dns/update"
		rec.RecordID = existing[p.update].ID
	}
	body := inboxKitTarget(uid)
	body["records"] = []inboxKitRecord{rec}
	var res inboxKitEnvelope
	if err := c.t.do(ctx, call{method: http.MethodPost, path: path, body: body, header: h}, &res); err != nil {
		return err
	}
	if err := res.err(); err != nil {
		return err
	}
	if len(p.remove) == 0 {
		return nil
	}
	ids := make([]string, len(p.remove))
	for i, idx := range p.remove {
		ids[i] = existing[idx].ID
	}
	del := inboxKitTarget(uid)
	del["record_ids"] = ids
	res = inboxKitEnvelope{}
	if err := c.t.do(ctx, call{method: http.MethodPost, path: "/v1/api/dns/delete", body: del, header: h}, &res); err != nil {
		return err
	}
	return res.err()
}
