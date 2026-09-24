package sandbox

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VendorMock plays two inbox vendors for the sandbox, so connecting a vendor
// account, importing its mailboxes and managing its domains work end to end
// with no vendor account. The backend reaches it through MAILVENDOR_SANDBOX_URL,
// at <url>/inboxkit and <url>/mailforge. Any API key is accepted.
//
// InboxKit holds Google Workspace mailboxes (password only, as the real API
// returns) across two workspaces, scopes every call but the workspace list by
// X-Workspace-Id as the real API does, and lets its domains be forwarded and
// their DNS written. Mailforge
// hosts its own SMTP/IMAP, pointed here at the sandbox's mailpit and dovecot so
// its mailboxes import and connect for real, and only forwards domains.
type VendorMock struct {
	mu      sync.Mutex
	domains map[string]*mockDomain // by id
	boxes   []mockBox
	smtp    endpoint
	imap    endpoint
	nextID  int
	latency time.Duration
}

type endpoint struct {
	Host string
	Port int
}

type mockDomain struct {
	ID, Vendor, Workspace, Name, Forwarding string
	Records                                 []mockRecord
}

type mockRecord struct {
	ID    string `json:"_id"`
	Host  string `json:"host"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type mockBox struct {
	ID, Vendor, Workspace, User, Domain, First, Last, Platform string
}

// inboxKitWorkspaces are the mock InboxKit account's workspaces, by uid.
var inboxKitWorkspaces = []struct{ UID, Name string }{
	{"6f1c2d3e-0000-4000-8000-000000000001", "Sunrise Outbound"},
	{"6f1c2d3e-0000-4000-8000-000000000002", "Sunrise Trials"},
}

// NewVendorMock seeds both vendors; smtp and imap are where Mailforge's mailboxes connect.
func NewVendorMock(cfg Config) *VendorMock {
	m := &VendorMock{
		domains: map[string]*mockDomain{},
		smtp:    endpoint{cfg.SMTPHost, cfg.SMTPPort},
		imap:    endpoint{cfg.IMAPHost, cfg.IMAPPort},
		latency: cfg.VendorLatency,
	}
	for _, d := range []mockDomain{
		{ID: "ik-d1", Vendor: "inboxkit", Workspace: inboxKitWorkspaces[0].UID, Name: "sunrise-outbound.test", Forwarding: "https://sunriselabs.test"},
		{ID: "ik-d2", Vendor: "inboxkit", Workspace: inboxKitWorkspaces[1].UID, Name: "trysunrise.test"},
		{ID: "mf-d1", Vendor: "mailforge", Workspace: "mf-ws", Name: "sunrisehq.test"},
	} {
		d := d
		m.domains[d.ID] = &d
	}
	people := [][2]string{{"Ava", "Stone"}, {"Leo", "Park"}, {"Mia", "Chen"}, {"Noah", "Reed"}}
	for i, p := range people {
		m.boxes = append(m.boxes,
			mockBox{ID: "ik-" + strconv.Itoa(i+1), Vendor: "inboxkit", Workspace: inboxKitWorkspaces[0].UID, User: strings.ToLower(p[0]), Domain: "sunrise-outbound.test", First: p[0], Last: p[1], Platform: "google"},
			mockBox{ID: "ik-" + strconv.Itoa(i+11), Vendor: "inboxkit", Workspace: inboxKitWorkspaces[1].UID, User: strings.ToLower(p[0]) + "." + strings.ToLower(p[1]), Domain: "trysunrise.test", First: p[0], Last: p[1], Platform: "google"},
			mockBox{ID: "mf-" + strconv.Itoa(i+1), Vendor: "mailforge", Workspace: "mf-ws", User: strings.ToLower(p[0]), Domain: "sunrisehq.test", First: p[0], Last: p[1], Platform: "smtp"},
		)
	}
	return m
}

func (m *VendorMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		writeMock(w, http.StatusUnauthorized, map[string]any{"error": true, "message": "missing API key"})
		return
	}
	// Real vendor APIs take a moment; answering instantly would hide every loading state.
	select {
	case <-time.After(m.latency):
	case <-r.Context().Done():
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/inboxkit/"):
		m.inboxKit(w, r, strings.TrimPrefix(r.URL.Path, "/inboxkit"))
	case strings.HasPrefix(r.URL.Path, "/mailforge/"):
		m.mailforge(w, r, strings.TrimPrefix(r.URL.Path, "/mailforge"))
	default:
		http.NotFound(w, r)
	}
}

func (m *VendorMock) inboxKit(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if path == "/v1/api/workspaces/list" {
		out := make([]map[string]any, 0, len(inboxKitWorkspaces))
		for _, ws := range inboxKitWorkspaces {
			out = append(out, map[string]any{"uid": ws.UID, "name": ws.Name})
		}
		writeMock(w, http.StatusOK, map[string]any{"error": false, "workspaces": out})
		return
	}
	ws := r.Header.Get("X-Workspace-Id")
	known := false
	for _, k := range inboxKitWorkspaces {
		known = known || k.UID == ws
	}
	if !known {
		writeMock(w, http.StatusBadRequest, map[string]any{"error": true, "message": "Workspace not found"})
		return
	}
	domain := func(uid string) *mockDomain {
		if d := m.domains[uid]; d != nil && d.Vendor == "inboxkit" && d.Workspace == ws {
			return d
		}
		return nil
	}
	switch path {
	case "/v1/api/mailboxes/list":
		var out []map[string]any
		for _, b := range m.boxes {
			if b.Vendor == "inboxkit" && b.Workspace == ws {
				out = append(out, map[string]any{"uid": b.ID, "domain_name": b.Domain, "first_name": b.First, "last_name": b.Last,
					"username": b.User, "platform": b.Platform, "status": "active"})
			}
		}
		writeMock(w, http.StatusOK, map[string]any{"error": false, "mailboxes": out, "pages": 1, "current_page": 1})
	case "/v1/api/mailboxes/show-credentials":
		uid := r.URL.Query().Get("uid")
		for _, b := range m.boxes {
			if b.Vendor == "inboxkit" && b.Workspace == ws && b.ID == uid {
				writeMock(w, http.StatusOK, map[string]any{"error": false, "password": "sandbox", "app_password": ""})
				return
			}
		}
		writeMock(w, http.StatusNotFound, map[string]any{"error": true, "message": "Mailbox not found"})
	case "/v1/api/domains/list":
		var out []map[string]any
		for _, d := range m.vendorDomains("inboxkit") {
			if d.Workspace == ws {
				out = append(out, map[string]any{"uid": d.ID, "name": d.Name, "forwarding_url": d.Forwarding})
			}
		}
		writeMock(w, http.StatusOK, map[string]any{"error": false, "domains": out, "pages": 1})
	case "/v1/api/domains/forwarding":
		target, _ := body["forwarding_url"].(string)
		uids, _ := body["uids"].([]any)
		for _, u := range uids {
			if d := domain(toString(u)); d != nil {
				d.Forwarding = target
			}
		}
		writeMock(w, http.StatusOK, map[string]any{"error": false})
	case "/v1/api/dns/list":
		d := domain(r.URL.Query().Get("uid"))
		if d == nil {
			writeMock(w, http.StatusNotFound, map[string]any{"error": true})
			return
		}
		writeMock(w, http.StatusOK, map[string]any{"error": false, "dns_record": map[string]any{"records": d.Records}})
	case "/v1/api/dns/add", "/v1/api/dns/update", "/v1/api/dns/delete":
		d := domain(toString(body["uid"]))
		if d == nil {
			writeMock(w, http.StatusNotFound, map[string]any{"error": true})
			return
		}
		m.applyDNS(d, path, body)
		writeMock(w, http.StatusOK, map[string]any{"error": false})
	default:
		http.NotFound(w, r)
	}
}

func (m *VendorMock) applyDNS(d *mockDomain, path string, body map[string]any) {
	if path == "/v1/api/dns/delete" {
		drop := map[string]bool{}
		ids, _ := body["record_ids"].([]any)
		for _, id := range ids {
			drop[toString(id)] = true
		}
		kept := d.Records[:0]
		for _, rec := range d.Records {
			if !drop[rec.ID] {
				kept = append(kept, rec)
			}
		}
		d.Records = kept
		return
	}
	recs, _ := body["records"].([]any)
	for _, raw := range recs {
		rec, _ := raw.(map[string]any)
		next := mockRecord{Host: toString(rec["host"]), Type: toString(rec["type"]), Value: toString(rec["value"])}
		if id := toString(rec["record_id"]); id != "" {
			for i := range d.Records {
				if d.Records[i].ID == id {
					next.ID = id
					d.Records[i] = next
				}
			}
			continue
		}
		m.nextID++
		next.ID = "rec-" + strconv.Itoa(m.nextID)
		d.Records = append(d.Records, next)
	}
}

func (m *VendorMock) mailforge(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case path == "/workspaces" && r.Method == http.MethodGet:
		writeMock(w, http.StatusOK, []map[string]any{{"id": "mf-ws", "name": "Sunrise"}})
	case path == "/mailboxes" && r.Method == http.MethodGet:
		var out []map[string]any
		for _, b := range m.boxes {
			if b.Vendor == "mailforge" {
				out = append(out, m.forgeBox(b))
			}
		}
		writeMock(w, http.StatusOK, out)
	case strings.HasPrefix(path, "/mailboxes/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "/mailboxes/")
		for _, b := range m.boxes {
			if b.Vendor == "mailforge" && b.ID == id {
				writeMock(w, http.StatusOK, m.forgeBox(b))
				return
			}
		}
		http.NotFound(w, r)
	case path == "/domains" && r.Method == http.MethodGet:
		var out []map[string]any
		for _, d := range m.vendorDomains("mailforge") {
			sld, tld, _ := strings.Cut(d.Name, ".")
			out = append(out, map[string]any{"id": d.ID, "sld": sld, "tld": tld, "forwardToDomain": d.Forwarding})
		}
		writeMock(w, http.StatusOK, out)
	case path == "/domains/forwards" && r.Method == http.MethodPatch:
		var body []map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, f := range body {
			if d, ok := m.domains[f["domainId"]]; ok && d.Vendor == "mailforge" {
				d.Forwarding = f["forwardToDomain"]
			}
		}
		writeMock(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (m *VendorMock) forgeBox(b mockBox) map[string]any {
	email := b.User + "@" + b.Domain
	return map[string]any{
		"id": b.ID, "email": email, "firstName": b.First, "lastName": b.Last, "domain": b.Domain, "status": "active", "workspaceId": b.Workspace,
		"credentials": map[string]any{
			"imapHost": m.imap.Host, "imapPort": m.imap.Port, "imapUsername": email, "imapPassword": "sandbox",
			"smtpHost": m.smtp.Host, "smtpPort": m.smtp.Port, "smtpUsername": email, "smtpPassword": "sandbox",
		},
	}
}

func (m *VendorMock) vendorDomains(vendor string) []*mockDomain {
	var out []*mockDomain
	for _, id := range []string{"ik-d1", "ik-d2", "mf-d1"} {
		if d := m.domains[id]; d != nil && d.Vendor == vendor {
			out = append(out, d)
		}
	}
	return out
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func writeMock(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
