package mailvendor

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestInboxKitAuthorizesAnApp(t *testing.T) {
	const ws = "6f1c2d3e-0000-4000-8000-000000000001"
	status := "processing"
	srv := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/api/mailboxes/client-id-request/initiate":
			writeJSON(w, 200, `{"error":false,"message":"Client ID request initiated successfully","data":{"uid":"req-1","status":"queued","domain_name":"acme.io"}}`)
		case "/v1/api/mailboxes/client-id-request/status/req-1":
			writeJSON(w, 200, `{"error":false,"message":"ok","data":{"uid":"req-1","status":"`+status+`","error_message":"No admin mailbox found for domain"}}`)
		default:
			writeJSON(w, 404, `{}`)
		}
	})
	c := newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, srv.URL, nil)
	az, ok := c.(AppAuthorizer)
	if !ok {
		t.Fatal("InboxKit client is not an AppAuthorizer")
	}

	id, err := az.AuthorizeApp(context.Background(), AppAuthorization{
		Domain: "acme.io", Mailbox: Mailbox{ID: ws + ":m1"}, Provider: ProviderMicrosoft,
		ClientID: "11111111-2222-3333-4444-555555555555", Scopes: []string{"User.Read"}, AppRoles: []string{"Mail.Send"},
	})
	if err != nil || id != ws+":req-1" {
		t.Fatalf("AuthorizeApp = %q, %v", id, err)
	}
	init := srv.requests()[0]
	assertHeader(t, init, "X-Workspace-Id", ws)
	var body map[string]any
	_ = json.Unmarshal([]byte(init.Body), &body)
	want := map[string]any{
		"domain": "acme.io", "client_id": "11111111-2222-3333-4444-555555555555",
		"scopes": []any{"User.Read"}, "app_roles": []any{"Mail.Send"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("initiate body = %v\nwant %v", body, want)
	}

	for _, tc := range []struct{ status, state, reason string }{
		{"processing", AuthorizationPending, ""},
		{"completed", AuthorizationCompleted, ""},
		{"errored", AuthorizationFailed, "No admin mailbox found for domain"},
	} {
		status = tc.status
		got, err := az.AuthorizationStatus(context.Background(), id)
		if err != nil || got.State != tc.state || got.Reason != tc.reason {
			t.Fatalf("%s: status = %+v, %v", tc.status, got, err)
		}
	}
}

func TestInboxKitGoogleAuthorizationIsDelegated(t *testing.T) {
	srv := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"error":false,"data":{"uid":"req-2","status":"queued"}}`)
	})
	c := newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, srv.URL, nil)
	scopes := []string{"https://www.googleapis.com/auth/gmail.modify"}
	if _, err := c.(AppAuthorizer).AuthorizeApp(context.Background(), AppAuthorization{
		Domain: "acme.io", Mailbox: Mailbox{ID: "ws:m1"}, Provider: ProviderGoogle, ClientID: "1234567890", Scopes: scopes, AppRoles: []string{"ignored"},
	}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(srv.requests()[0].Body), &body)
	if body["delegated"] != true || body["app_roles"] != nil || !reflect.DeepEqual(body["scopes"], []any{scopes[0]}) {
		t.Fatalf("google initiate body = %v", body)
	}
}

func TestInboxKitAuthorizationNeedsAWorkspace(t *testing.T) {
	c := newTestClient(t, VendorInboxKit, map[string]string{FieldAPIKey: testKey}, "http://127.0.0.1:1", nil)
	if _, err := c.(AppAuthorizer).AuthorizeApp(context.Background(), AppAuthorization{Domain: "acme.io", Mailbox: Mailbox{ID: "m1"}, Provider: ProviderMicrosoft, ClientID: "x"}); err == nil {
		t.Fatal("an id without a workspace was sent")
	}
}
