package msgraph

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type diagnosticRawRT struct {
	body   []byte
	status int
	path   string
}

func (r *diagnosticRawRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.path = req.URL.Path
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(bytes.NewReader(r.body)), Header: http.Header{}}, nil
}

func TestDiagnosticRawMessageFetchesOnlyNamedBoundedMessage(t *testing.T) {
	want := []byte("From: sender@example.test\r\n\r\nBounded diagnostic")
	rt := &diagnosticRawRT{body: want, status: http.StatusOK}
	c := &Client{hc: &http.Client{Transport: rt}}
	got, err := c.DiagnosticRawMessage(context.Background(), "provider/id", len(want))
	if err != nil || string(got) != string(want) || !strings.HasSuffix(rt.path, "/messages/provider/id/$value") {
		t.Fatalf("got=%q path=%s err=%v", got, rt.path, err)
	}
	if _, err = c.DiagnosticRawMessage(context.Background(), "provider/id", len(want)-1); err == nil {
		t.Fatal("oversize raw MIME accepted")
	}
}

func TestDiagnosticRawMessageDoesNotReturnProviderErrors(t *testing.T) {
	secret := "provider-secret-error"
	c := &Client{hc: &http.Client{Transport: &diagnosticRawRT{body: []byte(secret), status: http.StatusForbidden}}}
	_, err := c.DiagnosticRawMessage(context.Background(), "id", 1024)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("provider response escaped", err)
	}
}
