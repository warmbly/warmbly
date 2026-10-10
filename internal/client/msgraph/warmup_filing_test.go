package msgraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"golang.org/x/oauth2"
)

func TestResolveMessageIDRequiresCompleteIdentityLookup(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		uncertain        bool
	}{
		{"terminal absence", `{"value":[]}`, "", false},
		{"terminal absence with whitespace", "{\"value\":[]} \n", "", false},
		{"terminal identity", `{"value":[{"id":"live-id"}]}`, "live-id", false},
		{"terminal absence followed by identity", `{"value":[]}{"value":[{"id":"live-id"}]}`, "", true},
		{"identity followed by another page", `{"value":[{"id":"live-id"}]}{"value":[]}`, "", true},
		{"terminal absence followed by junk", `{"value":[]}private-marker`, "", true},
		{"missing collection", `{}`, "", true},
		{"null collection", `{"value":null}`, "", true},
		{"null response", `null`, "", true},
		{"missing identity", `{"value":[{}]}`, "", true},
		{"null identity", `{"value":[{"id":null}]}`, "", true},
		{"empty identity", `{"value":[{"id":""}]}`, "", true},
		{"blank identity", `{"value":[{"id":" \t\n "}]}`, "", true},
		{"padded identity", `{"value":[{"id":" live-id "}]}`, "", true},
		{"ambiguous identities", `{"value":[{"id":"one"},{"id":"two"}]}`, "", true},
		{"empty page continues", `{"value":[],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/messages?$skiptoken=more"}`, "", true},
		{"identity page continues", `{"value":[{"id":"live-id"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/messages?$skiptoken=more"}`, "", true},
		{"foreign continuation", `{"value":[],"@odata.nextLink":"https://other.example.test/collect?token=private-marker"}`, "", true},
		{"relative continuation", `{"value":[],"@odata.nextLink":"/v1.0/me/messages?$skiptoken=more"}`, "", true},
		{"http continuation", `{"value":[],"@odata.nextLink":"http://graph.microsoft.com/v1.0/me/messages?$skiptoken=more"}`, "", true},
		{"empty continuation", `{"value":[],"@odata.nextLink":""}`, "", true},
		{"blank continuation", `{"value":[],"@odata.nextLink":" "}`, "", true},
		{"null continuation", `{"value":[],"@odata.nextLink":null}`, "", true},
		{"wrong continuation type", `{"value":[],"@odata.nextLink":{"token":"private-marker"}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/v1.0/me/messages" || r.URL.Query().Get("$top") != "1" || r.URL.Query().Get("$select") != "id" || r.URL.Query().Get("$filter") != "internetMessageId eq '<warmup@example.test>'" {
					t.Errorf("lookup changed scope or followed continuation: %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, tc.body)
			}, nil)
			id, err := c.ResolveMessageID(t.Context(), "<warmup@example.test>")
			if id != tc.want || (err != nil) != tc.uncertain || requests != 1 {
				t.Fatalf("lookup: id=%q err=%v requests=%d", id, err, requests)
			}
			if tc.uncertain {
				merr := mailErrorOf(t, err)
				if !errors.Is(err, errMessageLookupIncomplete) || merr.Type != errx.MailErrorWarning || merr.Code != errx.MailErrorCodeServerUnreachable || merr.ResolveMethod != errx.MailErrorResolveMethodRetry {
					t.Fatalf("incomplete lookup must be temporary: %v", err)
				}
			}
			if err != nil && strings.Contains(err.Error(), "private-marker") {
				t.Fatal("lookup error exposed continuation payload")
			}
		})
	}
}

func TestResolveMessageIDRejectsBlankIdentityBeforeRequest(t *testing.T) {
	requests := 0
	c := deltaTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		fmt.Fprint(w, `{"value":[]}`)
	}, nil)
	for _, id := range []string{"", " \t\n "} {
		if _, err := c.ResolveMessageID(t.Context(), id); !errors.Is(err, errMessageLookupIncomplete) {
			t.Fatalf("blank identity must fail closed: %v", err)
		}
	}
	if requests != 0 {
		t.Fatal("blank identity queried provider")
	}
}

func TestResolveMessageIDMalformedResponseDoesNotEstablishAbsence(t *testing.T) {
	for _, body := range []string{"{", "", `{"value":{}}`, `{"value":"private-marker"}`, `{"value":[null]}`, `{"value":[{"id":42}]}`} {
		c := graphStatus(t, http.StatusOK, body)
		if id, err := c.ResolveMessageID(t.Context(), "<warmup@example.test>"); err == nil || id != "" || strings.Contains(err.Error(), "private-marker") {
			t.Fatalf("malformed response resolved or exposed payload: id=%q err=%v", id, err)
		}
	}
}

func TestResolveMessageIDPreservesProviderRetryEvidence(t *testing.T) {
	requests := 0
	c := deltaTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"code":"ServiceUnavailable"}}`)
	}, nil)
	if id, err := c.ResolveMessageID(t.Context(), "<warmup@example.test>"); id != "" || err == nil {
		t.Fatalf("unavailable provider resolved: id=%q err=%v", id, err)
	} else if merr := mailErrorOf(t, err); merr.Code != errx.MailErrorCodeServerUnreachable || merr.RetryAfter != 90*time.Second || requests != 1 {
		t.Fatalf("lost provider retry evidence: code=%s retry=%v requests=%d", merr.Code, merr.RetryAfter, requests)
	}
}

func TestWarmupFilingDoesNotMoveAlreadyFiledGraphMessage(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "folder", true: "archive"}[archive], func(t *testing.T) {
			moves := 0
			parent := "inbox-id"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/move"):
					moves++
					parent = "target-id"
					fmt.Fprint(w, `{"id":"new-id"}`)
				case strings.HasSuffix(r.URL.Path, "/mailFolders/archive"):
					fmt.Fprint(w, `{"id":"target-id"}`)
				case strings.HasSuffix(r.URL.Path, "/mailFolders"):
					fmt.Fprint(w, `{"value":[{"id":"target-id","displayName":"Warmbly"}]}`)
				default:
					fmt.Fprintf(w, `{"parentFolderId":%q}`, parent)
				}
			}))
			defer srv.Close()
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: rewriteTo(srv.URL)})
			client := &Client{}
			if err := client.Init(ctx, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
				t.Fatal(err)
			}
			move := func(id string) (string, error) {
				if archive {
					return client.MoveToArchive(ctx, id)
				}
				return client.MoveToFolder(ctx, id, "Warmbly")
			}
			id, err := move("old-id")
			if err != nil || id != "new-id" || moves != 1 {
				t.Fatalf("initial filing: id=%q moves=%d err=%v", id, moves, err)
			}
			id, err = move("new-id")
			if err != nil || id != "new-id" || moves != 1 {
				t.Fatalf("repeated filing moved message again: id=%q moves=%d err=%v", id, moves, err)
			}
		})
	}
}
