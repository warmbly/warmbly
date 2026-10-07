package goog

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestDiagnosticRawMessageFetchesOnlyNamedBoundedMessage(t *testing.T) {
	want := []byte("From: sender@example.test\r\n\r\nBounded diagnostic")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/gmail/v1/users/me/messages/provider%2Fid" || r.URL.Query().Get("format") != "raw" {
			t.Fatalf("request=%s", r.URL.String())
		}
		_, _ = fmt.Fprintf(w, `{"raw":%q}`, base64.RawURLEncoding.EncodeToString(want))
	}))
	defer srv.Close()
	c := &Client{srv: &gmail.Service{BasePath: srv.URL}, rawClient: srv.Client()}
	got, err := c.DiagnosticRawMessage(context.Background(), "provider/id", len(want))
	if err != nil || string(got) != string(want) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err = c.DiagnosticRawMessage(context.Background(), "provider/id", len(want)-1); err == nil {
		t.Fatal("oversize raw MIME accepted")
	}
}

func TestDiagnosticRawMessageDoesNotReturnProviderErrors(t *testing.T) {
	secret := "provider-secret-error"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, secret, http.StatusForbidden) }))
	defer srv.Close()
	c := &Client{srv: &gmail.Service{BasePath: srv.URL}, rawClient: srv.Client()}
	_, err := c.DiagnosticRawMessage(context.Background(), "id", 1024)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("provider response escaped", err)
	}
}
