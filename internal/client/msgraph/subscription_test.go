package msgraph

import (
	"net/http"
	"testing"
)

func TestListSubscriptionsFollowsContinuations(t *testing.T) {
	c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("$skiptoken") == "" {
			_, _ = w.Write([]byte(`{"value":[{"id":"a"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/subscriptions?$skiptoken=2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"id":"b","notificationUrl":"https://x"}]}`))
	}, nil)
	subs, err := c.ListSubscriptions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[0].ID != "a" || subs[1].ID != "b" {
		t.Fatalf("subscriptions %+v, want both pages", subs)
	}
}

func TestListSubscriptionsRefusesAContinuationOffGraph(t *testing.T) {
	c := deltaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[],"@odata.nextLink":"https://example.com/v1.0/subscriptions"}`))
	}, nil)
	if _, err := c.ListSubscriptions(t.Context()); err == nil {
		t.Fatal("a continuation off Graph's root was followed")
	}
}
