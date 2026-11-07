package mailvendor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

var (
	_ ConsentRequester = (*inboxKit)(nil)
	_ ConsentRequester = (*zapmail)(nil)
)

func TestInboxKitTrustsTheClientWithoutDelegation(t *testing.T) {
	const ws = "6f1c2d3e-0000-4000-8000-000000000001"
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/api/mailboxes/client-id-request/initiate":
			writeJSON(w, 200, `{"error":false,"data":{"uid":"req-9","status":"queued"}}`)
		case "/v1/api/mailboxes/client-id-request/status/req-9":
			writeJSON(w, 200, `{"error":false,"data":{"uid":"req-9","status":"completed"}}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	c := newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, srv.URL, nil).(ConsentRequester)
	id, err := c.TrustApp(context.Background(), AppTrust{Domain: "acme.io", Mailbox: Mailbox{ID: ws + ":m1"}, ClientID: "123-abc.apps.googleusercontent.com", AppName: "Warmbly"})
	if err != nil || id != ws+":req-9" {
		t.Fatalf("TrustApp = %q, %v", id, err)
	}
	req := srv.requests()[0]
	assertHeader(t, req, "X-Workspace-Id", ws)
	var body map[string]any
	_ = json.Unmarshal([]byte(req.Body), &body)
	want := map[string]any{"domain": "acme.io", "client_id": "123-abc.apps.googleusercontent.com", "delegated": false, "platform": "GOOGLE"}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("trust body = %v\nwant %v", body, want)
	}
	if st, err := c.TrustStatus(context.Background(), id); err != nil || st.State != AuthorizationCompleted {
		t.Fatalf("TrustStatus = %+v, %v", st, err)
	}
}

func TestInboxKitRequestsConsent(t *testing.T) {
	const ws = "6f1c2d3e-0000-4000-8000-000000000001"
	status, reason := "processing", ""
	inProgress := false
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/api/mailboxes/consent-request/initiate":
			if inProgress {
				writeJSON(w, 400, `{"error":true,"message":"Consent request already in progress for this mailbox"}`)
				return
			}
			writeJSON(w, 200, `{"error":false,"message":"Consent request initiated successfully","data":{"uid":"cr-1","status":"queued","mailbox_email":"ava@acme.io"}}`)
		case "/v1/api/mailboxes/consent-request/status/cr-1":
			writeJSON(w, 200, `{"error":false,"data":{"uid":"cr-1","status":"`+status+`","status_text":"Processing consent request","status_reason":`+reason+`,"retry_count":0,"created_at":"2026-01-15T10:30:00.000Z","last_attempt_at":"2026-01-15T10:31:00.000Z"}}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	c := newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, srv.URL, nil).(ConsentRequester)
	link := "https://accounts.google.com/o/oauth2/auth?client_id=1&state=vc.x"
	id, err := c.RequestConsent(context.Background(), ConsentRequest{Mailbox: Mailbox{ID: ws + ":mb-7", Email: "ava@acme.io"}, URL: link, ClientID: "1"})
	if err != nil || id != ws+":cr-1" {
		t.Fatalf("RequestConsent = %q, %v", id, err)
	}
	req := srv.requests()[0]
	assertHeader(t, req, "X-Workspace-Id", ws)
	var body map[string]any
	_ = json.Unmarshal([]byte(req.Body), &body)
	want := map[string]any{"mailbox_uid": "mb-7", "consent_url": link, "request_data": map[string]any{"client_id": "1"}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("consent body = %v\nwant %v", body, want)
	}

	for _, tc := range []struct{ status, reason, state, want string }{
		{"queued", "null", AuthorizationPending, ""},
		{"completed", "null", AuthorizationCompleted, ""},
		{"failed", `"Login challenge"`, AuthorizationFailed, "Login challenge"},
		{"errored", `""`, AuthorizationFailed, "Processing consent request"},
	} {
		status, reason = tc.status, tc.reason
		got, err := c.ConsentStatus(context.Background(), id)
		if err != nil || got.State != tc.state || got.Reason != tc.want {
			t.Fatalf("%s: status = %+v, %v", tc.status, got, err)
		}
		if tc.state == AuthorizationPending && !got.UpdatedAt.Equal(time.Date(2026, 1, 15, 10, 31, 0, 0, time.UTC)) {
			t.Fatalf("pending updated at = %v", got.UpdatedAt)
		}
	}

	inProgress = true
	if _, err := c.RequestConsent(context.Background(), ConsentRequest{Mailbox: Mailbox{ID: ws + ":mb-7"}, URL: link}); !errors.Is(err, ErrInProgress) {
		t.Fatalf("a mailbox with an open request = %v, want ErrInProgress", err)
	}
	if n, _ := c.ConsentBudget(); n != 0 {
		t.Fatalf("InboxKit budget = %d, want unlimited", n)
	}
}

func TestZapmailTrustsAndRequestsConsent(t *testing.T) {
	const ws = "zws-1"
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/domains":
			writeJSON(w, 200, `{"status":200,"data":{"totalPages":1,"domains":[{"id":"d-other","domain":"notacme.io"},{"id":"d-1","domain":"acme.io"}]}}`)
		case "/v2/domains/add-client-id":
			writeJSON(w, 200, `{"status":200,"message":"Client ID will be added to the domains soon."}`)
		case "/v2/mailboxes/custom-oauth":
			writeJSON(w, 200, `{"status":200,"message":"ok","data":{"exportId":10938}}`)
		case "/v2/exports/status":
			writeJSON(w, 200, `{"status":200,"message":"Export status fetched successfully","data":{"export_id":10938,"status":"failed","failure_reason":"Mailbox is disabled"}}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	c := newTestClient(t, VendorZapmail, map[string]string{FieldAPIKey: testKey}, srv.URL, nil).(ConsentRequester)
	id, err := c.TrustApp(context.Background(), AppTrust{Domain: "acme.io", Mailbox: Mailbox{ID: ws + ":mb-1"}, ClientID: "cid", AppName: "Warmbly"})
	if err != nil || id != ws+":d-1" {
		t.Fatalf("TrustApp = %q, %v", id, err)
	}
	reqs := srv.requests()
	if reqs[0].q("contains") != "acme.io" {
		t.Fatalf("domain lookup query = %v", reqs[0].Query)
	}
	assertHeader(t, reqs[1], "x-workspace-key", ws)
	assertHeader(t, reqs[1], "x-service-provider", "GOOGLE")
	var body map[string]any
	_ = json.Unmarshal([]byte(reqs[1].Body), &body)
	if want := map[string]any{"domainIds": []any{"d-1"}, "clientId": "cid", "app": "Warmbly"}; !reflect.DeepEqual(body, want) {
		t.Fatalf("add-client-id body = %v\nwant %v", body, want)
	}
	if st, err := c.TrustStatus(context.Background(), id); err != nil || st.State != AuthorizationCompleted {
		t.Fatalf("TrustStatus = %+v, %v", st, err)
	}

	link := "https://accounts.google.com/o/oauth2/auth?state=vc.y"
	cid, err := c.RequestConsent(context.Background(), ConsentRequest{Mailbox: Mailbox{ID: ws + ":mb-1", Email: "ava@acme.io", Domain: "acme.io"}, URL: link, ClientID: "cid", AppName: "Warmbly"})
	if err != nil || cid != ws+":10938" {
		t.Fatalf("RequestConsent = %q, %v", cid, err)
	}
	reqs = srv.requests()
	consent := reqs[len(reqs)-1]
	if consent.Path != "/v2/mailboxes/custom-oauth" || len(reqs) != 3 {
		t.Fatalf("the domain id was looked up again: %d requests, last %s", len(reqs), consent.Path)
	}
	body = nil
	_ = json.Unmarshal([]byte(consent.Body), &body)
	want := map[string]any{"google": map[string]any{
		"appName": "Warmbly", "clientId": "cid",
		"mailboxesPerDomain": map[string]any{"d-1": []any{map[string]any{"mailboxId": "mb-1", "oauthLink": link}}},
	}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("custom-oauth body = %v\nwant %v", body, want)
	}
	st, err := c.ConsentStatus(context.Background(), cid)
	if err != nil || st.State != AuthorizationFailed || st.Reason != "Mailbox is disabled" {
		t.Fatalf("ConsentStatus = %+v, %v", st, err)
	}
	all := srv.requests()
	if last := all[len(all)-1]; last.q("exportId") != "10938" {
		t.Fatalf("status query = %v", last.Query)
	}
	if n, window := c.ConsentBudget(); n != 3 || window != 7*24*time.Hour {
		t.Fatalf("Zapmail budget = %d per %v", n, window)
	}
}

func TestZapmailConsentIsNotRetriedOnItsLimit(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/domains" {
			writeJSON(w, 200, `{"status":200,"data":{"domains":[{"id":"d-1","domain":"acme.io"}]}}`)
			return
		}
		writeJSON(w, 429, `{"status":429,"message":"Rate limit exceeded for mailbox 'mb-1'"}`)
	})
	c := newTestClient(t, VendorZapmail, map[string]string{FieldAPIKey: testKey}, srv.URL, nil).(ConsentRequester)
	_, err := c.RequestConsent(context.Background(), ConsentRequest{Mailbox: Mailbox{ID: "ws:mb-1", Domain: "acme.io"}, URL: "https://accounts.google.com/x", ClientID: "cid", AppName: "Warmbly"})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if n := len(srv.requests()); n != 2 {
		t.Fatalf("%d requests, want the lookup and one consent", n)
	}
}

// SupportsConsent is what selects vendors in SQL, so it has to name exactly the clients that can sign in for Warmbly.
func TestSupportsConsentMatchesTheClients(t *testing.T) {
	for _, d := range Descriptors() {
		fields := map[string]string{}
		for _, f := range d.Fields {
			fields[f.Key] = "x"
		}
		c, err := New(d.ID, fields)
		if err != nil {
			t.Fatalf("%s: %v", d.ID, err)
		}
		_, is := c.(ConsentRequester)
		if is != SupportsConsent(d.ID) {
			t.Errorf("%s: ConsentRequester = %v, SupportsConsent = %v", d.ID, is, SupportsConsent(d.ID))
		}
	}
}
