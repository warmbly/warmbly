package mailvendor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func domainManager(t *testing.T, c Client) DomainManager {
	t.Helper()
	dm, ok := c.(DomainManager)
	if !ok {
		t.Fatalf("%s does not implement DomainManager", c.Vendor())
	}
	return dm
}

func decodeBody(t *testing.T, r seenRequest) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("%s %s body %q: %v", r.Method, r.Path, r.Body, err)
	}
	return m
}

// testDomain is a domain every vendor's calls accept.
var testDomain = Domain{ID: "7", Name: "acme.io"}

// testDomainFor is testDomain as the vendor lists it; workspace-scoped vendors carry the workspace in the id.
func testDomainFor(vendor string) Domain {
	if vendor == VendorInboxKit || vendor == VendorScaledMail || vendor == VendorZapmail {
		return Domain{ID: "ws-1:7", Name: "acme.io"}
	}
	return testDomain
}

func TestDomainCapabilitiesAllVendors(t *testing.T) {
	want := map[string]DomainCapabilities{
		VendorInboxKit:     {Forwarding: true, ForwardingRemove: true, DNS: true, DNSTypes: allDNSTypes()},
		VendorZapmail:      {Forwarding: true, ForwardingRemove: true, DNS: true, DNSTypes: allDNSTypes()},
		VendorMailforge:    {Forwarding: true, DNS: true, DNSTypes: []string{DNSTypeCNAME, DNSTypeTXT}},
		VendorInfraforge:   {Forwarding: true, DNS: true, DNSTypes: []string{DNSTypeCNAME, DNSTypeTXT}},
		VendorMaildoso:     {Forwarding: true, ForwardingRemove: true},
		VendorCheapInboxes: {Forwarding: true, ForwardingRemove: true, DNS: true, DNSTypes: allDNSTypes()},
		VendorScaledMail:   {Forwarding: true, ForwardingRemove: true, ForwardingReviewed: true},
	}
	for _, d := range Descriptors() {
		c, err := New(d.ID, fieldsFor(d.ID))
		if err != nil {
			t.Fatal(err)
		}
		got := domainManager(t, c).DomainCapabilities()
		if !reflect.DeepEqual(got, want[d.ID]) {
			t.Errorf("%s: capabilities = %+v, want %+v", d.ID, got, want[d.ID])
		}
		if got.DNS != (len(got.DNSTypes) > 0) {
			t.Errorf("%s: DNS %v disagrees with types %v", d.ID, got.DNS, got.DNSTypes)
		}
	}
}

// Every domain call maps a rejected key to ErrUnauthorized and never echoes the key.
func TestDomainCallsUnauthorizedAllVendors(t *testing.T) {
	for _, d := range Descriptors() {
		srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusUnauthorized, `{"message":"invalid api key `+testKey+`"}`)
		})
		dm := domainManager(t, newTestClient(t, d.ID, fieldsFor(d.ID), srv.URL, nil))
		runs := map[string]func() error{
			"domains":    func() error { _, err := dm.Domains(context.Background()); return err },
			"forwarding": func() error { return dm.SetForwarding(context.Background(), testDomainFor(d.ID), "https://acme.com") },
		}
		if dm.DomainCapabilities().DNS {
			runs["dns"] = func() error {
				return dm.UpsertDNSRecord(context.Background(), testDomainFor(d.ID), DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=abc"})
			}
		}
		for name, run := range runs {
			err := run()
			if !errors.Is(err, ErrUnauthorized) {
				t.Errorf("%s %s: err = %v, want ErrUnauthorized", d.ID, name, err)
				continue
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), srv.URL) {
				t.Errorf("%s %s: error leaks the key or URL: %q", d.ID, name, err)
			}
		}
	}
}

func TestDNSUnsupportedVendorsSendNothing(t *testing.T) {
	for _, vendor := range []string{VendorMaildoso, VendorScaledMail} {
		srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, `{}`) })
		dm := domainManager(t, newTestClient(t, vendor, fieldsFor(vendor), srv.URL, nil))
		err := dm.UpsertDNSRecord(context.Background(), testDomain, DNSRecord{Type: "A", Name: "acme.io", Value: "192.0.2.1"})
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", vendor, err)
		}
		if n := len(srv.requests()); n != 0 {
			t.Errorf("%s: sent %d requests", vendor, n)
		}
	}
}

func TestPrepareRecordValidation(t *testing.T) {
	caps := DomainCapabilities{DNS: true, DNSTypes: allDNSTypes()}
	ok := []DNSRecord{
		{Type: "cname", Name: "Track.ACME.io.", Value: "track.warmbly.com."},
		{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=abc"},
		{Type: "A", Name: "acme.io", Value: "192.0.2.1", TTL: 300},
		{Type: "AAAA", Name: "acme.io", Value: "2001:db8::1"},
	}
	for _, r := range ok {
		if _, _, err := prepareRecord("v", testDomain, r, caps); err != nil {
			t.Errorf("prepareRecord(%+v): %v", r, err)
		}
	}
	got, domain, _ := prepareRecord("v", testDomain, ok[0], caps)
	if domain != "acme.io" || got != (DNSRecord{Type: "CNAME", Name: "track.acme.io", Value: "track.warmbly.com"}) {
		t.Fatalf("normalized = %+v, %q", got, domain)
	}
	bad := map[string]DNSRecord{
		"outside domain":  {Type: "TXT", Name: "evil.com", Value: "x"},
		"suffix trick":    {Type: "TXT", Name: "notacme.io", Value: "x"},
		"empty value":     {Type: "TXT", Name: "acme.io", Value: " "},
		"control value":   {Type: "TXT", Name: "acme.io", Value: "a\nb"},
		"cname at root":   {Type: "CNAME", Name: "acme.io", Value: "track.warmbly.com"},
		"cname url":       {Type: "CNAME", Name: "t.acme.io", Value: "https://x.com/"},
		"a not ipv4":      {Type: "A", Name: "acme.io", Value: "2001:db8::1"},
		"aaaa not ipv6":   {Type: "AAAA", Name: "acme.io", Value: "192.0.2.1"},
		"negative ttl":    {Type: "A", Name: "acme.io", Value: "192.0.2.1", TTL: -1},
		"space in name":   {Type: "TXT", Name: "a b.acme.io", Value: "x"},
		"unsupported":     {Type: "MX", Name: "acme.io", Value: "mx.acme.io"},
		"oversized value": {Type: "TXT", Name: "acme.io", Value: strings.Repeat("x", maxTXTLength+1)},
	}
	for name, r := range bad {
		_, _, err := prepareRecord("v", testDomain, r, caps)
		if err == nil {
			t.Errorf("%s: accepted %+v", name, r)
		}
		if name == "unsupported" && !errors.Is(err, ErrUnsupported) {
			t.Errorf("unsupported type: %v", err)
		}
	}
	if _, _, err := prepareRecord("v", Domain{ID: "1"}, ok[1], caps); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing domain name: %v", err)
	}
	if _, _, err := prepareRecord("v", testDomain, ok[1], DomainCapabilities{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no DNS: %v", err)
	}
}

func TestPlanUpsert(t *testing.T) {
	existing := []existingRecord{
		{ID: "spf", Type: "TXT", Name: "acme.io", Value: `"v=spf1 include:_spf.google.com ~all"`},
		{ID: "v1", Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=old"},
		{ID: "a1", Type: "A", Name: "acme.io", Value: "192.0.2.1"},
		{ID: "a2", Type: "A", Name: "acme.io", Value: "192.0.2.2"},
		{ID: "c1", Type: "CNAME", Name: "track.acme.io", Value: "Track.Warmbly.com."},
	}
	cases := []struct {
		name string
		r    DNSRecord
		want dnsPlan
	}{
		{"txt replaces same key", DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=new"}, dnsPlan{update: 1}},
		{"txt keeps spf", DNSRecord{Type: "TXT", Name: "acme.io", Value: "warmbly-verify=x"}, dnsPlan{update: -1}},
		{"spf replaces spf", DNSRecord{Type: "TXT", Name: "acme.io", Value: "v=spf1 -all"}, dnsPlan{update: 0}},
		{"a set collapses", DNSRecord{Type: "A", Name: "acme.io", Value: "192.0.2.9"}, dnsPlan{update: 2, remove: []int{3}}},
		{"cname same target", DNSRecord{Type: "CNAME", Name: "track.acme.io", Value: "track.warmbly.com"}, dnsPlan{noop: true, update: -1}},
		{"txt identical quoted", DNSRecord{Type: "TXT", Name: "acme.io", Value: "v=spf1 include:_spf.google.com ~all"}, dnsPlan{noop: true, update: -1}},
		{"new name", DNSRecord{Type: "CNAME", Name: "t2.acme.io", Value: "x.warmbly.com"}, dnsPlan{update: -1}},
	}
	for _, tc := range cases {
		if got := planUpsert(existing, tc.r); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: plan = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestHostConversions(t *testing.T) {
	for in, want := range map[string]string{"@": "acme.io", "": "acme.io", "track": "track.acme.io", "Track.Acme.io.": "track.acme.io", "acme.io": "acme.io"} {
		if got := absoluteHost(in, "acme.io"); got != want {
			t.Errorf("absoluteHost(%q) = %q, want %q", in, got, want)
		}
	}
	if relativeHost("acme.io", "acme.io") != "@" || relativeHost("_warmbly.acme.io", "acme.io") != "_warmbly" {
		t.Fatal("relativeHost")
	}
}

func TestForwardingTargetValidation(t *testing.T) {
	caps := DomainCapabilities{Forwarding: true, ForwardingRemove: true}
	for _, u := range []string{"ftp://acme.com", "acme.com", "https://user:pw@acme.com", "https://", "https://acme.com/a\nb", "javascript:alert(1)"} {
		if _, err := forwardingTarget("v", u, caps); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("forwardingTarget(%q) = %v, want ErrInvalidConfig", u, err)
		}
	}
	if got, err := forwardingTarget("v", " https://acme.com/about ", caps); err != nil || got != "https://acme.com/about" {
		t.Fatalf("valid url = %q, %v", got, err)
	}
	if _, err := forwardingTarget("v", "", DomainCapabilities{Forwarding: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("remove without support: %v", err)
	}
}

func TestInboxKitDomains(t *testing.T) {
	const ws = "6f1c2d3e-0000-4000-8000-000000000001"
	dnsList := `{"error":false,"message":"DNS records retrieved successfully","dns_record":{"uid":"doc1","records":[
		{"_id":"60f7b8c9e4b0b8c9e4b0b8c1","host":"@","type":"MX","value":"aspmx.l.google.com","ttl":3600,"status":"propagated","priority":1,"proxied":false},
		{"_id":"60f7b8c9e4b0b8c9e4b0b8c2","host":"_warmbly","type":"TXT","value":"warmbly-verify=old","ttl":3600},
		{"_id":"60f7b8c9e4b0b8c9e4b0b8c3","host":"@","type":"A","value":"192.0.2.1","ttl":3600},
		{"_id":"60f7b8c9e4b0b8c9e4b0b8c4","host":"@","type":"A","value":"192.0.2.2","ttl":3600}]}}`
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/api/workspaces/list":
			writeJSON(w, 200, `{"error":false,"workspaces":[{"uid":"`+ws+`","name":"Main"}]}`)
		case "/v1/api/domains/list":
			var body struct{ Page int }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Page == 1 {
				writeJSON(w, 200, `{"error":false,"message":"Domains retrieved successfully","domains":[{"uid":"3340bae3-53c3-4ab8-9c61-1d8205b86c70","name":"example.com","status":"active","tld":"com","forwarding_url":"https://example.org","nameservers":["a.ns.cloudflare.com"]}],"total":2,"pages":2,"current_page":1,"limit":1}`)
				return
			}
			writeJSON(w, 200, `{"error":false,"domains":[{"uid":"u2","name":"acme","tld":"io","forwarding_url":""}],"total":2,"pages":2,"current_page":2,"limit":1}`)
		case "/v1/api/domains/forwarding":
			writeJSON(w, 200, `{"error":false,"message":"Domain forwarding configured successfully","domains":[{"_id":"x","name":"acme.io","uid":"u2"}]}`)
		case "/v1/api/dns/list":
			if r.URL.Query().Get("uid") == "fresh" {
				writeJSON(w, 404, `{"error":true,"message":"Domain or DNS records not found"}`)
				return
			}
			writeJSON(w, 200, dnsList)
		case "/v1/api/dns/add", "/v1/api/dns/update", "/v1/api/dns/delete":
			writeJSON(w, 200, `{"error":false,"message":"ok"}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	dm := domainManager(t, newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, srv.URL, nil))
	ctx := context.Background()

	got, err := dm.Domains(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Domain{{ID: ws + ":3340bae3-53c3-4ab8-9c61-1d8205b86c70", Name: "example.com", Forwarding: "https://example.org"}, {ID: ws + ":u2", Name: "acme.io"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Domains = %+v", got)
	}
	d := got[1]
	if err := dm.SetForwarding(ctx, d, "https://acme.com"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, d, ""); err != nil {
		t.Fatal(err)
	}
	// Replaces the verification token in place.
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=new", TTL: 300}); err != nil {
		t.Fatal(err)
	}
	// Collapses the two root A records into one.
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "A", Name: "acme.io", Value: "192.0.2.9"}); err != nil {
		t.Fatal(err)
	}
	// A new CNAME on a domain with no records yet.
	if err := dm.UpsertDNSRecord(ctx, Domain{ID: ws + ":fresh", Name: "acme.io"}, DNSRecord{Type: "CNAME", Name: "track.acme.io", Value: "track.warmbly.com"}); err != nil {
		t.Fatal(err)
	}
	// A domain id without its workspace cannot be addressed, so nothing is sent.
	if err := dm.SetForwarding(ctx, Domain{ID: "u2", Name: "acme.io"}, "https://acme.com"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unscoped id: %v", err)
	}

	reqs := srv.requests()
	for _, r := range reqs {
		assertHeader(t, r, "Authorization", "Bearer "+testKey)
		if r.Path != "/v1/api/workspaces/list" {
			assertHeader(t, r, "X-Workspace-Id", ws)
		}
	}
	var writes []map[string]any
	var paths []string
	for _, r := range reqs {
		if r.Method == http.MethodPost && r.Path != "/v1/api/domains/list" {
			writes = append(writes, decodeBody(t, r))
			paths = append(paths, r.Path)
		}
	}
	wantPaths := []string{"/v1/api/domains/forwarding", "/v1/api/domains/forwarding", "/v1/api/dns/update", "/v1/api/dns/update", "/v1/api/dns/delete", "/v1/api/dns/add"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("writes = %v", paths)
	}
	if !reflect.DeepEqual(writes[0], map[string]any{"uids": []any{"u2"}, "forwarding_url": "https://acme.com"}) || writes[1]["forwarding_url"] != "" {
		t.Fatalf("forwarding bodies = %v, %v", writes[0], writes[1])
	}
	upd := writes[2]["records"].([]any)[0].(map[string]any)
	if writes[2]["uid"] != "u2" || !reflect.DeepEqual(upd, map[string]any{"record_id": "60f7b8c9e4b0b8c9e4b0b8c2", "host": "_warmbly", "type": "TXT", "value": "warmbly-verify=new", "ttl": float64(300)}) {
		t.Fatalf("update body = %v", writes[2])
	}
	if a := writes[3]["records"].([]any)[0].(map[string]any); a["record_id"] != "60f7b8c9e4b0b8c9e4b0b8c3" || a["host"] != "@" {
		t.Fatalf("A update = %v", a)
	}
	if !reflect.DeepEqual(writes[4]["record_ids"], []any{"60f7b8c9e4b0b8c9e4b0b8c4"}) {
		t.Fatalf("delete body = %v", writes[4])
	}
	add := writes[5]["records"].([]any)[0].(map[string]any)
	if writes[5]["uid"] != "fresh" || !reflect.DeepEqual(add, map[string]any{"host": "track", "type": "CNAME", "value": "track.warmbly.com"}) {
		t.Fatalf("add body = %v", writes[5])
	}
}

func TestInboxKitDNSWithoutRecordIDs(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"error":false,"dns_record":{"records":[{"host":"_warmbly","type":"TXT","value":"warmbly-verify=old"}]}}`)
	})
	dm := domainManager(t, newTestClient(t, VendorInboxKit, fieldsFor(VendorInboxKit), srv.URL, nil))
	err := dm.UpsertDNSRecord(context.Background(), Domain{ID: "ws-1:u2", Name: "acme.io"}, DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=new"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if n := len(srv.requests()); n != 1 {
		t.Fatalf("%d requests, want only the list", n)
	}
	// An identical record needs no id: nothing is written.
	if err := dm.UpsertDNSRecord(context.Background(), Domain{ID: "ws-1:u2", Name: "acme.io"}, DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=old"}); err != nil {
		t.Fatalf("no-op: %v", err)
	}
}

func TestInboxKitDomainErrorEnvelope(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"error":true,"message":"Domain not found"}`)
	})
	dm := domainManager(t, newTestClient(t, VendorInboxKit, fieldsFor(VendorInboxKit), srv.URL, nil))
	if err := dm.SetForwarding(context.Background(), testDomainFor(VendorInboxKit), "https://acme.com"); err == nil {
		t.Fatal("SetForwarding accepted an error envelope")
	}
}

func TestZapmailDomains(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/workspaces":
			writeJSON(w, 200, `{"status":200,"data":[{"id":"ws-123","name":"Main"}]}`)
		case "/v2/domains":
			if r.Header.Get("x-service-provider") == "MICROSOFT" {
				writeJSON(w, 200, `{"status":200,"message":"ok","data":{"totalSearchedCount":1,"currentPage":1,"nextPage":null,"totalPages":1,"domains":[{"id":"ms-d","domain":"contoso.co","status":"ACTIVE","forwardTo":null}]}}`)
				return
			}
			writeJSON(w, 200, `{"status":200,"message":"ok","data":{"totalSearchedCount":1,"currentPage":1,"nextPage":null,"totalPages":1,"domains":[{"id":"g-d","domain":"acme.io","status":"ACTIVE","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z","forwardTo":"acme.com","forwardToAddedOnReseller":true,"dmarcEmail":null,"nameServers":[],"assignedMailboxesCount":"3"}]}}`)
		case "/v2/domains/forwarding", "/v2/domains/remove-forwarding":
			writeJSON(w, 200, `{"status":200,"message":"ok","data":null}`)
		case "/v2/dns/":
			writeJSON(w, 200, `{"status":200,"message":"DNS records fetched successfully","data":{"records":[
				{"id":"r1","assignedDomainId":"g-d","cdfRecordId":"cf1","cdfResponse":{"id":"cf1","name":"track.acme.io","type":"CNAME","content":"old.example.com"},"recordType":"CNAME","value":"old.example.com","host":"track.acme.io","priority":null},
				{"id":"r2","recordType":"A","value":"192.0.2.1","host":"acme.io"},
				{"id":"r3","recordType":"A","value":"192.0.2.2","host":"acme.io"}],"disabledRecords":[]}}`)
		case "/v2/dns":
			writeJSON(w, 200, `{"status":200,"message":"ok","data":null}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	dm := domainManager(t, newTestClient(t, VendorZapmail, map[string]string{FieldAPIKey: testKey}, srv.URL, nil))
	ctx := context.Background()
	got, err := dm.Domains(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Domain{{ID: "ws-123:g-d", Name: "acme.io", Forwarding: "acme.com"}, {ID: "ws-123:ms-d", Name: "contoso.co"}}) {
		t.Fatalf("Domains = %+v", got)
	}
	d := got[0]
	if err := dm.SetForwarding(ctx, d, "https://acme.com"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, got[1], ""); err != nil {
		t.Fatal(err)
	}
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "CNAME", Name: "track.acme.io", Value: "track.warmbly.com", TTL: 60}); err != nil {
		t.Fatal(err)
	}
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=abc"}); err != nil {
		t.Fatal(err)
	}
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "A", Name: "acme.io", Value: "192.0.2.9"}); err != nil {
		t.Fatal(err)
	}

	// The workspace list comes first; drop it so the indexes below read the domain calls.
	reqs := srv.requests()
	if reqs[0].Path != "/v2/workspaces" {
		t.Fatalf("first call = %s", reqs[0].Path)
	}
	reqs = reqs[1:]
	for _, r := range reqs {
		assertHeader(t, r, "x-auth-zapmail", testKey)
		assertHeader(t, r, "x-workspace-key", "ws-123")
	}
	var seen []string
	for _, r := range reqs[2:] {
		seen = append(seen, r.Method+" "+r.Path+" "+r.Header.Get("x-service-provider"))
	}
	wantSeen := []string{
		"POST /v2/domains/forwarding GOOGLE", "POST /v2/domains/remove-forwarding MICROSOFT",
		"GET /v2/dns/ GOOGLE", "PUT /v2/dns GOOGLE",
		"GET /v2/dns/ GOOGLE", "POST /v2/dns GOOGLE",
		"GET /v2/dns/ GOOGLE", "PUT /v2/dns GOOGLE", "DELETE /v2/dns GOOGLE",
	}
	if !reflect.DeepEqual(seen, wantSeen) {
		t.Fatalf("calls = %v", seen)
	}
	if b := decodeBody(t, reqs[2]); !reflect.DeepEqual(b, map[string]any{"domainIds": []any{"g-d"}, "forwardTo": "https://acme.com"}) {
		t.Fatalf("forwarding body = %v", b)
	}
	if b := decodeBody(t, reqs[3]); !reflect.DeepEqual(b, map[string]any{"domainId": "ms-d"}) || reqs[3].Header.Get("x-workspace-id") != "ws-123" {
		t.Fatalf("remove body = %v", b)
	}
	if reqs[4].q("id") != "g-d" {
		t.Fatalf("dns list query = %v", reqs[4].Query)
	}
	if b := decodeBody(t, reqs[5]); !reflect.DeepEqual(b, map[string]any{"assignedDomainId": "g-d", "dnsRecordId": "r1", "host": "track", "value": "track.warmbly.com", "recordType": "CNAME"}) {
		t.Fatalf("update body = %v", b)
	}
	if b := decodeBody(t, reqs[7]); !reflect.DeepEqual(b, map[string]any{"assignedDomainId": "g-d", "records": []any{map[string]any{"host": "_warmbly", "value": "warmbly-verify=abc", "recordType": "TXT"}}}) {
		t.Fatalf("add body = %v", b)
	}
	if del := reqs[10]; del.q("id") != "r3" || del.q("assignedDomainId") != "g-d" {
		t.Fatalf("delete query = %v", del.Query)
	}
}

func TestForgeDomains(t *testing.T) {
	dnsBody := `[{"editable":false,"name":"@","type":"MX","value":"mx.mailforge.ai"},{"editable":true,"name":"track","type":"CNAME","value":"old.example.com"},{"editable":false,"name":"_dmarc","type":"TXT","value":"v=DMARC1; p=none"}]`
	for _, vendor := range []string{VendorMailforge, VendorInfraforge} {
		t.Run(vendor, func(t *testing.T) {
			srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/domains":
					writeJSON(w, 200, `[{"id":"dom_1","sld":"acme","tld":"io","forwardToDomain":"https://acme.com","status":"active","workspaceId":"wks_1"},{"id":"dom_2","sld":"other","tld":"com","workspaceId":"wks_2"}]`)
				case r.URL.Path == "/domains/forwards":
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/domains/dom_1/dns" && r.Method == http.MethodGet:
					writeJSON(w, 200, dnsBody)
				case r.URL.Path == "/domains/dom_1/dns" && r.Method == http.MethodPut:
					w.WriteHeader(http.StatusNoContent)
				default:
					writeJSON(w, 404, `{"code":404,"message":"Domain not found"}`)
				}
			})
			dm := domainManager(t, newTestClient(t, vendor, map[string]string{FieldAPIKey: testKey}, srv.URL, nil))
			ctx := context.Background()
			got, err := dm.Domains(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := []Domain{{ID: "dom_1", Name: "acme.io", Forwarding: "https://acme.com"}, {ID: "dom_2", Name: "other.com"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Domains = %+v", got)
			}
			d := got[0]
			if err := dm.SetForwarding(ctx, d, "https://acme.com/about"); err != nil {
				t.Fatal(err)
			}
			if err := dm.SetForwarding(ctx, d, ""); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("remove: %v", err)
			}
			if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "CNAME", Name: "track.acme.io", Value: "track.warmbly.com"}); err != nil {
				t.Fatal(err)
			}
			if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "TXT", Name: "_warmbly.acme.io", Value: "warmbly-verify=abc"}); err != nil {
				t.Fatal(err)
			}
			if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "TXT", Name: "_dmarc.acme.io", Value: "v=DMARC1; p=reject"}); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("locked record: %v", err)
			}
			if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "A", Name: "acme.io", Value: "192.0.2.1"}); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("A record: %v", err)
			}

			reqs := srv.requests()
			var puts []string
			for _, r := range reqs {
				assertHeader(t, r, "Authorization", testKey)
				if r.Method == http.MethodPut {
					puts = append(puts, r.Body)
				}
			}
			if b := reqs[1].Body; b != `[{"domainId":"dom_1","forwardToDomain":"https://acme.com/about"}]` || reqs[1].Method != http.MethodPatch {
				t.Fatalf("forwards body = %s", b)
			}
			wantPuts := []string{
				`{"records":[{"editable":false,"name":"@","type":"MX","value":"mx.mailforge.ai"},{"editable":true,"name":"track","type":"CNAME","value":"track.warmbly.com"},{"editable":false,"name":"_dmarc","type":"TXT","value":"v=DMARC1; p=none"}]}`,
				`{"records":[{"editable":false,"name":"@","type":"MX","value":"mx.mailforge.ai"},{"editable":true,"name":"track","type":"CNAME","value":"old.example.com"},{"editable":false,"name":"_dmarc","type":"TXT","value":"v=DMARC1; p=none"},{"name":"_warmbly","type":"TXT","value":"warmbly-verify=abc"}]}`,
			}
			if !reflect.DeepEqual(puts, wantPuts) {
				t.Fatalf("PUT bodies =\n%s", strings.Join(puts, "\n"))
			}
		})
	}
}

func TestInfraforgeSingleObjectDomains(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"id":"dom_9xh7elemavnvew01itfxc","sld":"example","tld":"com","forwardToDomain":"https://yourdomain.com","workspaceId":"wks_70my6ggvn5csfw3o27ojq"}`)
	})
	dm := domainManager(t, newTestClient(t, VendorInfraforge, fieldsFor(VendorInfraforge), srv.URL, nil))
	got, err := dm.Domains(context.Background())
	if err != nil || !reflect.DeepEqual(got, []Domain{{ID: "dom_9xh7elemavnvew01itfxc", Name: "example.com", Forwarding: "https://yourdomain.com"}}) {
		t.Fatalf("Domains = %+v, %v", got, err)
	}
}

func TestMaildosoDomains(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user/domains" && r.Method == http.MethodGet:
			writeJSON(w, 200, `[{"id":7,"user_id":1,"domain_name":"acme.io","is_hidden":false,"enable_tracking":true,"linked_accounts":3,"created_at":"2025-01-01T00:00:00Z","domain_type":"STANDARD","domain_status":"active","redirect_to":"https://acme.com","name_servers":[],"accounts_limit":null},{"id":8,"domain_name":"beta.io","redirect_to":null}]`)
		case r.URL.Path == "/v1/user/domains" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	dm := domainManager(t, newTestClient(t, VendorMaildoso, fieldsFor(VendorMaildoso), srv.URL, nil))
	ctx := context.Background()
	got, err := dm.Domains(ctx)
	if err != nil || !reflect.DeepEqual(got, []Domain{{ID: "7", Name: "acme.io", Forwarding: "https://acme.com"}, {ID: "8", Name: "beta.io"}}) {
		t.Fatalf("Domains = %+v, %v", got, err)
	}
	if err := dm.SetForwarding(ctx, got[0], "https://acme.com/new"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, got[1], ""); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, Domain{ID: "abc", Name: "x.io"}, "https://acme.com"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("non-numeric id: %v", err)
	}
	reqs := srv.requests()
	if reqs[1].Body != `[{"id":7,"redirect_to":"https://acme.com/new"}]` || reqs[2].Body != `[{"id":8,"redirect_to":null}]` {
		t.Fatalf("bodies = %s / %s", reqs[1].Body, reqs[2].Body)
	}
	for _, r := range reqs {
		assertHeader(t, r, "Authorization", "Bearer "+testKey)
	}
}

func TestCheapInboxesDomains(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/domains":
			if r.URL.Query().Get("offset") == "0" {
				writeJSON(w, 200, `{"domains":[{"id":"d_abc123","domain":"acmeoutreach.com","status":"active","source_provider":"cheapinboxes","dns_mode":"nameservers","infra_provider":"google","auto_renew":true,"forwarding_url":"https://acme.com","tags":["campaign-1"]}],"pagination":{"total":2,"limit":1,"offset":0}}`)
				return
			}
			writeJSON(w, 200, `{"domains":[{"id":"d_def456","domain":"acmeimported.com","status":"pending_manual_setup"}],"pagination":{"total":2,"limit":1,"offset":1}}`)
		case r.URL.Path == "/v1/domains/d_abc123/forwarding":
			writeJSON(w, 200, `{"domain":{"id":"d_abc123","forwarding_url":"https://acme.com","forwarding_status":"active"}}`)
		case r.URL.Path == "/v1/domains/d_abc123/dns-records" && r.Method == http.MethodGet:
			switch r.URL.Query().Get("type") {
			case "TXT":
				writeJSON(w, 200, `{"records":[{"id":"rec_002","type":"TXT","name":"acmeoutreach.com","content":"v=spf1 include:_spf.google.com ~all","ttl":3600,"proxied":false}]}`)
			case "A":
				writeJSON(w, 200, `{"records":[{"id":"rec_a1","type":"A","name":"acmeoutreach.com","content":"192.0.2.1","ttl":3600},{"id":"rec_a2","type":"A","name":"acmeoutreach.com","content":"192.0.2.2","ttl":3600}]}`)
			default:
				writeJSON(w, 200, `{"records":[]}`)
			}
		case strings.HasPrefix(r.URL.Path, "/v1/domains/d_abc123/dns-records"):
			writeJSON(w, 200, `{"record":{"id":"rec_003"},"success":true}`)
		default:
			writeJSON(w, 404, `{"error":{"code":"NOT_FOUND","message":"not found"}}`)
		}
	})
	dm := domainManager(t, newTestClient(t, VendorCheapInboxes, fieldsFor(VendorCheapInboxes), srv.URL, nil))
	ctx := context.Background()
	got, err := dm.Domains(ctx)
	if err != nil || !reflect.DeepEqual(got, []Domain{{ID: "d_abc123", Name: "acmeoutreach.com", Forwarding: "https://acme.com"}, {ID: "d_def456", Name: "acmeimported.com"}}) {
		t.Fatalf("Domains = %+v, %v", got, err)
	}
	d := got[0]
	if err := dm.SetForwarding(ctx, d, "https://acme.com"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, d, ""); err != nil {
		t.Fatal(err)
	}
	// The SPF record stays; the token is created beside it.
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "TXT", Name: "acmeoutreach.com", Value: "warmbly-verify=abc", TTL: 300}); err != nil {
		t.Fatal(err)
	}
	if err := dm.UpsertDNSRecord(ctx, d, DNSRecord{Type: "A", Name: "acmeoutreach.com", Value: "192.0.2.9"}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	reqs := srv.requests()
	for _, r := range reqs[2:] {
		seen = append(seen, r.Method+" "+r.Path)
		assertHeader(t, r, "Authorization", "Bearer "+testKey)
	}
	wantSeen := []string{
		"PATCH /v1/domains/d_abc123/forwarding", "PATCH /v1/domains/d_abc123/forwarding",
		"GET /v1/domains/d_abc123/dns-records", "POST /v1/domains/d_abc123/dns-records",
		"GET /v1/domains/d_abc123/dns-records", "PATCH /v1/domains/d_abc123/dns-records/rec_a1", "DELETE /v1/domains/d_abc123/dns-records/rec_a2",
	}
	if !reflect.DeepEqual(seen, wantSeen) {
		t.Fatalf("calls = %v", seen)
	}
	if reqs[2].Body != `{"forwarding_url":"https://acme.com"}` || reqs[3].Body != `{"forwarding_url":null}` {
		t.Fatalf("forwarding bodies = %s / %s", reqs[2].Body, reqs[3].Body)
	}
	if reqs[5].Body != `{"type":"TXT","name":"@","content":"warmbly-verify=abc","ttl":300}` {
		t.Fatalf("create body = %s", reqs[5].Body)
	}
	if reqs[7].Body != `{"type":"A","name":"@","content":"192.0.2.9"}` {
		t.Fatalf("update body = %s", reqs[7].Body)
	}
	if reqs[6].q("type") != "A" {
		t.Fatalf("list query = %v", reqs[6].Query)
	}
}

func TestScaledMailDomains(t *testing.T) {
	const org = "recORG000000001"
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/organizations":
			writeJSON(w, 200, `[{"id":"`+org+`","name":"Acme"}]`)
		case r.URL.Path == "/domains":
			writeJSON(w, 200, `{"total":2,"domains":[{"id":"recDOM1","domain":"outreach-one.com","tag":"","redirect":"https://example.com","order_type":"google","status":"Active"},{"id":"recDOM2","domain":"outreach-two.com","redirect":"","order_type":"outlook","status":"Active"}]}`)
		case strings.HasPrefix(r.URL.Path, "/swap-redirect/"):
			writeJSON(w, 200, `{"message":"Redirect Swap Requested"}`)
		default:
			writeJSON(w, 404, `{"error":"Domain not found"}`)
		}
	})
	dm := domainManager(t, newTestClient(t, VendorScaledMail, map[string]string{FieldAPIKey: testKey}, srv.URL, nil))
	ctx := context.Background()
	got, err := dm.Domains(ctx)
	if err != nil || !reflect.DeepEqual(got, []Domain{{ID: org + ":recDOM1", Name: "outreach-one.com", Forwarding: "https://example.com"}, {ID: org + ":recDOM2", Name: "outreach-two.com"}}) {
		t.Fatalf("Domains = %+v, %v", got, err)
	}
	if err := dm.SetForwarding(ctx, got[0], "https://mybrand.com/landing"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, got[0], ""); err != nil {
		t.Fatal(err)
	}
	// The vendor refuses a request for the redirect already set, so none is sent.
	if err := dm.SetForwarding(ctx, got[0], "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if err := dm.SetForwarding(ctx, got[1], ""); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()[1:]
	if len(reqs) != 3 {
		t.Fatalf("%d requests after the organization list, want 3", len(reqs))
	}
	for _, r := range reqs[1:] {
		if r.Method != http.MethodPost || r.Path != "/swap-redirect/outreach-one.com" || r.q("organization_id") != org {
			t.Fatalf("request = %+v", r)
		}
	}
	if reqs[1].Body != `{"new_redirect":"https://mybrand.com/landing"}` || reqs[2].Body != `{"new_redirect":""}` {
		t.Fatalf("bodies = %s / %s", reqs[1].Body, reqs[2].Body)
	}
}

func TestDomainCallsRequireIdentity(t *testing.T) {
	for _, d := range Descriptors() {
		srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, `{}`) })
		dm := domainManager(t, newTestClient(t, d.ID, fieldsFor(d.ID), srv.URL, nil))
		err := dm.SetForwarding(context.Background(), Domain{}, "https://acme.com")
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: SetForwarding on an empty domain: %v", d.ID, err)
		}
		if n := len(srv.requests()); n != 0 {
			t.Errorf("%s: sent %d requests for an empty domain", d.ID, n)
		}
		if caps := dm.DomainCapabilities(); caps.DNS && !slices.Contains(caps.DNSTypes, DNSTypeTXT) {
			t.Errorf("%s: DNS without TXT", d.ID)
		}
	}
}
