package repository

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// The IMAP token is the same route with a purpose an older backend ignores, and a refusal stays typed.
func TestBrokeredIMAPTokenAsksForItsPurpose(t *testing.T) {
	var queries []string
	refuse := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if refuse {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"google_delegation_unauthorized","message":"no"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"t","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	client, err := NewHTTPBrokeredTokenClient(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := client.Source(id).Token(); err != nil {
		t.Fatal(err)
	}
	imap := client.(BrokeredIMAPTokenClient)
	if _, err := imap.IMAPSource(id).Token(); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "" || queries[1] != "purpose=imap" {
		t.Fatalf("queries = %q", queries)
	}
	refuse = true
	var refused *BrokeredTokenRefused
	if _, err := imap.IMAPSource(uuid.New()).Token(); !errors.As(err, &refused) {
		t.Fatalf("a 403 for the IMAP token = %v, want BrokeredTokenRefused", err)
	}
}
