package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// captured is one request the stub PostHog received.
type captured struct {
	APIKey     string         `json:"api_key"`
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
}

// stub stands in for PostHog's capture API and hands back what it was sent.
func stub(t *testing.T) (*httptest.Server, chan captured) {
	t.Helper()
	got := make(chan captured, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != capturePath {
			t.Errorf("posted to %s, want %s", r.URL.Path, capturePath)
		}
		var c captured
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			t.Errorf("decode body: %v", err)
		}
		got <- c
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func waitFor(t *testing.T, got chan captured) captured {
	t.Helper()
	select {
	case c := <-got:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("no event reached the capture host")
		return captured{}
	}
}

// The contract with PostHog's ingestion for a named person: the account id as
// the distinct id, so the event merges with the browser session the dashboard
// identified under the same id; the person on $set, acquisition on $set_once,
// the workspace on $groups, and the request's address for geolocation. A
// second event identifies the group's properties.
func TestCaptureSendsTheIdentifiedContract(t *testing.T) {
	srv, got := stub(t)
	c := New("phc_test", srv.URL)
	if c == nil {
		t.Fatal("a configured key should produce a client")
	}

	c.Capture("signup_completed", Request{
		UserID:           "user-1",
		Email:            "ada@example.com",
		Name:             "Ada Lovelace",
		OrganizationID:   "org-1",
		OrganizationName: "Analytical Engines",
		Plan:             "pro",
		SetOnce:          map[string]any{"utm_source": "newsletter"},
		IP:               "203.0.113.9",
		UserAgent:        "Mozilla/5.0",
		Host:             "app.warmbly.com",
	}, map[string]any{"invited": false})

	events := map[string]captured{}
	for range 2 {
		ev := waitFor(t, got)
		events[ev.Event] = ev
	}

	ev, ok := events["signup_completed"]
	if !ok {
		t.Fatalf("signup_completed never arrived; got %v", events)
	}
	if ev.APIKey != "phc_test" {
		t.Errorf("api_key = %q", ev.APIKey)
	}
	if ev.DistinctID != "user-1" {
		t.Errorf("distinct_id = %q, want the user id", ev.DistinctID)
	}
	if _, cookieless := ev.Properties["$cookieless_mode"]; cookieless {
		t.Error("a named person must not carry the cookieless flag; ingestion would replace the id with the hash")
	}
	for key, want := range map[string]any{
		"$ip":             "203.0.113.9",
		"$raw_user_agent": "Mozilla/5.0",
		"$host":           "app.warmbly.com",
		"invited":         false,
	} {
		if ev.Properties[key] != want {
			t.Errorf("properties[%q] = %v, want %v", key, ev.Properties[key], want)
		}
	}
	set, _ := ev.Properties["$set"].(map[string]any)
	if set["email"] != "ada@example.com" || set["name"] != "Ada Lovelace" {
		t.Errorf("$set = %v", ev.Properties["$set"])
	}
	once, _ := ev.Properties["$set_once"].(map[string]any)
	if once["utm_source"] != "newsletter" {
		t.Errorf("$set_once = %v", ev.Properties["$set_once"])
	}
	groups, _ := ev.Properties["$groups"].(map[string]any)
	if groups[OrganizationGroup] != "org-1" {
		t.Errorf("$groups = %v", ev.Properties["$groups"])
	}

	gi, ok := events["$groupidentify"]
	if !ok {
		t.Fatalf("$groupidentify never arrived; got %v", events)
	}
	if gi.Properties["$group_type"] != OrganizationGroup || gi.Properties["$group_key"] != "org-1" {
		t.Errorf("group identify = %v", gi.Properties)
	}
	gset, _ := gi.Properties["$group_set"].(map[string]any)
	if gset["name"] != "Analytical Engines" || gset["plan"] != "pro" {
		t.Errorf("$group_set = %v", gi.Properties["$group_set"])
	}
}

// With nobody to name the event falls back to the cookieless contract: the
// sentinel as the distinct id, the mode flag, and the three hash inputs. Get
// any of these wrong and the event lands as a separate visitor, or not at all.
func TestCaptureWithoutAPersonIsCookieless(t *testing.T) {
	srv, got := stub(t)
	c := New("phc_test", srv.URL)
	c.Capture("subscription_started", Request{
		IP:        "203.0.113.9",
		UserAgent: "Mozilla/5.0",
		Host:      "app.warmbly.com",
	}, map[string]any{"plan": "pro"})

	ev := waitFor(t, got)
	if ev.DistinctID != cookielessDistinctID {
		t.Errorf("distinct_id = %q, want the cookieless sentinel %q", ev.DistinctID, cookielessDistinctID)
	}
	for key, want := range map[string]any{
		"$cookieless_mode": true,
		"$ip":              "203.0.113.9",
		"$raw_user_agent":  "Mozilla/5.0",
		"$host":            "app.warmbly.com",
		"plan":             "pro",
	} {
		if ev.Properties[key] != want {
			t.Errorf("properties[%q] = %v, want %v", key, ev.Properties[key], want)
		}
	}
	for _, absent := range []string{"$set", "$set_once", "$groups"} {
		if _, ok := ev.Properties[absent]; ok {
			t.Errorf("event carries %q with nobody named", absent)
		}
	}
	select {
	case extra := <-got:
		t.Errorf("unexpected second event %q; there is no group to identify", extra.Event)
	case <-time.After(200 * time.Millisecond):
	}
}

// No key is the self-host default, and it has to mean no client and no request
// rather than a client that quietly points at PostHog Cloud.
func TestNewWithoutAKeyIsOffAndSafeToCall(t *testing.T) {
	for _, key := range []string{"", "   "} {
		if c := New(key, ""); c != nil {
			t.Fatalf("New(%q) returned a client; analytics must be off without a key", key)
		}
	}
	// A nil client is the "never wired" shape and must not panic.
	var c *Client
	c.Capture("signup_completed", Request{}, nil)
}

// An unset host must resolve to EU cloud, and a trailing slash must not produce
// a double slash in the capture path.
func TestHostDefaultingAndTrimming(t *testing.T) {
	if got := New("k", "").host; got != DefaultHost {
		t.Errorf("default host = %q, want %q", got, DefaultHost)
	}
	if got := New("k", "https://ph.example.com/").host; got != "https://ph.example.com" {
		t.Errorf("trailing slash not trimmed: %q", got)
	}
}
