package msgraph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func deltaTestClient(t *testing.T, handler http.HandlerFunc, links map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := &Client{Email: "box@outlook.test", DeltaLinks: links}
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: rewriteTo(srv.URL)})
	if err := c.Init(ctx, &oauth2.Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeltaExpiryClassificationIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   bool
	}{
		{410, "syncStateNotFound", true}, {410, "", true}, {400, "syncStateNotFound", true},
		{400, "InvalidRequest", false}, {401, "syncStateNotFound", false},
		{403, "syncStateNotFound", false}, {429, "syncStateNotFound", false}, {503, "syncStateNotFound", false},
	} {
		resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + tc.code + `"}}`))}
		if got := deltaExpired(HandleError(resp)); got != tc.want {
			t.Errorf("status=%d code=%s: expiry=%v, want %v", tc.status, tc.code, got, tc.want)
		}
	}
	if deltaExpired(context.DeadlineExceeded) {
		t.Fatal("transport failure reset a checkpoint")
	}
}

func TestDeltaExpiredRecoveryReplaysBeforeCheckpointAndResumesOnReload(t *testing.T) {
	base := "https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages/delta"
	saved := base + "?$deltatoken=expired"
	page2 := base + "?$skiptoken=recovery-2"
	fresh := base + "?$deltatoken=fresh"
	requests := []string{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests = append(requests, r.URL.RawQuery)
		switch {
		case r.URL.Query().Get("$deltatoken") == "expired":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error":{"code":"syncStateNotFound"}}`))
		case r.URL.Query().Get("$skiptoken") == "recovery-2":
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{map[string]any{"id": "later", "isRead": false}}, "@odata.deltaLink": fresh})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{map[string]any{"id": "unseen", "isRead": false}, map[string]any{"id": "known", "isRead": true}}, "@odata.nextLink": page2})
		}
	}
	delivered := map[string]bool{}
	c := deltaTestClient(t, handler, map[string]string{FolderInbox: saved})
	denyLater := true
	c.OnMessageSeen = func(_ context.Context, _, id string, seen bool) (bool, error) {
		if id == "later" && denyLater {
			return false, ErrStop
		}
		if id == "known" && !seen {
			t.Fatal("lost changed read state")
		}
		delivered[id] = true
		return true, nil
	}
	c.OnDelta = func(_ context.Context, _, link string) error {
		if link == fresh && !delivered["later"] {
			t.Fatal("fresh baseline published before replay")
		}
		saved = link
		return nil
	}
	if err := c.syncFolder(t.Context(), FolderInbox); !errors.Is(err, ErrStop) {
		t.Fatalf("first pass = %v, want budget stop", err)
	}
	if saved != page2 || !delivered["unseen"] || !delivered["known"] {
		t.Fatalf("checkpoint=%s delivered=%v", saved, delivered)
	}
	denyLater = false
	reloaded := deltaTestClient(t, handler, map[string]string{FolderInbox: saved})
	reloaded.OnMessageSeen, reloaded.OnDelta = c.OnMessageSeen, c.OnDelta
	before := len(requests)
	if err := reloaded.syncFolder(t.Context(), FolderInbox); err != nil {
		t.Fatal(err)
	}
	if saved != fresh || len(delivered) != 3 || len(requests) != before+1 || !strings.Contains(requests[before], "recovery-2") {
		t.Fatalf("restart lost progress: saved=%s delivered=%v requests=%v", saved, delivered, requests)
	}
}

func TestDeltaCheckpointPublicationFailurePinsReplay(t *testing.T) {
	fresh := "https://graph.microsoft.com/v1.0/me/mailFolders/archive/messages/delta?$deltatoken=fresh"
	c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{map[string]any{"id": "unseen"}}, "@odata.deltaLink": fresh})
	}, nil)
	delivered := 0
	c.OnMessageSeen = func(context.Context, string, string, bool) (bool, error) { delivered++; return true, nil }
	c.OnDelta = func(context.Context, string, string) error { return errors.New("publication failed") }
	if err := c.syncFolder(t.Context(), FolderArchive); err == nil || c.DeltaLinks[FolderArchive] != "" || delivered != 1 {
		t.Fatalf("publication failure: err=%v links=%v delivered=%d", err, c.DeltaLinks, delivered)
	}
}

func TestDeltaContinuesLaterFoldersAndSkipsOnlyAbsentArchive(t *testing.T) {
	for _, earlyFailure := range []bool{false, true} {
		seen := map[string]bool{}
		c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			folder := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1.0/me/mailFolders/"), "/")[0]
			seen[folder] = true
			if folder == FolderArchive || (folder == FolderInbox && earlyFailure) {
				status := http.StatusNotFound
				if folder == FolderInbox {
					status = http.StatusServiceUnavailable
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorItemNotFound"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}, "@odata.deltaLink": "https://graph.microsoft.com" + r.URL.Path + "?$deltatoken=fresh"})
		}, nil)
		complete, err := c.SyncPass(t.Context())
		if (err != nil) != earlyFailure || complete == earlyFailure || len(seen) != len(TrackedFolders) {
			t.Fatalf("earlyFailure=%v complete=%v err=%v seen=%v", earlyFailure, complete, err, seen)
		}
	}
}
