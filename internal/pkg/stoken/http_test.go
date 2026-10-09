package stoken

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestBoundedProviderAndRefreshHTTPClient(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Hour, 20 * time.Millisecond} {
		base := &http.Client{Timeout: timeout}
		ctx := BoundedContext(context.WithValue(context.Background(), oauth2.HTTPClient, base))
		bounded := ctx.Value(oauth2.HTTPClient).(*http.Client)
		want := ProviderRequestTimeout
		if timeout > 0 && timeout < want {
			want = timeout
		}
		if base.Timeout != timeout || bounded.Timeout != want {
			t.Fatalf("base=%v bounded=%v want=%v", base.Timeout, bounded.Timeout, want)
		}
		if c := HTTPClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test"})); c.Timeout != want {
			t.Fatalf("provider timeout=%v want=%v", c.Timeout, want)
		}
	}
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx := BoundedContext(context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Timeout: 30 * time.Millisecond}))
	cfg := oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: srv.URL, AuthStyle: oauth2.AuthStyleInParams}}
	start := time.Now()
	if _, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: "test"}).Token(); err == nil {
		t.Fatal("stalled refresh succeeded")
	}
	if time.Since(start) > time.Second || ctx.Err() != nil {
		t.Fatal("refresh was unbounded or cancelled mailbox context")
	}
}
