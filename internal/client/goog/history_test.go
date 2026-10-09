package goog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestExpiredHistoryOnlyAppliesToNonzeroHistory404(t *testing.T) {
	for _, tc := range []struct {
		name    string
		start   uint64
		code    int
		expired bool
	}{
		{"expired", 50, 404, true}, {"quota", 50, 429, false}, {"forbidden", 50, 403, false}, {"unavailable profile", 0, 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profileCalls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/me/profile" {
					profileCalls++
				}
				if tc.start != 0 && r.URL.Query().Get("startHistoryId") != "50" {
					t.Errorf("history request=%s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"message":"injected"}}`, tc.code)
			}))
			defer srv.Close()
			svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithHTTPClient(srv.Client()), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			client := &Client{srv: svc}
			checkpoint, err := client.FetchHistory(t.Context(), tc.start)
			if err == nil || errors.Is(err, ErrHistoryExpired) != tc.expired {
				t.Fatalf("checkpoint=%d err=%v", checkpoint, err)
			}
			if checkpoint != tc.start {
				t.Fatalf("checkpoint advanced from %d to %d on failure", tc.start, checkpoint)
			}
			if tc.start != 0 && profileCalls != 0 {
				t.Fatal("expired history must not silently replace the checkpoint")
			}
		})
	}
}

func TestHistoryBaselineRejectsEmptyProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"historyId":"0"}`)) }))
	defer srv.Close()
	svc, err := gmail.NewService(t.Context(), option.WithEndpoint(srv.URL+"/"), option.WithHTTPClient(srv.Client()), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	if id, err := (&Client{srv: svc}).HistoryBaseline(t.Context()); err == nil || id != 0 {
		t.Fatalf("baseline=%d err=%v", id, err)
	}
}

func TestRecoveryPageExpiryIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		name, token, message string
		code                 int
		recovery, expired    bool
	}{
		{"expired recovery page", "old", "Invalid pageToken", 400, true, true},
		{"other invalid input", "old", "Invalid query", 400, true, false},
		{"first page invalid input", "", "Invalid pageToken", 400, true, false},
		{"transient", "old", "Invalid pageToken", 429, true, false},
		{"ordinary backfill", "old", "Invalid pageToken", 400, false, true},
		{"ordinary spaced token", "old", "Invalid page token", 400, false, true},
		{"ordinary other invalid input", "old", "Invalid query", 400, false, false},
		{"ordinary invalid page size", "old", "Invalid page size", 400, false, false},
		{"ordinary first page", "", "Invalid pageToken", 400, false, false},
		{"ordinary quota", "old", "Invalid pageToken", 429, false, false},
		{"ordinary auth", "old", "Invalid pageToken", 401, false, false},
		{"ordinary forbidden", "old", "Invalid pageToken", 403, false, false},
		{"ordinary transport", "old", "Invalid pageToken", 503, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"message":%q}}`, tc.code, tc.message)
			}))
			defer srv.Close()
			svc, err := gmail.NewService(t.Context(), option.WithEndpoint(srv.URL+"/"), option.WithHTTPClient(srv.Client()), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = (&Client{srv: svc}).listMessages(t.Context(), "after:2026/09/01", tc.token, 50, tc.recovery)
			if err == nil || errors.Is(err, ErrPageTokenExpired) != tc.expired || errors.Is(err, ErrRecoveryPageExpired) != tc.expired {
				t.Fatalf("err=%v; want expired=%t", err, tc.expired)
			}
		})
	}
}
