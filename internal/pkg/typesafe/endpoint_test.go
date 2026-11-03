package typesafe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithEndpoint(t *testing.T) {
	if c := NewClient("k", WithEndpoint("  ")); c.endpoint != defaultEndpoint {
		t.Fatalf("an empty override must keep the default, got %q", c.endpoint)
	}
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.9}}}`))
	}))
	defer srv.Close()
	if _, err := NewClient("k", WithEndpoint("  "+srv.URL+"  ")).Ask(context.Background(), map[string]string{"a": "b"}, map[string]Question{"q": Noul("x")}); err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("the request did not go to the configured endpoint")
	}
}
