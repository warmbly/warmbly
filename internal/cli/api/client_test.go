package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/campaigns":    "/v1/campaigns",
		"campaigns":     "/v1/campaigns",
		"/v1/campaigns": "/v1/campaigns",
		"/v2/campaigns": "/v2/campaigns",
		// "/verify" starts with /v but is not a version, so it must be
		// prefixed rather than treated as v-something.
		"/verify": "/v1/verify",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorEnvelopeSurvives(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"forbidden","message":"missing scope","code":"insufficient_scope","request_id":"req_123"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "wmbly_x", "test")
	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/campaigns"})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %T, want *api.Error", err)
	}
	if apiErr.Code != "insufficient_scope" || apiErr.RequestID != "req_123" {
		t.Errorf("the machine-readable fields were lost: %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "req_123") {
		t.Errorf("the request id must reach the message: %s", apiErr.Error())
	}
	if StatusOf(err) != http.StatusForbidden {
		t.Errorf("StatusOf = %d", StatusOf(err))
	}
}

func TestAnonymousRequestSendsNoBearer(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "test")
	if _, err := c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/auth/cli/code", Anonymous: true}); err != nil {
		t.Fatalf("anonymous request failed: %v", err)
	}
	if sawAuth != "" {
		t.Errorf("an anonymous request carried %q", sawAuth)
	}

	// A normal request with no token must fail before it reaches the network.
	if _, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/me"}); err == nil {
		t.Error("a request with no token should not be attempted")
	}
}

func TestPaginateFollowsTheCursor(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "":
			pages++
			fmt.Fprint(w, `{"data":[{"id":"a"},{"id":"b"}],"pagination":{"has_more":true,"next_cursor":"c2"}}`)
		case "c2":
			pages++
			fmt.Fprint(w, `{"data":[{"id":"c"}],"pagination":{"has_more":false,"next_cursor":null}}`)
		default:
			t.Errorf("unexpected cursor %q", cursor)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "wmbly_x", "test")
	merged, err := c.Paginate(context.Background(), Request{Method: http.MethodGet, Path: "/campaigns"}, 10)
	if err != nil {
		t.Fatalf("paginate: %v", err)
	}
	if pages != 2 {
		t.Errorf("fetched %d pages, want 2", pages)
	}
	var doc struct {
		Data       []map[string]string `json:"data"`
		Pagination struct {
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("merged payload is not JSON: %v", err)
	}
	if len(doc.Data) != 3 {
		t.Errorf("merged %d rows, want 3", len(doc.Data))
	}
	if doc.Pagination.HasMore {
		t.Error("the merged envelope must report no more pages")
	}
}

// An endpoint that does not use the cursor envelope comes back untouched
// rather than being flattened into an empty list.
func TestPaginateLeavesNonListPayloadsAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"count":7}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "wmbly_x", "test")
	out, err := c.Paginate(context.Background(), Request{Method: http.MethodGet, Path: "/unibox/count"}, 10)
	if err != nil {
		t.Fatalf("paginate: %v", err)
	}
	if !strings.Contains(string(out), `"count":7`) {
		t.Errorf("payload was rewritten: %s", out)
	}
}

// A long --all walk will meet the per-key minute budget. The client waits the
// Retry-After rather than returning a partial list the caller cannot
// distinguish from a complete one.
func TestPaginateWaitsOutARateLimit(t *testing.T) {
	limited := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limited {
			limited = false
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"code":"rate_limit_exceeded","message":"slow down"}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"a"}],"pagination":{"has_more":false,"next_cursor":null}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "wmbly_x", "test")
	merged, err := c.Paginate(context.Background(), Request{Method: http.MethodGet, Path: "/campaigns"}, 5)
	if err != nil {
		t.Fatalf("paginate should have waited and retried: %v", err)
	}
	if !strings.Contains(string(merged), `"id":"a"`) {
		t.Errorf("the retried page was lost: %s", merged)
	}
}

// A rate limit that never clears must still end, rather than looping until
// max-pages with the caller none the wiser.
func TestPaginateGivesUpOnAPermanentRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":"rate_limit_exceeded"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "wmbly_x", "test")
	if _, err := c.Paginate(context.Background(), Request{Method: http.MethodGet, Path: "/campaigns"}, 2); err == nil {
		t.Fatal("a permanent rate limit must surface as an error")
	}
}
